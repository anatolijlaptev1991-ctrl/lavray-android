// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import android.app.NotificationManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone

/** What the notices can do: [onConnect] runs the consent flow and connects, the others close one notice. */
class NoticeActions(
    val onConnect: () -> Unit,
    val onDismissStop: () -> Unit,
    val onDismissFailure: (String) -> Unit,
    val onDismissWarnings: () -> Unit,
)

/**
 * The service's own words on what happened, in the app: a missing VPN permission, a tunnel that stopped by itself, a
 * protection that switched off, the notes of the core. The service does not post its notification while the app is
 * open, so for a person looking at the screen this is the only place that tells them.
 */
@Composable
fun ServiceNoticeBanners(notices: ServiceNotices, actions: NoticeActions) {
    val any = notices.needsPermission || notices.stop != null || notices.silentFailure != null || notices.warnings.isNotEmpty()
    if (!any) return
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (notices.needsPermission) PermissionBanner(actions.onConnect)
        notices.stop?.let { StopBanner(it, actions) }
        notices.silentFailure?.let { SilentFailureBanner(it, actions) }
        if (notices.warnings.isNotEmpty()) WarningsBanner(notices.warnings, actions.onDismissWarnings)
    }
}

@Composable
private fun PermissionBanner(onAllow: () -> Unit) {
    LavBanner(
        StatusTone.Error, stringResource(R.string.nt_perm_t), text = stringResource(R.string.nt_perm_d),
        actions = { LavButton(stringResource(R.string.nt_perm_go), onAllow, variant = ButtonVariant.Primary) },
    )
}

@Composable
private fun StopBanner(stop: StopNotice, actions: NoticeActions) {
    val lines = buildList {
        add(stringResource(stop.reason))
        add(stringResource(R.string.nt_stop_do))
        if (stop.tip) add(stringResource(R.string.nt_stop_tip))
        // The system notification is how this would normally have been shown; with it blocked, the screen is all there is.
        if (stop.notificationsOff) add(stringResource(R.string.nt_stop_nonotif))
    }
    LavBanner(
        StatusTone.Error, stringResource(R.string.nt_stop_t), text = lines.joinToString("\n"),
        actions = {
            LavButton(stringResource(R.string.nt_reconnect), actions.onConnect, variant = ButtonVariant.Primary)
            LavButton(stringResource(R.string.imp_clipHide), actions.onDismissStop, variant = ButtonVariant.Ghost)
        },
    )
}

@Composable
private fun SilentFailureBanner(failure: SilentFailure, actions: NoticeActions) {
    LavBanner(
        StatusTone.Error, stringResource(failure.title), text = stringResource(failure.body),
        actions = {
            LavButton(stringResource(R.string.nt_reconnect), actions.onConnect, variant = ButtonVariant.Primary)
            LavButton(stringResource(R.string.imp_clipHide), { actions.onDismissFailure(failure.kind) }, variant = ButtonVariant.Ghost)
        },
    )
}

@Composable
private fun WarningsBanner(notes: List<WarningNote>, onDismiss: () -> Unit) {
    val lines = notes.map { note -> if (note.code != null) stringResource(note.text, note.code) else stringResource(note.text) }
    LavBanner(
        StatusTone.Neutral, stringResource(R.string.nt_warn_t), text = lines.joinToString("\n"),
        actions = { LavButton(stringResource(R.string.imp_clipHide), onDismiss, variant = ButtonVariant.Ghost) },
    )
}

/** Whether the system lets the app post notifications; read again each time the screen comes back (the person may have changed it). */
@Composable
fun rememberNotificationsOn(): Boolean {
    val context = LocalContext.current
    var on by remember { mutableStateOf(notificationsOn(context)) }
    LifecycleResumeEffect(Unit) {
        on = notificationsOn(context)
        onPauseOrDispose { }
    }
    return on
}

private fun notificationsOn(context: Context): Boolean =
    context.getSystemService(NotificationManager::class.java)?.areNotificationsEnabled() != false
