// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

enum class WriteResult {
    SAVED,

    /** The preference did not reach storage: nothing changed. */
    NOT_SAVED,

    /** The receiver could not be switched on: the preference was rolled back, so the switch must not show "on". */
    RECEIVER_FAILED,

    /**
     * The receiver could not be switched on and the rollback write failed too: the stored value is ON while the receiver
     * may be off. The caller must keep reconciling (the service does), and the switch must not claim success.
     */
    ROLLBACK_FAILED,
}

/**
 * The auto-connect setting and the boot receiver, always changed together under one lock, so a toggle that arrives while
 * another reconcile runs cannot leave the preference on and the receiver off.
 */
class AutoConnectSetting(
    private val read: () -> Boolean,
    private val write: (Boolean) -> Boolean,
    private val switch: BootReceiverSwitch,
) {
    /** Writes [enabled] and aligns the receiver; a failed write reaches the caller. */
    fun apply(enabled: Boolean): WriteResult = synchronized(LOCK) {
        val previous = read()
        if (!write(enabled)) return WriteResult.NOT_SAVED
        val outcome = switch.reconcile(enabled)
        if (enabled && outcome == SwitchOutcome.FAILED_STILL_DISABLED) {
            return if (write(previous)) WriteResult.RECEIVER_FAILED else WriteResult.ROLLBACK_FAILED
        }
        WriteResult.SAVED
    }

    /** Aligns the receiver with the stored preference (service start, boot, update). */
    fun reconcile(): SwitchOutcome = synchronized(LOCK) { switch.reconcile(read()) }

    private companion object {
        val LOCK = Any()
    }
}

/** When to try again after a switch that was refused. */
object SwitchRetry {
    private val DELAYS_MS = longArrayOf(2_000, 10_000, 60_000)

    /** Delay before retry number [attempt] (0-based), or null when it is time to stop. Only a failed enable is retried. */
    fun delayMs(outcome: SwitchOutcome, attempt: Int): Long? =
        if (outcome == SwitchOutcome.FAILED_STILL_DISABLED) DELAYS_MS.getOrNull(attempt) else null
}
