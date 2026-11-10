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

/** What the boot flow does to the world; the platform's implementation is below, tests bring their own. */
internal interface BootActions {
    fun consentGranted(): Boolean

    /** Starts the service as a boot start. Throws a RuntimeException when the system refuses. */
    fun startService()
    fun notifyPermissionNeeded()
    fun notifyFailure(kind: String, message: String?)
}

/** The decisions of both receivers, free of Android types: one preference read, then start, notify or nothing. */
internal object BootLogic {
    fun run(trigger: BootTrigger, autoConnect: Boolean, intent: TunnelIntent, actions: BootActions) {
        // A wish from before a reboot reads as cleared (the boot count differs): only auto-connect carries it over.
        val want = intent.snapshot.wantConnected
        val wanted = if (trigger == BootTrigger.BOOT) autoConnect else want
        val consent = wanted && actions.consentGranted()
        when (BootPolicy.decide(trigger, autoConnect, want, consent)) {
            BootAction.IGNORE -> Unit
            BootAction.NOTIFY_PERMISSION -> {
                intent.onPermissionMissing()
                actions.notifyPermissionNeeded()
            }
            BootAction.START_SERVICE -> start(intent, actions)
        }
    }

    private fun start(intent: TunnelIntent, actions: BootActions) {
        try {
            intent.onConnectRequested()
            actions.startService()
        } catch (e: RuntimeException) {
            // ForegroundServiceStartNotAllowedException or SecurityException on a restricted device: say so, do not crash.
            intent.onGaveUp(FailureKind.RESTRICTED_START, e.message, wasEstablished = false)
            actions.notifyFailure(FailureKind.RESTRICTED_START, e.message)
        }
    }
}

internal object BootFlow {
    private const val TAG = "LavrayBoot"

    fun handle(context: Context, trigger: BootTrigger) {
        val prefs = VpnPrefs(context)
        val setting = BootComponents.setting(context, prefs)
        val outcome = setting.reconcile()
        if (outcome == SwitchOutcome.FAILED_STILL_DISABLED || outcome == SwitchOutcome.FAILED_STAYS_ENABLED) {
            Log.w(TAG, "boot receiver switch failed: $outcome")
        }
        val intent = TunnelIntent(prefs) { BootCount.read(context) }
        BootLogic.run(trigger, prefs.autoConnect, intent, object : BootActions {
            // Calling prepare() from a receiver is not described by the docs; treated as "no consent" on failure.
            // It can make this app the prepared VPN package when another one is; a start follows, so that is acceptable.
            override fun consentGranted() = runCatching { VpnService.prepare(context) == null }.getOrDefault(false)

            override fun startService() {
                context.startForegroundService(
                    Intent(context, LavrayVpnService::class.java).putExtra(StartIntent.EXTRA_SOURCE, StartSource.BOOT.wire),
                )
            }

            override fun notifyPermissionNeeded() = VpnNotifications.postPermissionNeeded(context)

            override fun notifyFailure(kind: String, message: String?) = VpnNotifications.postFailure(context, kind, message)
        })
    }
}

/** The platform's switch of the boot receiver component, and the one lock-protected setting that drives it. */
internal object BootComponents {
    fun setting(context: Context, prefs: VpnPrefs): AutoConnectSetting =
        AutoConnectSetting({ prefs.autoConnect }, { prefs.setAutoConnect(it) }, BootReceiverSwitch(portFor(context)))

    private fun portFor(context: Context): BootSwitchPort {
        val pm = context.packageManager
        val component = ComponentName(context, BootReceiver::class.java)
        return object : BootSwitchPort {
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
    }
}
