// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.PersistableBundle
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavConfirmDialog
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSheet
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import kotlinx.coroutines.launch

/** Which sheet or dialog the subscriptions screen shows. One at a time; null means none. */
sealed interface SubSheet {
    data object Add : SubSheet
    data class Settings(val row: SubRow) : SubSheet
    data class Menu(val row: SubRow) : SubSheet
    data class ConfirmDelete(val row: SubRow) : SubSheet
    data class ConfirmRollback(val row: SubRow) : SubSheet
}

/** Everything the sheets need from the screen. */
class SubSheetEnv(
    val model: SubscriptionsModel,
    val show: (SubSheet?) -> Unit,
    val onCopied: () -> Unit,
)

@Composable
fun SubscriptionSheets(sheet: SubSheet?, confirm: PendingConfirm?, env: SubSheetEnv) {
    val close = { env.show(null) }
    when (sheet) {
        null -> Unit
        SubSheet.Add -> AddSheet(env, close)
        is SubSheet.Settings -> SettingsSheet(sheet.row, env, close)
        is SubSheet.Menu -> MenuSheet(sheet.row, env, close)
        is SubSheet.ConfirmDelete -> DeleteDialog(sheet.row, env, close)
        is SubSheet.ConfirmRollback -> RollbackDialog(sheet.row, env, close)
    }
    if (confirm != null) UpdateConfirmDialog(confirm, env.model)
}

// ---- add

@Composable
private fun AddSheet(env: SubSheetEnv, close: () -> Unit) {
    var url by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Int?>(null) }
    val scope = rememberCoroutineScope()
    LavSheet(stringResource(R.string.subs_add), close) {
        LavTextField(
            url, { url = it; error = null }, label = stringResource(R.string.imp_link), placeholder = LINK_HINT,
            mono = true, error = error?.let { stringResource(it) },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
        )
        LavTextField(name, { name = it }, label = stringResource(R.string.imp_name))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
            LavButton(stringResource(R.string.common_cancel), close, variant = ButtonVariant.Ghost)
            LavButton(
                stringResource(R.string.imp_do),
                {
                    if (url.isBlank()) {
                        error = R.string.sub_err_notFound
                    } else {
                        busy = true
                        scope.launch {
                            when (val r = env.model.create(name, url)) {
                                is CoreResult.Ok -> close()
                                is CoreResult.Fail -> { error = r.kind.textRes(); busy = false }
                            }
                        }
                    }
                },
                variant = ButtonVariant.Primary, loading = busy,
            )
        }
    }
}

// ---- settings

@Composable
private fun SettingsSheet(row: SubRow, env: SubSheetEnv, close: () -> Unit) {
    var form by remember { mutableStateOf(EditForm.from(row)) }
    var shared by remember { mutableStateOf<SharedSubSettings?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<ErrorKind?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(row.id) {
        when (val r = env.model.loadShared()) {
            is CoreResult.Ok -> { shared = r.value; if (!form.hwidTouched) form = form.copy(hwid = r.value.hwid) }
            is CoreResult.Fail -> error = r.kind
        }
    }
    LavSheet(stringResource(R.string.sub_settings), close) {
        error?.let { LavText(stringResource(it.textRes()), style = Lav.type.small, color = Lav.colors.errorText) }
        LavTextField(
            form.name, { form = form.copy(name = it) }, label = stringResource(R.string.sa_name),
            error = if (form.name.isBlank()) stringResource(R.string.rt_nameEmpty) else null,
        )
        LavTextField(
            form.url, { form = form.copy(url = it) }, label = stringResource(R.string.imp_link), mono = true,
            error = if (form.url.isNotBlank() && !SubscriptionLogic.isWebLink(form.url)) stringResource(R.string.sub_url_web) else null,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
        )
        IntervalPicker(form) { form = it }
        UaPicker(form) { form = it }
        LavText(stringResource(R.string.sub_set_global), style = Lav.type.small, color = Lav.colors.textSecondary)
        LavSwitchRow(
            stringResource(R.string.sub_set_hwid), form.hwid, { form = form.copy(hwid = it, hwidTouched = true) },
            hint = stringResource(R.string.sub_hwid_hint), enabled = shared != null,
        )
        LavSwitchRow(
            stringResource(R.string.sub_set_tunnel), shared?.tunnel ?: false, {},
            hint = stringResource(R.string.sa_tunnel_unavailable), enabled = false,
        )
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
            LavButton(stringResource(R.string.common_cancel), close, variant = ButtonVariant.Ghost)
            LavButton(
                stringResource(R.string.common_save),
                {
                    busy = true
                    scope.launch {
                        when (val r = env.model.save(row, SubscriptionEdit.plan(row, form))) {
                            is CoreResult.Ok -> close()
                            is CoreResult.Fail -> { error = r.kind; busy = false }
                        }
                    }
                },
                variant = ButtonVariant.Primary, loading = busy, enabled = form.valid,
            )
        }
    }
}

