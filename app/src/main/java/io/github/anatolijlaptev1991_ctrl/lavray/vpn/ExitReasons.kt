// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.ActivityManager
import android.app.ApplicationExitInfo
import android.content.Context
import android.os.Build
import android.os.SystemClock

/** One entry of ActivityManager.getHistoricalProcessExitReasons, reduced to what the selection needs. */
data class ExitRecord(val processName: String, val timestampMs: Long, val reason: Int)

/**
 * Why the previous :vpn process ended, from ApplicationExitInfo (API 30+). INFORMATION ONLY: the code goes into
 * `stopReason` for the UI. No decision of the service depends on it, because the record cannot be trusted to tell what
 * the person meant: before API 34 an update kill is recorded as REASON_USER_REQUESTED, the same as "Force stop" and as
 * swiping the app out of Recents, which an OEM cleaner does to a running VPN too. Reading "user_requested" as "the person
 * wants the tunnel off" would silently leave the phone unprotected; reading it as a death only costs a banner.
 */
object ExitReasons {
    /** Stable codes for the UI. Constants are the platform's, so a wrong number cannot hide behind a literal. */
    fun code(reason: Int): String? = when (reason) {
        ApplicationExitInfo.REASON_EXIT_SELF -> "exit_self"
        ApplicationExitInfo.REASON_SIGNALED -> "signaled"
        ApplicationExitInfo.REASON_LOW_MEMORY -> "low_memory"
        ApplicationExitInfo.REASON_CRASH -> "crash"
        ApplicationExitInfo.REASON_CRASH_NATIVE -> "crash_native"
        ApplicationExitInfo.REASON_ANR -> "anr"
        ApplicationExitInfo.REASON_INITIALIZATION_FAILURE -> "initialization_failure"
        ApplicationExitInfo.REASON_PERMISSION_CHANGE -> "permission_change"
        ApplicationExitInfo.REASON_EXCESSIVE_RESOURCE_USAGE -> "excessive_resource_usage"
        ApplicationExitInfo.REASON_USER_REQUESTED -> "user_requested"
        ApplicationExitInfo.REASON_USER_STOPPED -> "user_stopped"
        ApplicationExitInfo.REASON_DEPENDENCY_DIED -> "dependency_died"
        ApplicationExitInfo.REASON_OTHER -> "other"
        ApplicationExitInfo.REASON_FREEZER -> "freezer"
        ApplicationExitInfo.REASON_PACKAGE_STATE_CHANGE -> "package_state_change"
        ApplicationExitInfo.REASON_PACKAGE_UPDATED -> "package_updated"
        // Added after the compile SDK (REASON_MEMORY_LIMITER, REASON_ANOMALY); the values are the platform's.
        REASON_MEMORY_LIMITER -> "memory_limiter"
        REASON_ANOMALY -> "anomaly"
        else -> null
    }

    /**
     * The newest record of [processName] that ended in THIS boot (not before [notBeforeMs]), or null. An older record
     * cannot explain a death of the current boot, and the wish it would be compared with belongs to this boot only; a
     * missing record leaves the reason unknown, which is the safe outcome.
     */
    fun latest(records: List<ExitRecord>, processName: String, notBeforeMs: Long): ExitRecord? =
        records.filter { it.processName == processName && it.timestampMs >= notBeforeMs }.maxByOrNull { it.timestampMs }

    /** The code of the newest :vpn exit of this boot, or null when unknown or below API 30. */
    fun lastVpnExit(context: Context): String? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return null
        return try {
            val am = context.getSystemService(ActivityManager::class.java)
            val records = am.getHistoricalProcessExitReasons(context.packageName, 0, MAX_RECORDS)
                .map { ExitRecord(it.processName, it.timestamp, it.reason) }
            // The start of this boot in wall-clock time, with a minute of slack; a clock that jumped can only hide a record.
            val bootStartMs = System.currentTimeMillis() - SystemClock.elapsedRealtime() - BOOT_SLACK_MS
            latest(records, context.packageName + ":vpn", bootStartMs)?.let { code(it.reason) }
        } catch (_: RuntimeException) {
            null
        }
    }

    private const val REASON_MEMORY_LIMITER = 17
    private const val REASON_ANOMALY = 18
    private const val MAX_RECORDS = 16
    private const val BOOT_SLACK_MS = 60_000L
}
