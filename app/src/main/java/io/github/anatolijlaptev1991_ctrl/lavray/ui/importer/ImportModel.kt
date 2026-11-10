// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileImportContent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
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
) {
    val payloadLarge: Boolean get() = payload.length > FIELD_LIMIT
    val canSubmit: Boolean get() = !busy && input != ImportInput.Empty && input != ImportInput.Unrecognized &&
        !(input is ImportInput.Links && input.readable == 0)

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
    else copy(tab = next, payload = "", input = ImportInput.Empty, note = null, error = null, errorLine = null)

enum class ImportNote { QrNotFound, QrUnreadable, FileUnreadable, FileTooLarge, NothingRecognised }

sealed interface ImportEvent {
    data class Imported(val count: Int, val subscription: Boolean) : ImportEvent
}

/**
 * The import flow: text arrives from the clipboard, a typed link, a QR code (camera or image), a file, a share
 * intent or a deep link; it is classified locally and only sent to the core when the person confirms. Content
 * from outside the app is never imported silently.
 */
class ImportModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val servers: ServersModel,
) {
    private val mutable = MutableStateFlow(ImportUi())
    private val mutableEvents = MutableSharedFlow<ImportEvent>(extraBufferCapacity = 4)

    val ui: StateFlow<ImportUi> = mutable.asStateFlow()
    val events: SharedFlow<ImportEvent> = mutableEvents.asSharedFlow()

    /** Opens the sheet, optionally with text that came from outside (deep link, share, scan). */
    fun open(prefill: String? = null, tab: ImportTab = ImportTab.Link) {
        mutable.value = ImportUi(open = true, tab = tab)
        if (!prefill.isNullOrBlank()) setPayload(prefill)
    }

    fun close() {
        mutable.update { it.copy(open = false, busy = false) }
    }

    fun selectTab(tab: ImportTab) = mutable.update { it.switchedTo(tab) }

    fun setPayload(text: String) {
        mutable.update { it.copy(payload = text, note = null, error = null, errorLine = null) }
        scope.launch(Dispatchers.Default) {
            val input = LinkClassifier.classify(text)
            mutable.update { cur -> if (cur.payload == text) cur.copy(input = input) else cur }
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

    fun submit(targetGroupId: Long?) {
        val cur = mutable.value
        if (!cur.canSubmit) return
        mutable.update { it.copy(busy = true, error = null, errorLine = null) }
        scope.launch(Dispatchers.IO) {
            val request = ImportContentRequest(content = cur.payload, targetGroupId = targetGroupId)
            when (val r = session.call { profileImportContent(request) }) {
                is CoreResult.Ok -> {
                    val added = r.value.addedProfileIds.size
                    mutable.value = ImportUi()
                    servers.refresh()
                    mutableEvents.tryEmit(ImportEvent.Imported(added, subscription = r.value.createdGroupId != null))
                }
                is CoreResult.Fail -> mutable.update { it.copy(busy = false, error = r.kind, errorLine = r.line) }
            }
        }
    }
}
