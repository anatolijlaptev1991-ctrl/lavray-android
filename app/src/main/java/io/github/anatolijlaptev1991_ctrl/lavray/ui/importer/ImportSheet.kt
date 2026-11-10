// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import android.content.ClipboardManager
import android.content.Context
import android.content.ContentResolver
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.AppEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSheet
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Hosts the import sheet and turns import results into toasts. Placed once, in the app shell. */
@Composable
fun ImportHost() {
    val env = LocalEnv.current
    val nav = LocalNavigator.current
    val model = env.importer
    val ui by model.ui.collectAsStateWithLifecycle()
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(model) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            model.events.collect { e ->
                val ctx = env.appContext
                val toSubscriptions = { nav.openSection("servers", "subscriptions") }
                when (e) {
                    is ImportEvent.Done -> env.showImportToast(e.report.toastText(ctx), toSubscriptions.takeIf { e.report is ImportReport.SubscriptionConfirm })
                    is ImportEvent.NotDone -> env.showImportToast(e.problem.toastText(ctx), toSubscriptions.takeIf { e.problem is ImportProblem.NotFetched })
                    is ImportEvent.Failed -> env.showImportToast(ctx.getString(e.kind.textRes()), null)
                }
            }
        }
    }
    if (ui.open) ImportSheet(ui, model)
}

/** The toast of an import; with [openSubscriptions] it carries the way to the screen where the person goes on. */
private fun AppEnv.showImportToast(text: String, openSubscriptions: (() -> Unit)?) {
    if (openSubscriptions == null) toast.show(text) else toast.show(text, appContext.getString(R.string.imp_p_open), openSubscriptions)
}

@Composable
private fun ImportSheet(ui: ImportUi, model: ImportModel) {
    val nav = LocalNavigator.current
    LavSheet(stringResource(R.string.imp_title), onDismiss = model::close) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LavChip(stringResource(R.string.imp_tab_link), ui.tab == ImportTab.Link, { model.selectTab(ImportTab.Link) }, icon = LavIcon.Link)
            LavChip(stringResource(R.string.imp_tab_qr), ui.tab == ImportTab.Qr, { model.selectTab(ImportTab.Qr) }, icon = LavIcon.Qr)
            LavChip(stringResource(R.string.imp_tab_file), ui.tab == ImportTab.File, { model.selectTab(ImportTab.File) }, icon = LavIcon.File)
        }
        when (ui.tab) {
            ImportTab.Link -> LinkTab(ui, model)
            ImportTab.Qr -> QrTab(model)
            ImportTab.File -> FileTab(model)
        }
        ui.note?.let { NoteBanner(it) }
        Preview(ui)
        ui.previewError?.let { PreviewErrorBanner(it) }
        if (ui.busy) Working()
        ui.problem?.let { p -> ProblemBanner(p) { model.close(); nav.openSection("servers", "subscriptions") } }
        ui.error?.let { kind ->
            LavBanner(StatusTone.Error, ui.errorLine?.let { stringResource(R.string.imp_err_line, it.toString()) } ?: stringResource(kind.textRes()))
        }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, androidx.compose.ui.Alignment.End)) {
            LavButton(stringResource(R.string.common_cancel), model::close, variant = ButtonVariant.Ghost)
            LavButton(
                stringResource(if (ui.preview?.isSubscription == true) R.string.subs_add else R.string.imp_do), { model.submit(null) },
                variant = ButtonVariant.Primary, loading = ui.busy, enabled = ui.canSubmit,
            )
        }
    }
}

