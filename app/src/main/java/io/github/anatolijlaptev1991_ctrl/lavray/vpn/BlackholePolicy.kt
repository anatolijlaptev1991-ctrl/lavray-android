// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/** What the held interface must do right now. */
sealed interface HoldDecision {
    /** Not a blackhole situation: nothing is held, or the core runs on the interface. */
    data object None : HoldDecision

    /** Keep the interface with nobody reading it; apps have no network until [untilMs] (same clock as `now`). */
    data class Hold(val untilMs: Long) : HoldDecision

    /** Close the interface now. [capExpired] means the hard cap ended a hold: the user must be told. */
    data class Release(val capExpired: Boolean) : HoldDecision
}

/** What the policy looks at; plain values. */
data class HoldInput(
    /** The Kotlin side still holds an open interface descriptor. */
    val tunHeld: Boolean,
    /** The core is CONNECTING, CONNECTED, DISCONNECTING or RECONNECTING (it uses the interface). */
    val coreActive: Boolean,
    /** The core was CONNECTED in this session and nobody stopped it deliberately ([LinkWatch.established]). */
    val established: Boolean,
    val inGrace: Boolean,
    val phase: AutoPhase,
    /** A newer interface than the one the last CONNECTED run had has been established: the core reads one again. */
    val newInterface: Boolean = false,
)

/**
 * The exact rule of the transition blackhole (docs/ARCHITECTURE.md, "Мост без дыры"). Closing the
 * interface while an established tunnel is only being rebuilt would send every app straight to the
 * physical network. So the interface is kept, unread, for as long as all of these hold:
 *  - the interface is held and nobody reads it: the core is not running, or it is running but has not yet replaced the
 *    interface (it sits in CONNECTING before it calls OpenTun; the hold is not released by "core active" alone);
 *  - the tunnel had been established and was not stopped on purpose;
 *  - the app itself is causing the gap: a grace period after a core drop or restart, or an attempt
 *    (reconnect, server switch, retry right after a loss) is running;
 *  - less than [capMs] have passed since the hold began.
 * Waiting for a network, a retry back-off, a failed or stopped session never holds: apps get their
 * normal connection back at once. The next establish() replaces the interface and ends the hold.
 */
class BlackholePolicy(private val capMs: Long = DEFAULT_CAP_MS) {
    private var since: Long? = null

    fun evaluate(nowMs: Long, input: HoldInput): HoldDecision {
        if (!input.tunHeld) {
            since = null
            return HoldDecision.None
        }
        val begun = since != null
        // The core runs on a (new) interface: no blackhole. A core that is only "active" with no new interface is
        // still not reading the held one, so a hold that has begun keeps counting.
        if (input.coreActive && (!begun || input.newInterface)) {
            since = null
            return HoldDecision.None
        }
        val causedByUs = input.inGrace || input.phase == AutoPhase.ATTEMPTING || (begun && input.phase == AutoPhase.CONNECTED)
        if (!input.established || !causedByUs) {
            since = null
            return HoldDecision.Release(capExpired = false)
        }
        val begin = since ?: nowMs.also { since = it }
        return if (nowMs - begin >= capMs) {
            since = null
            HoldDecision.Release(capExpired = true)
        } else {
            HoldDecision.Hold(begin + capMs)
        }
    }

    fun reset() {
        since = null
    }

    companion object {
        const val DEFAULT_CAP_MS = 30_000L
    }
}
