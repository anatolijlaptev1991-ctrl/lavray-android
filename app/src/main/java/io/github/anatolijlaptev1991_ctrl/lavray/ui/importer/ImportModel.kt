// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileImportContent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

enum class ImportTab { Link, Qr, File }

@Immutable
data class ImportUi(
    val open: Boolean = false,
    val tab: ImportTab = ImportTab.Link,
    /** The content to import. Large documents (files) are kept here but not shown in a text field. */
    val payload: String = "",
    val input: ImportInput = ImportInput.Empty,
    val busy: Boolean = false,
    val error: ErrorKind? = null,
    /** The line of the pasted text the core named when it refused the content, if it named one. */
    val errorLine: Int? = null,
    /** Shown when a QR image or file could not be read or held nothing recognisable. */
    val note: ImportNote? = null,
    /** What the core says it will do with [payload] (a preview that saves nothing), once it has said it. */
    val preview: CorePreview? = null,
    val previewError: PreviewError? = null,
    /** Why the last import brought nothing in although the core did not refuse it. */
    val problem: ImportProblem? = null,
    /** Which opening of the sheet this is: work that finishes after the sheet was closed must not touch a newer one. */
    val sheet: Int = 0,
) {
    val payloadLarge: Boolean get() = payload.length > FIELD_LIMIT
    val canSubmit: Boolean get() = !busy && input != ImportInput.Empty && input != ImportInput.Unrecognized &&
        !(input is ImportInput.Links && input.readable == 0) && previewError?.blocksImport != true

    companion object {
        const val FIELD_LIMIT = 20_000
    }
}

/**
 * Switching tabs drops what the previous tab held: otherwise a link typed under "Link" would keep its "1 server"
 * preview and an enabled Import button under "File" or "QR code", where nothing has been chosen.
 */
fun ImportUi.switchedTo(next: ImportTab): ImportUi =
    if (next == tab) copy(note = null, error = null)
    else copy(
        tab = next, payload = "", input = ImportInput.Empty, note = null, error = null, errorLine = null,
        preview = null, previewError = null, problem = null,
    )

enum class ImportNote { QrNotFound, QrUnreadable, FileUnreadable, FileTooLarge, NothingRecognised }

sealed interface ImportEvent {
    /** The import finished and the sheet closed (or was closed already): this is what changed. */
    data class Done(val report: ImportReport) : ImportEvent

    /** The import brought nothing in, and nobody is looking at the sheet any more to be told. */
    data class NotDone(val problem: ImportProblem) : ImportEvent

    /** The core refused the import and the sheet is gone. */
    data class Failed(val kind: ErrorKind) : ImportEvent
}

/**
 * The import flow: text arrives from the clipboard, a typed link, a QR code (camera or image), a file, a share
 * intent or a deep link. While the sheet is open the core is asked what it would do with the text (a preview that
 * saves nothing), and the local classifier answers meanwhile. Content from outside the app is never imported
 * silently. On Import the core decides again: a subscription link is added as a subscription and loaded
 * through [SubscriptionsModel], whose progress the sheet shows; anything else is saved as servers.
 * The core's answer, not the local guess, tells which it is, and an answer that saved nothing is never called a success.
 */
class ImportModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val subscriptions: SubscriptionsModel,
    private val onServersChanged: () -> Unit,
    private val previewDelayMs: Long = PREVIEW_DELAY_MS,
) {
    private val mutable = MutableStateFlow(ImportUi())
    // A queue, not a broadcast: a result that arrives while the app is in the background waits for the screen to come back.
    private val eventQueue = Channel<ImportEvent>(Channel.UNLIMITED)
    private var previewJob: Job? = null

    val ui: StateFlow<ImportUi> = mutable.asStateFlow()
    val events: Flow<ImportEvent> = eventQueue.receiveAsFlow()

    /** Opens the sheet, optionally with text that came from outside (deep link, share, scan). */
    fun open(prefill: String? = null, tab: ImportTab = ImportTab.Link) {
        previewJob?.cancel()
        mutable.value = ImportUi(open = true, tab = tab, sheet = mutable.value.sheet + 1)
        if (!prefill.isNullOrBlank()) setPayload(prefill)
    }

    fun close() {
        previewJob?.cancel()
        mutable.update { it.copy(open = false, busy = false) }
    }

    fun selectTab(tab: ImportTab) {
        if (tab != mutable.value.tab) previewJob?.cancel()
        mutable.update { it.switchedTo(tab) }
    }

    fun setPayload(text: String) {
        previewJob?.cancel()
        mutable.update { it.copy(payload = text, note = null, error = null, errorLine = null, problem = null, preview = null, previewError = null) }
        val sheet = mutable.value.sheet
        previewJob = scope.launch(Dispatchers.Default) {
            val input = LinkClassifier.classify(text)
            mutable.update { cur -> if (cur.payload == text) cur.copy(input = input) else cur }
            if (input != ImportInput.Empty && input != ImportInput.Unrecognized) askCore(text, sheet)
        }
    }

    fun setNote(note: ImportNote) = mutable.update { it.copy(note = note) }

    /** A decoded QR code: accepted into the sheet if it is something we can import, otherwise a hint. */
    fun offerScanned(text: String) {
        if (LinkClassifier.isImportable(text)) {
            open(text, ImportTab.Link)
        } else {
            mutable.update { it.copy(open = true, tab = ImportTab.Qr, note = ImportNote.NothingRecognised) }
        }
    }

    // ---- the core's preview: debounced, cancelled by the next input, only while the sheet is open

    private suspend fun askCore(text: String, sheet: Int) {
        if (text.length > PREVIEW_LIMIT) return // a whole document: the local reading stands, the import itself is not capped
        delay(previewDelayMs)
        if (!stillShowing(sheet, text)) return
        val reply = session.call { profileImportContent(ImportContentRequest(content = text, previewOnly = true)) }
        mutable.update { cur ->
            when {
                cur.sheet != sheet || !cur.open || cur.payload != text -> cur
                reply is CoreResult.Ok -> cur.copy(preview = ImportOutcomes.preview(reply.value), previewError = null)
                reply is CoreResult.Fail -> cur.copy(previewError = PreviewError(reply.kind, reply.line, isEncrypted(text)), preview = null)
                else -> cur
            }
        }
    }

    private fun stillShowing(sheet: Int, text: String): Boolean = mutable.value.let { it.open && it.sheet == sheet && it.payload == text }

    private fun isEncrypted(text: String): Boolean = text.trimStart().startsWith(ENCRYPTED_PREFIX, ignoreCase = true)

    // ---- import

    fun submit(targetGroupId: Long?) {
        val cur = mutable.value
        if (!cur.canSubmit) return
        previewJob?.cancel()
        mutable.update { it.copy(busy = true, error = null, errorLine = null, problem = null) }
        scope.launch(Dispatchers.IO) { settle(cur.sheet, importNow(cur.payload, targetGroupId)) }
    }

    private suspend fun importNow(payload: String, targetGroupId: Long?): ImportResult {
        val request = ImportContentRequest(content = payload, targetGroupId = targetGroupId)
        return when (val r = session.call { profileImportContent(request) }) {
            is CoreResult.Fail -> ImportResult.Rejected(r.kind, r.line)
            is CoreResult.Ok -> if (r.value.kind == ImportContentResponse.ContentKind.CONTENT_KIND_SUBSCRIPTION_URL) {
                // The core saved nothing for a subscription link: the client makes the subscription and loads it.
                ImportOutcomes.subscribed(subscriptions.addAndFetch(payload))
            } else {
                ImportOutcomes.saved(r.value)
            }
        }
    }

    private fun settle(sheet: Int, result: ImportResult) {
        if (changedServers(result)) onServersChanged()
        val mine = mutable.value.sheet == sheet
        val visible = mine && mutable.value.open
        mutable.update { cur ->
            when {
                cur.sheet != sheet -> cur
                result is ImportResult.Report -> ImportUi(sheet = cur.sheet)
                result is ImportResult.Problem -> cur.copy(busy = false, problem = result.problem)
                result is ImportResult.Rejected -> cur.copy(busy = false, error = result.kind, errorLine = result.line)
                else -> cur
            }
        }
        when {
            result is ImportResult.Report -> eventQueue.trySend(ImportEvent.Done(result.report))
            visible -> Unit
            result is ImportResult.Problem -> eventQueue.trySend(ImportEvent.NotDone(result.problem))
            result is ImportResult.Rejected -> eventQueue.trySend(ImportEvent.Failed(result.kind))
        }
    }

    /** The servers list is read again whenever something may have been saved, even by an import that then reported a problem. */
    private fun changedServers(result: ImportResult): Boolean = when (result) {
        is ImportResult.Report -> true
        is ImportResult.Problem -> result.problem is ImportProblem.NotFetched
        is ImportResult.Rejected -> false
    }

    private companion object {
        const val PREVIEW_DELAY_MS = 400L
        const val PREVIEW_LIMIT = 1024 * 1024
        const val ENCRYPTED_PREFIX = "happ://"
    }
}
