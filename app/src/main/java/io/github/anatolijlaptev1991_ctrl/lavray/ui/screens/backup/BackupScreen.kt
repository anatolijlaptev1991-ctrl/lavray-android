// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.BackupSummary
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavConfirmDialog
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.hasTunnel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.findActivity
import kotlinx.coroutines.launch
import java.text.DateFormat
import java.util.Date

/**
 * Backup (desktop BackupExportDialog and BackupRestoreDialog): an encrypted file of all data, saved and opened through
 * the system file picker, so the app needs no storage permission. Restoring checks the file first, shows what is in it,
 * warns that everything is replaced, and tells the person to close and reopen the app afterwards.
 */
@Composable
fun BackupScreen() {
    val env = LocalEnv.current
    // The model belongs to the app, not to this screen: a restore that is running goes on if the person leaves, and its
    // result (and the "close the app" card) is still there when they come back.
    val model = env.backup
    val state by model.state.collectAsStateWithLifecycle()
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    val tunnelUp = phaseOf(vpn.state).hasTunnel
    DisposableEffect(model) { onDispose { model.release() } }

    LavScreenColumn {
        state.error?.let { LavBanner(StatusTone.Error, stringResource(it)) }
        ExportCard(model, state)
        if (state.restored) RestoredCard(state.needsRestart) else RestoreCard(model, state, tunnelUp)
    }
}

@Composable
private fun ExportCard(model: BackupModel, state: BackupState) {
    val env = LocalEnv.current
    val scope = rememberCoroutineScope()
    var pw by remember { mutableStateOf("") }
    var repeat by remember { mutableStateOf("") }
    var ack by remember { mutableStateOf(false) }
    val savedText = stringResource(R.string.bk_saved)
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument(MIME_BINARY)) { uri: Uri? ->
        if (uri == null) {
            model.cancelExport()
        } else {
            scope.launch {
                model.saveTo(uri)
                if (model.state.value.saved) {
                    pw = ""; repeat = ""; ack = false
                    env.toast.show(savedText)
                }
            }
        }
    }
    LaunchedEffect(state.exportName) { state.exportName?.let { launcher.launch(it) } }

    val issue = BackupRules.issue(pw)
    val matches = BackupRules.same(pw, repeat)
    val pwError = when {
        pw.isEmpty() -> null
        issue == PasswordIssue.Short -> stringResource(R.string.bk_pwShort, BackupRules.MIN_PASSWORD_CHARS.toString())
        issue == PasswordIssue.Long -> stringResource(R.string.bk_pwLong)
        else -> null
    }
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            LavText(stringResource(R.string.bk_exportTitle), style = Lav.type.h2)
            LavText(stringResource(R.string.bk_hint), style = Lav.type.small, color = Lav.colors.textSecondary)
            LavText(stringResource(R.string.bk_notIncluded), style = Lav.type.caption, color = Lav.colors.textTertiary)
            PasswordField(pw, { pw = it }, stringResource(R.string.bk_pw), pwError)
            PasswordField(repeat, { repeat = it }, stringResource(R.string.bk_pw2), if (repeat.isNotEmpty() && !matches) stringResource(R.string.bk_mismatch) else null)
            LavText(stringResource(R.string.bk_pwHint, BackupRules.MIN_PASSWORD_CHARS.toString()), style = Lav.type.caption, color = Lav.colors.textTertiary)
            LavSwitchRow(stringResource(R.string.bk_ack), ack, { ack = it })
            LavButton(
                stringResource(R.string.bk_exportBtn), { scope.launch { model.createBackup(pw) } },
                variant = ButtonVariant.Primary, icon = LavIcon.Download, loading = state.busy && state.exportName == null,
                enabled = issue == null && matches && ack && !state.busy,
            )
        }
    }
}

