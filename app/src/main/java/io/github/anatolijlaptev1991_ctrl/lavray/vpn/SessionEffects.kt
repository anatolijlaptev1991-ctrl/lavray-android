// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

/** What the engine asks the service to say to the person when nobody is looking at the app. */
sealed interface AlertKind {
    data object PermissionNeeded : AlertKind
    data class Failure(val kind: String, val message: String?) : AlertKind
    data object Stopped : AlertKind
    data object ProtectionOff : AlertKind
}

/** The result of an attempt, posted by the coroutine that ran it. */
sealed interface AttemptOutcome {
    data class Success(val result: ConnectResult) : AttemptOutcome
    data class Failure(val failure: AttemptFailure) : AttemptOutcome
}

/** What the engine needs to know about the world; every read is cheap and non-blocking. */
internal interface SessionEnv {
    fun consentGranted(): Boolean
    fun networkUp(): Boolean
    fun uiBound(): Boolean

    /** The Kotlin side still holds an interface descriptor. */
    fun tunHeld(): Boolean

    /** The UI has asked to keep the process alive for a long operation. */
    fun busy(): Boolean
    fun nowMono(): Long
    fun nowWall(): Long

    /** The sequence number of the newest command that came from the person (connect, reconnect, disconnect). */
    fun latestUserSeq(): Long
}

/** Instructions of the engine; the service is only the interpreter. Order matters: they are executed in the order given. */
internal sealed interface SessionEffect {
    data class StartAttempt(val generation: Int, val profile: Long?, val disconnectFirst: Boolean) : SessionEffect
    data object CancelAttempt : SessionEffect
    data class ScheduleRetry(val delayMs: Long) : SessionEffect
    data object CancelRetry : SessionEffect
    data class WatchNetwork(val enabled: Boolean) : SessionEffect
    data class StartGrace(val token: Int, val delayMs: Long) : SessionEffect
    data object CancelGrace : SessionEffect
    data class ScheduleHold(val delayMs: Long) : SessionEffect
    data object CancelHold : SessionEffect

    /** Close the held interface; [onlyGeneration] null closes whatever is held, otherwise only that generation. */
    data class ReleaseTun(val onlyGeneration: Int?) : SessionEffect

    /** Ask the core to stop (bounded); the service then calls [SessionEngine.onDisconnectDone]. */
    data object DisconnectCore : SessionEffect
    data class Alert(val alert: AlertKind) : SessionEffect
    data object CancelAlert : SessionEffect
    data class SetForeground(val wanted: Boolean) : SessionEffect
    data object StopSelf : SessionEffect
    data class Publish(val status: VpnStatus) : SessionEffect
}
