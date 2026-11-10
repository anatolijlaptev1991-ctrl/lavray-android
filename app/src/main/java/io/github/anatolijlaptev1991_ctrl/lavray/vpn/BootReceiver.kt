// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.content.BroadcastReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.util.Log
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind

/**
 * BOOT_COMPLETED and the vendors' QUICKBOOT_POWERON, in the :vpn process. Switched off while "connect at phone
 * start" is off (see [BootComponents]), so a default install spawns no process at boot.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val trigger = BootTrigger.fromAction(intent.action)
        if (trigger != BootTrigger.BOOT) return
        BootFlow.handle(context, trigger)
    }
}

/**
 * MY_PACKAGE_REPLACED. Always enabled: after an update the old process is gone and so is the tunnel. It brings the
 * tunnel back if it was wanted, and re-aligns the boot receiver with the preference (an install made before the
 * receiver could be switched off starts with it on).
 */
class UpdateReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val trigger = BootTrigger.fromAction(intent.action)
        if (trigger != BootTrigger.UPDATE) return
        BootFlow.handle(context, trigger)
    }
}

/** What both receivers do: one preference read, then start the service, post "permission needed", or nothing. */
internal object BootFlow {
    private const val TAG = "LavrayBoot"

    fun handle(context: Context, trigger: BootTrigger) {
        val prefs = VpnPrefs(context)
        val intent = TunnelIntent(prefs)
        val autoConnect = prefs.autoConnect
        BootComponents.reconcile(context, autoConnect)
        if (BootPolicy.dropsOldIntent(trigger, autoConnect)) intent.onBootWithoutAutoConnect()
        val want = intent.snapshot.wantConnected
        val wanted = if (trigger == BootTrigger.BOOT) autoConnect else want
        // Calling prepare() from a receiver is not described by the docs; treated as "no consent" on failure.
        // It can make this app the prepared VPN package when another one is; a start follows, so that is acceptable.
        val consent = wanted && runCatching { VpnService.prepare(context) == null }.getOrDefault(false)
        when (BootPolicy.decide(trigger, autoConnect, want, consent)) {
            BootAction.IGNORE -> Unit
            BootAction.NOTIFY_PERMISSION -> {
                intent.onPermissionMissing()
                VpnNotifications.postPermissionNeeded(context)
            }
            BootAction.START_SERVICE -> start(context, intent)
        }
    }

    private fun start(context: Context, intent: TunnelIntent) {
        try {
            intent.onConnectRequested()
            context.startForegroundService(
                Intent(context, LavrayVpnService::class.java).putExtra(StartIntent.EXTRA_SOURCE, StartSource.BOOT.wire),
            )
        } catch (e: RuntimeException) {
            // ForegroundServiceStartNotAllowedException or SecurityException on a restricted device: say so, do not crash.
            Log.w(TAG, "service start refused: ${e.javaClass.simpleName}")
            intent.onGaveUp(FailureKind.RESTRICTED_START, e.message, wasEstablished = false)
            VpnNotifications.postFailure(context, FailureKind.RESTRICTED_START, e.message)
        }
    }
}

/** The platform's switch of the boot receiver component. */
internal object BootComponents {
    /** Aligns the receiver with the preference; never throws. The fallback of every failure is "receiver stays on". */
    fun reconcile(context: Context, autoConnect: Boolean): SwitchOutcome {
        val pm = context.packageManager
        val component = ComponentName(context, BootReceiver::class.java)
        val port = object : BootSwitchPort {
            override fun current(): BootSwitch? = try {
                when (pm.getComponentEnabledSetting(component)) {
                    PackageManager.COMPONENT_ENABLED_STATE_DISABLED,
                    PackageManager.COMPONENT_ENABLED_STATE_DISABLED_USER,
                    PackageManager.COMPONENT_ENABLED_STATE_DISABLED_UNTIL_USED,
                    -> BootSwitch.DISABLED
                    else -> BootSwitch.DEFAULT
                }
            } catch (_: RuntimeException) {
                null
            }

            override fun set(target: BootSwitch) {
                val state = if (target == BootSwitch.DISABLED) {
                    PackageManager.COMPONENT_ENABLED_STATE_DISABLED
                } else {
                    PackageManager.COMPONENT_ENABLED_STATE_DEFAULT
                }
                pm.setComponentEnabledSetting(component, state, PackageManager.DONT_KILL_APP)
            }
        }
        val outcome = BootReceiverSwitch(port).reconcile(autoConnect)
        if (outcome == SwitchOutcome.FAILED_STAYS_ENABLED || outcome == SwitchOutcome.FAILED_STILL_DISABLED) {
            Log.w("LavrayBoot", "boot receiver switch failed: $outcome")
        }
        return outcome
    }
}
