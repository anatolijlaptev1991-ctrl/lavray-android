// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * Whether the boot receiver is registered at all. With "connect at phone start" off (the default) the
 * receiver is switched off, so the system does not spawn the :vpn process at every boot for nothing.
 *
 * The manifest leaves the receiver ENABLED (the component default) on purpose: whatever goes wrong with a
 * switch, the fallback is "the receiver is on", never "auto-connect silently off". Turning it on means
 * "back to the manifest default", turning it off is an explicit DISABLED.
 */
enum class BootSwitch {
    /** Manifest default (enabled). */
    DEFAULT,
    DISABLED,
}

object BootComponentPolicy {
    fun desired(autoConnectEnabled: Boolean): BootSwitch = if (autoConnectEnabled) BootSwitch.DEFAULT else BootSwitch.DISABLED
}

/** The platform's component switch, behind an interface so the policy runs on the JVM. */
interface BootSwitchPort {
    /** The state in force now, or null when it cannot be read. */
    fun current(): BootSwitch?

    /** Throws when the platform refuses. */
    fun set(target: BootSwitch)
}

enum class SwitchOutcome {
    UNCHANGED,
    CHANGED,

    /** Turning the receiver off failed: it stays on, which is the safe side. */
    FAILED_STAYS_ENABLED,

    /** Turning the receiver on failed after it had been off: auto-connect will not fire until a later reconcile succeeds. */
    FAILED_STILL_DISABLED,
}

/** Brings the receiver in line with the preference. Safe to call on every start: a no-op when nothing differs. */
class BootReceiverSwitch(private val port: BootSwitchPort) {
    fun reconcile(autoConnectEnabled: Boolean): SwitchOutcome {
        val target = BootComponentPolicy.desired(autoConnectEnabled)
        val now = port.current()
        if (now == target) return SwitchOutcome.UNCHANGED
        return try {
            port.set(target)
            SwitchOutcome.CHANGED
        } catch (_: RuntimeException) {
            if (target == BootSwitch.DISABLED) SwitchOutcome.FAILED_STAYS_ENABLED else SwitchOutcome.FAILED_STILL_DISABLED
        }
    }
}