@Composable
private fun LinkTab(ui: ImportUi, model: ImportModel) {
    val context = LocalContext.current
    var clip by remember { mutableStateOf<String?>(null) }
    // The clipboard is read once, when this tab opens with an empty field (note imp.clipNote says so).
    LaunchedEffect(ui.open) { clip = if (ui.payload.isEmpty()) readClipboardLink(context) else null }
    val offer = clip
    if (offer != null && ui.payload.isEmpty()) {
        LavBanner(
            StatusTone.Direct, stringResource(R.string.imp_clip, LinkClassifier.mask(offer)), text = stringResource(R.string.imp_clipNote),
            actions = {
                LavButton(stringResource(R.string.imp_clipPaste), { model.setPayload(offer); clip = null }, variant = ButtonVariant.Primary, icon = LavIcon.Clipboard)
                LavButton(stringResource(R.string.imp_clipHide), { clip = null }, variant = ButtonVariant.Ghost)
            },
        )
    }
    if (ui.payloadLarge) {
        LavText(stringResource(R.string.imp_prev_doc), style = Lav.type.body)
    } else {
        var text by remember(ui.payload) { mutableStateOf(ui.payload) }
        LavTextField(
            text, { text = it; model.setPayload(it) },
            modifier = Modifier.fillMaxWidth(), label = stringResource(R.string.imp_link),
            placeholder = "vless://…", mono = true, singleLine = false, minLines = 3, leading = LavIcon.Link,
        )
        LavText(stringResource(R.string.imp_hint), style = Lav.type.small, color = Lav.colors.textTertiary)
        LavButton(
            stringResource(R.string.imp_clipPaste),
            { readClipboardText(context)?.let { model.setPayload(it) } },
            icon = LavIcon.Clipboard,
        )
    }
}

@Composable
private fun QrTab(model: ImportModel) {
    val context = LocalContext.current
    val nav = LocalNavigator.current
    val scope = rememberCoroutineScope()
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri != null) scope.launch {
            when (val r = QrImageReader.read(context.contentResolver, uri)) {
                is QrImageResult.Found -> model.offerScanned(r.text)
                QrImageResult.NotFound -> model.setNote(ImportNote.QrNotFound)
                QrImageResult.Unreadable -> model.setNote(ImportNote.QrUnreadable)
            }
        }
    }
    LavText(stringResource(R.string.imp_qr_hint), style = Lav.type.small, color = Lav.colors.textSecondary)
    LavButton(
        stringResource(R.string.imp_qr_scan), { model.close(); nav.push("scan") },
        variant = ButtonVariant.Primary, icon = LavIcon.Camera, modifier = Modifier.fillMaxWidth(),
    )
    LavButton(stringResource(R.string.imp_qrFile), { picker.launch("image/*") }, icon = LavIcon.Image, modifier = Modifier.fillMaxWidth())
}

@Composable
private fun FileTab(model: ImportModel) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri != null) scope.launch {
            when (val r = readTextFile(context.contentResolver, uri)) {
                is FileRead.Text -> model.setPayload(r.text)
                FileRead.TooLarge -> model.setNote(ImportNote.FileTooLarge)
                FileRead.Unreadable -> model.setNote(ImportNote.FileUnreadable)
            }
        }
    }
    LavText(stringResource(R.string.imp_fileHint_phone), style = Lav.type.small, color = Lav.colors.textSecondary)
    LavButton(stringResource(R.string.imp_file_choose), { picker.launch("*/*") }, icon = LavIcon.File, modifier = Modifier.fillMaxWidth())
}

@Composable
private fun NoteBanner(note: ImportNote) {
    val text = when (note) {
        ImportNote.QrNotFound -> stringResource(R.string.imp_qrNotFound)
        ImportNote.QrUnreadable -> stringResource(R.string.imp_qrBadImage)
        ImportNote.FileUnreadable -> stringResource(R.string.imp_file_unreadable)
        ImportNote.FileTooLarge -> stringResource(R.string.imp_fileTooLarge, (LinkClassifier.MAX_TEXT_CHARS / (1024 * 1024)).toString())
        ImportNote.NothingRecognised -> stringResource(R.string.imp_unrecognized)
    }
    LavBanner(StatusTone.Connecting, text)
}

@Composable
private fun Working() {
    val subs by LocalEnv.current.subscriptions.state.collectAsStateWithLifecycle()
    WorkingLine(fetching = subs.isBusy, phase = subs.progress.phase)
}