@Composable
private fun RestoreCard(model: BackupModel, state: BackupState, tunnelUp: Boolean) {
    val env = LocalEnv.current
    val scope = rememberCoroutineScope()
    var pw by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri != null) scope.launch { model.pick(uri) }
    }
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            LavText(stringResource(R.string.bk_restoreTitle), style = Lav.type.h2)
            LavText(stringResource(R.string.bk_restoreD), style = Lav.type.small, color = Lav.colors.textSecondary)
            LavBanner(StatusTone.Connecting, stringResource(R.string.bk_restart_warn_t), text = stringResource(R.string.bk_restart_warn_d))
            if (tunnelUp) LavBanner(StatusTone.Error, stringResource(R.string.bk_err_connected))
            LavButton(stringResource(R.string.bk_chooseFile), { picker.launch(arrayOf("*/*")) }, icon = LavIcon.File)
            LavText(state.pickedName ?: stringResource(R.string.bk_noFile), style = Lav.type.mono, color = Lav.colors.textSecondary)
            if (state.pickedName != null) {
                PasswordField(pw, { pw = it; model.invalidateCheck() }, stringResource(R.string.bk_pwRestore), null)
                if (state.summary == null) {
                    LavButton(
                        stringResource(if (state.busy) R.string.bk_checking else R.string.bk_check), { scope.launch { model.check(pw) } },
                        variant = ButtonVariant.Primary, loading = state.busy, enabled = pw.isNotEmpty() && !state.busy,
                    )
                }
            }
            state.summary?.let { Summary(it) }
            if (state.summary != null) {
                LavBanner(StatusTone.Connecting, stringResource(R.string.bk_replaceWarn))
                LavButton(
                    stringResource(if (state.busy) R.string.bk_restoring else R.string.bk_replaceBtn), { confirm = true },
                    variant = ButtonVariant.Danger, loading = state.busy, enabled = !tunnelUp && !state.busy,
                )
            }
        }
    }
    if (confirm) {
        LavConfirmDialog(
            title = stringResource(R.string.bk_confirmTitle),
            text = stringResource(R.string.bk_confirm_phone) + "\n\n" + stringResource(R.string.bk_restart_warn_d),
            confirmLabel = stringResource(R.string.bk_confirmOk), dismissLabel = stringResource(R.string.common_cancel),
            // On the app's scope: leaving the screen must not cancel a restore the core has already begun.
            onConfirm = { confirm = false; env.scope.launch { model.replace(pw) }; pw = "" }, onDismiss = { confirm = false }, danger = true,
        )
    }
}

@Composable
private fun RestoredCard(needsRestart: Boolean) {
    val context = LocalContext.current
    LavCard(Modifier.fillMaxWidth(), strong = true) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            LavText(stringResource(R.string.bk_done), style = Lav.type.h2)
            LavBanner(
                StatusTone.Connecting, stringResource(R.string.bk_restart_after_t),
                text = stringResource(if (needsRestart) R.string.bk_doneRestart else R.string.bk_restart_after_d),
            )
            LavButton(stringResource(R.string.bk_close_app), { context.findActivity()?.finishAffinity() }, variant = ButtonVariant.Primary)
        }
    }
}

@Composable
private fun Summary(s: BackupSummary) {
    val date = remember(s.createdAtMs) { DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(s.createdAtMs)) }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        LavText(stringResource(R.string.bk_sumTitle), style = Lav.type.bodyStrong)
        SummaryLine(R.string.bk_sumGroups, s.groups.toString())
        SummaryLine(R.string.bk_sumProfiles, s.profiles.toString())
        SummaryLine(R.string.bk_sumSubs, s.subscriptions.toString())
        SummaryLine(R.string.bk_sumPresets, s.presets.toString())
        SummaryLine(R.string.bk_sumSettings, s.settings.toString())
        SummaryLine(R.string.bk_sumVersion, s.appVersion.ifBlank { "—" })
        if (s.createdAtMs > 0) SummaryLine(R.string.bk_sumDate, date)
    }
}

@Composable
private fun SummaryLine(label: Int, value: String) {
    androidx.compose.foundation.layout.Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        LavText(stringResource(label), style = Lav.type.small, color = Lav.colors.textSecondary)
        LavText(value, style = Lav.type.mono)
    }
}

@Composable
private fun PasswordField(value: String, onChange: (String) -> Unit, label: String, error: String?) {
    LavTextField(
        value, onChange, label = label, error = error,
        visualTransformation = PasswordVisualTransformation(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
    )
}

private const val MIME_BINARY = "application/octet-stream"
