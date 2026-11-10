// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/** Who asked for the connection. Only system-initiated starts keep retrying. */
enum class StartSource(val wire: String) {
    USER("user"),
    BOOT("boot"),

    /** Service start without our extra: the system's Always-on VPN. */
    SYSTEM("system");

    val retries: Boolean get() = this != USER

    companion object {
        fun fromWire(value: String?): StartSource = entries.firstOrNull { it.wire == value } ?: SYSTEM
    }
}

enum class AutoPhase { IDLE, WAITING_NETWORK, ATTEMPTING, BACKING_OFF, CONNECTED, FAILED, STOPPED }

sealed interface AutoEvent {
    data class Start(val source: StartSource, val consentGranted: Boolean, val networkAvailable: Boolean) : AutoEvent
    data object NetworkAvailable : AutoEvent
    data object NetworkLost : AutoEvent
    data object AttemptSucceeded : AutoEvent
    data class AttemptFailed(val permanent: Boolean, val message: String?, val kind: String? = null) : AutoEvent
    data object TimerFired : AutoEvent

    /** The connection dropped after it had been established. */
    data object ConnectionLost : AutoEvent
    data object ManualDisconnect : AutoEvent
    data object ConsentRevoked : AutoEvent
}

sealed interface AutoEffect {
    data object StartAttempt : AutoEffect
    data class ScheduleRetry(val delayMs: Long) : AutoEffect
    data object CancelRetry : AutoEffect
    data class WatchNetwork(val enabled: Boolean) : AutoEffect
    data object NotifyNeedsPermission : AutoEffect
    data class Fail(val message: String?, val kind: String? = null) : AutoEffect
}

data class AutoState(
    val phase: AutoPhase = AutoPhase.IDLE,
    val retries: Boolean = false,
    val networkAvailable: Boolean = true,
) {
    /** True while the machine still intends to connect, so the process must stay alive. */
    val pending: Boolean
        get() = phase == AutoPhase.WAITING_NETWORK || phase == AutoPhase.ATTEMPTING || phase == AutoPhase.BACKING_OFF
}

/**
 * Decision logic of auto-connect and reconnect, free of Android types: events in,
 * effects out. The service interprets the effects (starts the attempt, arms a
 * timer, registers a NetworkCallback). Not thread-safe; the service feeds it from
 * one coroutine.
 */
class AutoConnectMachine(private val backoff: RetryBackoff = RetryBackoff()) {
    var state: AutoState = AutoState()
        private set

    fun handle(event: AutoEvent): List<AutoEffect> = when (event) {
        is AutoEvent.Start -> onStart(event)
        AutoEvent.NetworkAvailable -> onNetworkAvailable()
        AutoEvent.NetworkLost -> onNetworkLost()
        AutoEvent.AttemptSucceeded -> onSucceeded()
        is AutoEvent.AttemptFailed -> onFailed(event)
        AutoEvent.TimerFired -> onTimer()
        AutoEvent.ConnectionLost -> onConnectionLost()
        AutoEvent.ManualDisconnect -> stop(AutoPhase.STOPPED, emptyList())
        AutoEvent.ConsentRevoked -> stop(AutoPhase.STOPPED, listOf(AutoEffect.NotifyNeedsPermission))
    }

    private fun onStart(e: AutoEvent.Start): List<AutoEffect> {
        backoff.reset()
        if (!e.consentGranted) {
            // A retry timer or network watcher from an earlier start must not outlive this one.
            state = AutoState(AutoPhase.IDLE, e.source.retries, e.networkAvailable)
            return listOf(AutoEffect.CancelRetry, AutoEffect.WatchNetwork(false), AutoEffect.NotifyNeedsPermission)
        }
        if (e.source.retries && !e.networkAvailable) {
            state = AutoState(AutoPhase.WAITING_NETWORK, true, false)
            return listOf(AutoEffect.WatchNetwork(true))
        }
        state = AutoState(AutoPhase.ATTEMPTING, e.source.retries, e.networkAvailable)
        return listOf(AutoEffect.StartAttempt)
    }

    private fun onNetworkAvailable(): List<AutoEffect> {
        state = state.copy(networkAvailable = true)
        if (state.phase != AutoPhase.WAITING_NETWORK && state.phase != AutoPhase.BACKING_OFF) return emptyList()
        backoff.reset()
        state = state.copy(phase = AutoPhase.ATTEMPTING)
        return listOf(AutoEffect.CancelRetry, AutoEffect.StartAttempt)
    }

    private fun onNetworkLost(): List<AutoEffect> {
        state = state.copy(networkAvailable = false)
        if (state.phase != AutoPhase.BACKING_OFF) return emptyList()
        state = state.copy(phase = AutoPhase.WAITING_NETWORK)
        return listOf(AutoEffect.CancelRetry)
    }

    private fun onSucceeded(): List<AutoEffect> {
        if (state.phase != AutoPhase.ATTEMPTING) return emptyList()
        backoff.reset()
        state = state.copy(phase = AutoPhase.CONNECTED)
        return listOf(AutoEffect.CancelRetry, AutoEffect.WatchNetwork(false))
    }

    private fun onFailed(e: AutoEvent.AttemptFailed): List<AutoEffect> {
        if (state.phase != AutoPhase.ATTEMPTING) return emptyList()
        if (e.permanent || !state.retries) {
            state = state.copy(phase = AutoPhase.FAILED)
            return listOf(AutoEffect.CancelRetry, AutoEffect.WatchNetwork(false), AutoEffect.Fail(e.message, e.kind))
        }
        if (!state.networkAvailable) {
            state = state.copy(phase = AutoPhase.WAITING_NETWORK)
            return listOf(AutoEffect.WatchNetwork(true))
        }
        state = state.copy(phase = AutoPhase.BACKING_OFF)
        return listOf(AutoEffect.WatchNetwork(true), AutoEffect.ScheduleRetry(backoff.nextDelayMs()))
    }

    private fun onTimer(): List<AutoEffect> {
        if (state.phase != AutoPhase.BACKING_OFF) return emptyList()
        if (!state.networkAvailable) {
            state = state.copy(phase = AutoPhase.WAITING_NETWORK)
            return emptyList()
        }
        state = state.copy(phase = AutoPhase.ATTEMPTING)
        return listOf(AutoEffect.StartAttempt)
    }

    private fun onConnectionLost(): List<AutoEffect> {
        if (state.phase != AutoPhase.CONNECTED) return emptyList()
        if (!state.retries) {
            state = state.copy(phase = AutoPhase.IDLE)
            return emptyList()
        }
        backoff.reset()
        if (!state.networkAvailable) {
            state = state.copy(phase = AutoPhase.WAITING_NETWORK)
            return listOf(AutoEffect.WatchNetwork(true))
        }
        state = state.copy(phase = AutoPhase.ATTEMPTING)
        return listOf(AutoEffect.WatchNetwork(true), AutoEffect.StartAttempt)
    }

    private fun stop(phase: AutoPhase, extra: List<AutoEffect>): List<AutoEffect> {
        state = state.copy(phase = phase)
        return listOf(AutoEffect.CancelRetry, AutoEffect.WatchNetwork(false)) + extra
    }
}