@Composable
private fun Preview(ui: ImportUi) {
    val core = ui.preview
    if (core != null) {
        CoreSummary(core, ui.input)
        (ui.input as? ImportInput.Links)?.issues?.forEach { IssueBanner(it, othersStay = core.count > 0) }
        return
    }
    when (val input = ui.input) {
        ImportInput.Empty -> Unit
        ImportInput.Unrecognized -> if (ui.payload.isNotBlank()) LavBanner(StatusTone.Connecting, stringResource(R.string.imp_unrecognized))
        is ImportInput.Links -> {
            // Only what the core will really take is counted: a line it would refuse is named, not promised.
            if (input.readable > 0) {
                LavText(
                    pluralStringResource(R.plurals.pl_srv, input.readable, input.readable) +
                        " · " + input.schemes.joinToString(", ") { it.uppercase() },
                    style = Lav.type.bodyStrong,
                )
            }
            input.issues.forEach { IssueBanner(it, othersStay = input.readable > 0) }
        }
        is ImportInput.SubscriptionUrl -> {
            LavText(stringResource(R.string.imp_pv_sub), style = Lav.type.bodyStrong)
            if (input.insecure) LavBanner(StatusTone.Connecting, stringResource(R.string.imp_httpWarn))
        }
        is ImportInput.EncryptedLink -> LavText(stringResource(R.string.onb_src_happ_d), style = Lav.type.bodyStrong)
        is ImportInput.Document -> LavText(stringResource(R.string.imp_prev_doc), style = Lav.type.bodyStrong)
    }
}

@Composable
private fun IssueBanner(issue: LinkIssue, othersStay: Boolean) {
    val why = stringResource(
        when (issue.problem) {
            LinkProblem.SsBase64 -> R.string.imp_issue_ss_b64
            LinkProblem.SsPair -> R.string.imp_issue_ss_pair
            LinkProblem.SsMethod -> R.string.imp_issue_ss_method
        },
    )
    val rest = if (othersStay) " " + stringResource(R.string.imp_issue_rest) else ""
    LavBanner(StatusTone.Connecting, stringResource(R.string.imp_issue_t, issue.line.toString()), text = why + rest + "\n" + issue.masked)
}

// ---- clipboard and file helpers (main-thread clipboard, IO-thread file)

internal fun readClipboardText(context: Context): String? {
    val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return null
    val item = cm.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0) ?: return null
    return item.coerceToText(context)?.toString()?.takeIf { it.length <= CLIPBOARD_LIMIT }
}

internal fun readClipboardLink(context: Context): String? = readClipboardText(context)?.trim()?.takeIf(LinkClassifier::isImportable)

private const val CLIPBOARD_LIMIT = 100_000
private const val BUFFER = 16 * 1024

sealed interface FileRead {
    data class Text(val text: String) : FileRead
    data object TooLarge : FileRead
    data object Unreadable : FileRead
}

/** Reads a text file the person picked. Larger than the core accepts is refused before it is read into memory. */
internal suspend fun readTextFile(resolver: ContentResolver, uri: Uri): FileRead = withContext(Dispatchers.IO) {
    try {
        val size = resolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use { c ->
            if (c.moveToFirst() && !c.isNull(0)) c.getLong(0) else -1L
        } ?: -1L
        if (size > LinkClassifier.MAX_TEXT_CHARS) return@withContext FileRead.TooLarge
        val input = resolver.openInputStream(uri) ?: return@withContext FileRead.Unreadable
        val out = java.io.ByteArrayOutputStream()
        input.use { stream ->
            val buffer = ByteArray(BUFFER)
            while (true) {
                val n = stream.read(buffer)
                if (n < 0) break
                if (out.size() + n > LinkClassifier.MAX_TEXT_CHARS) return@withContext FileRead.TooLarge
                out.write(buffer, 0, n)
            }
        }
        FileRead.Text(out.toByteArray().toString(Charsets.UTF_8))
    } catch (_: java.io.IOException) {
        FileRead.Unreadable
    } catch (_: SecurityException) {
        FileRead.Unreadable
    }
}
