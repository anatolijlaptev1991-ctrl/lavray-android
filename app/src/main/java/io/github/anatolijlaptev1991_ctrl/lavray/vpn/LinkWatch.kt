// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState

sealed interface LinkEffect {
    data class StartGrace(val token: Int, val delayMs: Long) : LinkEffect
    data object CancelGrace : LinkEffect

    /** The grace period ended with the core still down: the tunnel is lost for good. */
    data object Lost : LinkEffect
}

/**
 * Decides when a core going IDLE or ERROR means the tunnel is really gone. The core passes through
 * IDLE on its own restarts (CONNECTED, DISCONNECTING, IDLE, CONNECTING, CONNECTED), so a drop of an
 * established tunnel is first only a grace period; a later CONNECTING or CONNECTED cancels it.
 * Free of Android types; the service feeds it from the event loop and runs the timer.
 */
class LinkWatch(private val graceMs: Long = DEFAULT_GRACE_MS) {
    /**
     * The core has been CONNECTED in this session and nobody has stopped it on purpose. It stays true after
     * [lost], so the first rebuild after a loss may still be held as a transition; only [reset] ends it.
     */
    var established: Boolean = false
        private set

    /** The grace period ended with the core down. A later CONNECTED makes the session live again. */
    var lost: Boolean = false
        private set

    /** The core is down, the grace timer is running. */
    var inGrace: Boolean = false
        private set

    private var token = 0

    /** [attemptRunning]: an attempt of ours owns the outcome, so a core drop is not news. */
    fun onCore(state: CoreState, attemptRunning: Boolean = false): List<LinkEffect> = when (state) {
        CoreState.CORE_STATE_CONNECTED -> {
            established = true
            lost = false
            cancel()
        }
        CoreState.CORE_STATE_CONNECTING, CoreState.CORE_STATE_RECONNECTING -> cancel()
        CoreState.CORE_STATE_DISCONNECTING -> emptyList()
        CoreState.CORE_STATE_IDLE, CoreState.CORE_STATE_ERROR -> onDown(attemptRunning)
    }

    fun onGraceElapsed(elapsed: Int): List<LinkEffect> {
        if (!inGrace || elapsed != token) return emptyList()
        inGrace = false
        lost = true
        return listOf(LinkEffect.Lost)
    }

    /** A deliberate stop or a decision that the session is over. */
    fun reset(): List<LinkEffect> {
        established = false
        lost = false
        return cancel()
    }

    private fun onDown(attemptRunning: Boolean): List<LinkEffect> {
        if (!established || lost || inGrace || attemptRunning) return emptyList()
        inGrace = true
        token++
        return listOf(LinkEffect.StartGrace(token, graceMs))
    }

    private fun cancel(): List<LinkEffect> {
        if (!inGrace) return emptyList()
        inGrace = false
        return listOf(LinkEffect.CancelGrace)
    }

    companion object {
        const val DEFAULT_GRACE_MS = 12_000L
    }
}
