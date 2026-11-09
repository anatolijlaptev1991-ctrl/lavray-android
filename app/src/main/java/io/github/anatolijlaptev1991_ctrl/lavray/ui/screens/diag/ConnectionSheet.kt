// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSheet
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat
import kotlinx.coroutines.launch
import java.text.DateFormat
import java.util.Date

/** Everything the core knows about one connection, with Copy address and (for an active one) Close. */
@Composable
fun ConnectionSheet(row: ConnRow, nowMs: Long, model: ConnectionsModel, onDismiss: () -> Unit) {
    val env = LocalEnv.current
    val context = LocalContext.current
    val resources = LocalResources.current
    val scope = rememberCoroutineScope()
    val fmt = rememberUnitFormat()
    var closing by remember { mutableStateOf(false) }
    val copied = stringResource(R.string.conns_copied)
    val copyFailed = stringResource(R.string.conns_copyFailed)
    val closedText = stringResource(R.string.toast_connClosed)
    val started = remember(row.startedAtMs) { DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.MEDIUM).format(Date(row.startedAtMs)) }

    LavSheet(row.destination, onDismiss) {
        Column(Modifier.semantics(mergeDescendants = false) {}, verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Field(R.string.conns_col_status, stringResource(statusRes(row.status)))
            row.resolved?.let { Field(R.string.conn_d_resolved, it, mono = true) }
            Field(R.string.conns_col_net, row.network.uppercase().ifEmpty { "—" })
            Field(R.string.conns_col_out, row.chain.joinToString(" → ").ifEmpty { row.outbound.ifEmpty { "—" } }, mono = true)
            row.rule?.let { Field(R.string.conns_col_rule, it, mono = true) }
            row.process?.let { Field(R.string.conns_col_app, it, mono = true) }
            row.inbound?.let { Field(R.string.conn_d_inbound, it, mono = true) }
            Field(R.string.conn_d_started, started)
            Field(R.string.conns_col_dur, fmt.clock(row.durationSeconds(nowMs)), mono = true)
            Field(R.string.conns_col_down, fmt.bytes(row.downloadBytes), mono = true)
            Field(R.string.conns_col_up, fmt.bytes(row.uploadBytes), mono = true)
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            LavButton(stringResource(R.string.conns_copy), {
                val ok = copyToClipboard(context, row.address)
                env.toast.show(if (ok) copied else copyFailed)
            }, icon = LavIcon.Copy)
            if (row.status == ConnStatus.Active) {
                LavButton(stringResource(R.string.conns_close), {
                    closing = true
                    scope.launch {
                        when (val r = model.close(row.id)) {
                            is CoreResult.Ok -> env.toast.show(closedText)
                            is CoreResult.Fail -> env.toast.show(resources.getString(r.kind.textRes()))
                        }
                        closing = false
                        onDismiss()
                    }
                }, variant = ButtonVariant.Danger, icon = LavIcon.Close, loading = closing)
            }
        }
    }
}

private fun statusRes(s: ConnStatus): Int = when (s) {
    ConnStatus.Active -> R.string.conns_st_active
    ConnStatus.Closed -> R.string.conns_st_closed
    ConnStatus.Blocked -> R.string.conns_st_blocked
}

@Composable
private fun Field(label: Int, value: String, mono: Boolean = false) {
    Column(Modifier.semantics(mergeDescendants = true) {}) {
        LavText(stringResource(label), style = Lav.type.caption, color = Lav.colors.textTertiary)
        LavText(value, style = if (mono) Lav.type.mono else Lav.type.body)
    }
}

/** Plain-text copy; a device that refuses clipboard access reports failure instead of crashing. */
internal fun copyToClipboard(context: Context, text: String): Boolean = try {
    val cm = context.getSystemService(ClipboardManager::class.java)
    cm?.setPrimaryClip(ClipData.newPlainText("", text)) != null
} catch (_: SecurityException) {
    false
}
