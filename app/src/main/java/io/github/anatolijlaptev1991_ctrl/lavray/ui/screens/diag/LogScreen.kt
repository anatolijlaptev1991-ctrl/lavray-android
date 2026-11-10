// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import android.content.ActivityNotFoundException
import android.content.Intent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavColors
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import java.text.DateFormat
import java.util.Date

/**
 * Log (desktop screen_log.tsx): the last lines and the live stream of the core log, level filter, search, pause,
 * auto-scroll, Copy and Share. The core redacts secrets before it sends a line and the lines are shown unchanged.
 * The stream runs only while this screen is composed and the app is started; the buffer holds [LogBuffer.CAPACITY] lines.
 */
@Composable
fun LogScreen() {
    val env = LocalEnv.current
    val context = LocalContext.current
    val model = remember { LogModel(env.session) }
    val lines by model.lines.collectAsStateWithLifecycle()
    val error by model.streamError.collectAsStateWithLifecycle()
    val owner = LocalLifecycleOwner.current
    var min by remember { mutableStateOf(LogSeverity.Info) }
    var query by remember { mutableStateOf("") }
    var frozen by remember { mutableStateOf<List<LogEntry>?>(null) }
    var autoscroll by remember { mutableStateOf(true) }
    val listState = rememberLazyListState()

    LaunchedEffect(model) { owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { model.stream() } }
    DisposableEffect(model) { onDispose { model.clear() } }

    val source = frozen ?: lines
    val shown by remember(source, min, query) { derivedStateOf { LogFilter.apply(source, min, query) } }
    val lastSeq = shown.lastOrNull()?.seq
    LaunchedEffect(lastSeq, autoscroll) {
        if (autoscroll && shown.isNotEmpty()) listState.scrollToItem(shown.lastIndex)
    }
    val copied = stringResource(R.string.toast_logCopied)
    val failed = stringResource(R.string.conns_copyFailed)
    val shareFailed = stringResource(R.string.log_shareFailed)

    Column(Modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            LavTextField(
                query, { query = it }, Modifier.fillMaxWidth(), placeholder = stringResource(R.string.log_searchPh), leading = LavIcon.Search,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            )
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LogSeverity.entries.forEach { s -> LavChip(s.label, min == s, { min = s }) }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                LavIconButton(
                    if (frozen == null) LavIcon.Stop else LavIcon.Refresh,
                    stringResource(if (frozen == null) R.string.log_pause else R.string.log_resume),
                    { frozen = if (frozen == null) lines else null },
                )
                LavIconButton(LavIcon.Copy, stringResource(R.string.log_copy), {
                    val ok = copyToClipboard(context, LogFilter.export(shown))
                    env.toast.show(if (ok) copied else failed)
                })
                LavIconButton(LavIcon.Share, stringResource(R.string.log_share), {
                    if (!shareText(context, LogFilter.export(shown, LogFilter.SHARE_MAX_CHARS))) env.toast.show(shareFailed)
                })
                LavSwitchRow(stringResource(R.string.log_autoscroll), autoscroll, { autoscroll = it }, Modifier.weight(1f))
            }
            LavText(stringResource(R.string.log_exportD), style = Lav.type.caption, color = Lav.colors.textTertiary)
        }
        error?.let { LavBanner(StatusTone.Error, stringResource(it.textRes()), Modifier.padding(horizontal = 16.dp), text = stringResource(R.string.conns_retrying)) }
        if (shown.isEmpty()) {
            LavEmptyState(stringResource(R.string.nav_log), text = stringResource(if (source.isEmpty()) R.string.log_none else R.string.log_noMatch), icon = LavIcon.List)
        } else {
            LogList(shown, listState)
        }
    }
}

@Composable
private fun LogList(shown: List<LogEntry>, state: androidx.compose.foundation.lazy.LazyListState) {
    val time = remember { DateFormat.getTimeInstance(DateFormat.MEDIUM) }
    val colors = Lav.colors
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp), state = state) {
        items(shown, key = { it.seq }, contentType = { "line" }) { e ->
            BasicText(lineText(e, time, colors), Modifier.fillMaxWidth().padding(vertical = 2.dp), style = Lav.type.mono.copy(fontSize = Lav.type.caption.fontSize, lineHeight = Lav.type.caption.fontSize * 1.35f))
        }
    }
}

private fun lineText(e: LogEntry, time: DateFormat, c: LavColors): AnnotatedString = buildAnnotatedString {
    withStyle(SpanStyle(color = c.textTertiary)) { append(time.format(Date(e.tsMs)) + " ") }
    withStyle(SpanStyle(color = severityColor(e.severity, c))) { append(e.severity.label + " ") }
    if (e.source.isNotEmpty()) withStyle(SpanStyle(color = c.textTertiary)) { append(e.source + " ") }
    withStyle(SpanStyle(color = c.textPrimary)) { append(e.message) }
}

private fun severityColor(s: LogSeverity, c: LavColors): Color = when (s) {
    LogSeverity.Error -> c.errorText
    LogSeverity.Warn -> c.connectingText
    LogSeverity.Info -> c.textSecondary
    LogSeverity.Debug, LogSeverity.Trace -> c.textTertiary
}

/** True when the chooser was opened; false when no app takes plain text or the text was too big for the system. */
private fun shareText(context: android.content.Context, text: String): Boolean {
    val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text)
    return try {
        context.startActivity(Intent.createChooser(send, null))
        true
    } catch (_: ActivityNotFoundException) {
        false
    } catch (_: RuntimeException) {
        // TransactionTooLargeException arrives wrapped in a RuntimeException.
        false
    }
}
