// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.content.Context

/**
 * Settings and tunnel state owned by the :vpn process (the boot receiver reads them before any UI exists).
 * Every write is commit(), not apply(): the process may be killed right after, and a lost write would mean
 * no auto-connect after the next reboot, or a tunnel that nobody restores, with no visible sign.
 */
class VpnPrefs(context: Context) : TunnelStore {
    private val prefs = context.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    val autoConnect: Boolean get() = prefs.getBoolean(KEY_AUTO_CONNECT, false)

    /** Writes the setting; false when the write did not reach storage. */
    fun setAutoConnect(value: Boolean): Boolean = prefs.edit().putBoolean(KEY_AUTO_CONNECT, value).commit()

    /** Per-app choice as JSON (see [AppSelection]); empty means all apps. */
    var appSelection: AppSelection
        get() = AppSelection.parse(prefs.getString(KEY_APPS, null))
        set(value) {
            prefs.edit().putString(KEY_APPS, value.toJson()).commit()
        }


    override fun load(): PersistedTunnel = PersistedTunnel(
        wantConnected = prefs.getBoolean(KEY_WANT, false),
        unexpectedStop = prefs.getBoolean(KEY_UNEXPECTED, false),
        stopReason = prefs.getString(KEY_STOP_REASON, null),
        needsPermission = prefs.getBoolean(KEY_NEEDS_PERMISSION, false),
        lastFailure = prefs.getString(KEY_LAST_FAILURE, null),
        failureKind = prefs.getString(KEY_FAILURE_KIND, null),
        failureReason = prefs.getString(KEY_FAILURE_REASON, null),
        resumeAttempts = prefs.getInt(KEY_RESUME_ATTEMPTS, 0),
        bootCount = prefs.getInt(KEY_BOOT_COUNT, -1),
        bootId = prefs.getString(KEY_BOOT_ID, null),
    )

    override fun save(state: PersistedTunnel) {
        prefs.edit()
            .putBoolean(KEY_WANT, state.wantConnected)
            .putBoolean(KEY_UNEXPECTED, state.unexpectedStop)
            .putString(KEY_STOP_REASON, state.stopReason)
            .putBoolean(KEY_NEEDS_PERMISSION, state.needsPermission)
            .putString(KEY_LAST_FAILURE, state.lastFailure)
            .putString(KEY_FAILURE_KIND, state.failureKind)
            .putString(KEY_FAILURE_REASON, state.failureReason)
            .putInt(KEY_RESUME_ATTEMPTS, state.resumeAttempts)
            .putInt(KEY_BOOT_COUNT, state.bootCount)
            .putString(KEY_BOOT_ID, state.bootId)
            .commit()
    }

    private companion object Constants {
        const val FILE = "vpn"
        const val KEY_APPS = "app_selection"
        const val KEY_AUTO_CONNECT = "auto_connect"
        const val KEY_WANT = "want_connected"
        const val KEY_UNEXPECTED = "unexpected_stop"
        const val KEY_STOP_REASON = "stop_reason"
        const val KEY_NEEDS_PERMISSION = "needs_permission"
        const val KEY_LAST_FAILURE = "last_failure"
        const val KEY_FAILURE_KIND = "failure_kind"
        const val KEY_FAILURE_REASON = "failure_reason"
        const val KEY_RESUME_ATTEMPTS = "resume_attempts"
        const val KEY_BOOT_COUNT = "boot_count"
        const val KEY_BOOT_ID = "boot_id"
    }
}

/** Settings.Global.BOOT_COUNT of this boot, or -1 when the system does not say. */
object BootCount {
    fun read(context: Context): Int = try {
        android.provider.Settings.Global.getInt(context.contentResolver, android.provider.Settings.Global.BOOT_COUNT, -1)
    } catch (_: RuntimeException) {
        -1
    }
}

/** The kernel's id of this boot, or null when the file cannot be read; stable for the life of the process. */
object BootId {
    val current: String? by lazy {
        try {
            java.io.File("/proc/sys/kernel/random/boot_id").readText().trim().takeIf { it.isNotEmpty() }
        } catch (_: java.io.IOException) {
            null
        } catch (_: SecurityException) {
            null
        }
    }
}
