// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.ActivityManager
import android.content.Context
import android.os.Build

/** Why the previous :vpn process ended, from ApplicationExitInfo (API 30+); a stable code for the UI, or null. */
object ExitReasons {
    /** ApplicationExitInfo.REASON_* values (literals: the codes are stable API constants). */
    fun code(reason: Int): String? = when (reason) {
        1 -> "exit_self"
        2 -> "signaled"
        3 -> "low_memory"
        4 -> "crash"
        5 -> "crash_native"
        6 -> "anr"
        7 -> "initialization_failure"
        8 -> "permission_change"
        9 -> "excessive_resource_usage"
        10 -> "user_requested"
        11 -> "user_stopped"
        12 -> "dependency_died"
        14 -> "freezer"
        15 -> "package_updated"
        16 -> "package_state_change"
        else -> null
    }

    /** The reason of the newest recorded exit of this app's :vpn process, or null when unknown or below API 30. */
    fun lastVpnExit(context: Context): String? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return null
        return try {
            val am = context.getSystemService(ActivityManager::class.java)
            am.getHistoricalProcessExitReasons(context.packageName, 0, MAX_RECORDS)
                .filter { it.processName.endsWith(":vpn") }
                .maxByOrNull { it.timestamp }
                ?.let { code(it.reason) }
        } catch (_: RuntimeException) {
            null
        }
    }

    private const val MAX_RECORDS = 8
}