@Composable
private fun IntervalPicker(form: EditForm, onChange: (EditForm) -> Unit) {
    LavText(stringResource(R.string.sub_set_interval), style = Lav.type.small, color = Lav.colors.textSecondary)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        SubscriptionLogic.HOURS.forEach { h ->
            LavChip(
                pluralStringResource(R.plurals.pl_h, h, h.toString()), form.hours == h,
                { onChange(form.copy(hours = h, intervalTouched = true)) },
            )
        }
    }
}

@Composable
private fun UaPicker(form: EditForm, onChange: (EditForm) -> Unit) {
    LavText(stringResource(R.string.sub_set_ua), style = Lav.type.small, color = Lav.colors.textSecondary)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        UaPreset.entries.forEach { p ->
            LavChip(uaLabel(p), form.ua == p, { onChange(form.copy(ua = p, uaTouched = true)) })
        }
    }
    if (form.ua == UaPreset.Custom) {
        LavTextField(
            form.uaCustom, { onChange(form.copy(uaCustom = it.take(SubscriptionLogic.UA_MAX), uaTouched = true)) },
            label = stringResource(R.string.sub_set_uaField), mono = true,
        )
    }
}

@Composable
private fun uaLabel(p: UaPreset): String = when (p) {
    UaPreset.Default -> stringResource(R.string.sub_set_uaDefault)
    UaPreset.Clash -> "Clash Meta"
    UaPreset.V2rayN -> "v2rayN"
    UaPreset.Custom -> stringResource(R.string.sub_set_uaCustom)
}

// ---- menu and confirmations

@Composable
private fun MenuRow(icon: LavIcon, text: String, onClick: () -> Unit, danger: Boolean = false) {
    val c = Lav.colors
    LavListRow(
        title = text, onClick = onClick,
        leading = { LavIconView(icon, if (danger) c.errorText else c.textSecondary, size = 20.dp) },
    )
}

@Composable
private fun MenuSheet(row: SubRow, env: SubSheetEnv, close: () -> Unit) {
    val context = LocalContext.current
    LavSheet(row.name, close) {
        MenuRow(LavIcon.Refresh, stringResource(R.string.subs_rollback), { env.show(SubSheet.ConfirmRollback(row)) })
        if (row.url.isNotEmpty()) {
            MenuRow(LavIcon.Copy, stringResource(R.string.common_copyLink), { close(); copyLink(context, row.url); env.onCopied() })
            if (SubscriptionLogic.isWebLink(row.url)) {
                MenuRow(LavIcon.Link, stringResource(R.string.common_open), { close(); openLink(context, row.url) })
            }
        }
        MenuRow(LavIcon.Trash, stringResource(R.string.subs_delete), { env.show(SubSheet.ConfirmDelete(row)) }, danger = true)
    }
}

@Composable
private fun DeleteDialog(row: SubRow, env: SubSheetEnv, close: () -> Unit) {
    LavConfirmDialog(
        title = stringResource(R.string.subs_delete),
        // Placeholders {n} and {name} are numbered alphabetically: n first.
        text = stringResource(R.string.subs_delConfirm, row.count.toString(), row.name),
        confirmLabel = stringResource(R.string.common_delete),
        dismissLabel = stringResource(R.string.common_cancel),
        onConfirm = { close(); env.model.delete(row) },
        onDismiss = close,
        danger = true,
    )
}

@Composable
private fun RollbackDialog(row: SubRow, env: SubSheetEnv, close: () -> Unit) {
    LavConfirmDialog(
        title = stringResource(R.string.subs_rollback_t, row.name),
        text = stringResource(R.string.subs_rollback_d),
        confirmLabel = stringResource(R.string.subs_rollback),
        dismissLabel = stringResource(R.string.common_cancel),
        onConfirm = { close(); env.model.rollback(row) },
        onDismiss = close,
        danger = true,
    )
}

/** "The update will remove N of M servers": goes on only after the person agrees. */
@Composable
private fun UpdateConfirmDialog(ask: PendingConfirm, model: SubscriptionsModel) {
    LavConfirmDialog(
        title = stringResource(R.string.sub_confirm_t, ask.name),
        text = pluralStringResource(R.plurals.pl_subDel, ask.total, ask.remove.toString(), ask.total.toString()),
        confirmLabel = stringResource(R.string.common_refresh),
        dismissLabel = stringResource(R.string.common_cancel),
        onConfirm = { model.acceptConfirm(ask) },
        onDismiss = { model.dismissConfirm() },
        danger = true,
    )
}

private const val LINK_HINT = "https://provider.example/sub"

/** Copies the link as sensitive content, so keyboards and the clipboard preview do not show the token (API 33+). */
private fun copyLink(context: Context, link: String) {
    val clip = ClipData.newPlainText("link", link)
    clip.description.extras = PersistableBundle().apply { putBoolean("android.content.extra.IS_SENSITIVE", true) }
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager)?.setPrimaryClip(clip)
}

private fun openLink(context: Context, link: String) {
    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(link)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    try {
        context.startActivity(intent)
    } catch (_: ActivityNotFoundException) {
        // No browser installed: nothing to open the link with, and the copy action still works.
    }
}
