// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectMode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreConnect
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreSubscribeEvents
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsGetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.coroutines.launch

class NoServerException : Exception("no server selected")

/** The core reported ERROR before the tunnel was ever up. Retryable: the next attempt may well work. */
class CoreConnectException(message: String?) : Exception(message ?: "core reported an error while connecting")

data class AttemptFailure(val permanent: Boolean, val message: String?, val kind: String = FailureKind.CONNECT_FAILED)

/** The few core calls an attempt needs, so the waiting logic runs on the JVM against a fake. */
internal interface CoreApi {
    suspend fun status(): CoreStatus
    suspend fun connect(profileId: Long): CoreStatus?
    suspend fun lastProfileId(): Long?
    fun events(): Flow<CoreStatus>
}

internal class ClientCoreApi(private val client: LavrayClient) : CoreApi {
    override suspend fun status(): CoreStatus = client.coreGetStatus()

    override suspend fun connect(profileId: Long): CoreStatus? =
        client.coreConnect(ConnectRequest(profileId = profileId, mode = ConnectMode.CONNECT_MODE_TUN)).status

    override suspend fun lastProfileId(): Long? {
        val reply = client.settingsGetSettings(GetSettingsRequest(keys = listOf(SETTING_REMEMBER_ID)))
        val value = reply.values[SETTING_REMEMBER_ID] ?: return null
        return (value.intValue ?: value.stringValue?.toLongOrNull())?.takeIf { it > 0 }
    }

    override fun events(): Flow<CoreStatus> = client.coreSubscribeEvents().mapNotNull { it.stateChanged }

    private companion object {
        const val SETTING_REMEMBER_ID = "remember.id"
    }
}

/** One connection attempt against a running core, as the desktop's ConnectLast does it. */
internal object ConnectAttempt {
    /**
     * Connects [profileId], or the last connected server when null, and returns only once the core is
     * CONNECTED. A core that is already connecting (another attempt's work) is waited for, not trusted;
     * a core that reports ERROR before it was ever up fails the attempt, so it is retried or reported
     * instead of being shown as connected. The caller bounds the time.
     */
    suspend fun run(api: CoreApi, profileId: Long?): CoreStatus = coroutineScope {
        // Subscribe before the first call: a state change in between must not be missed.
        val updates = Channel<CoreStatus>(Channel.UNLIMITED)
        val collector = launch(start = CoroutineStart.UNDISPATCHED) {
            try {
                api.events().collect { updates.trySend(it) }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // The stream ends when the core stops; the outcome then comes from the calls themselves.
            }
        }
        try {
            var current = api.status()
            if (!inProgress(current.state)) {
                val id = profileId ?: api.lastProfileId() ?: throw NoServerException()
                // Whatever was queued before this call describes the previous run.
                while (updates.tryReceive().isSuccess) { /* drained */ }
                current = api.connect(id) ?: current
            }
            awaitConnected(current, updates)
        } finally {
            collector.cancel()
            updates.close()
        }
    }

    private fun inProgress(state: CoreState) =
        state == CoreState.CORE_STATE_CONNECTED || state == CoreState.CORE_STATE_CONNECTING || state == CoreState.CORE_STATE_RECONNECTING

    private suspend fun awaitConnected(first: CoreStatus, updates: Channel<CoreStatus>): CoreStatus {
        var status = first
        while (true) {
            when (status.state) {
                CoreState.CORE_STATE_CONNECTED -> return status
                CoreState.CORE_STATE_ERROR -> throw CoreConnectException(status.lastError)
                else -> Unit
            }
            status = updates.receive()
        }
    }

    fun classify(error: Throwable, tunFailure: TunFailure? = null): AttemptFailure = when {
        // The tunnel could not be built for a reason of ours (for example no installed app in the allow-list).
        tunFailure != null -> AttemptFailure(permanent = true, message = tunFailure.message, kind = tunFailure.kind)
        error is NoServerException -> AttemptFailure(permanent = true, message = error.message, kind = FailureKind.NO_SERVER)
        error is CoreStartException -> AttemptFailure(permanent = true, message = error.message, kind = FailureKind.CORE_START)
        error is ApiException -> AttemptFailure(CoreStatusMapper.isPermanent(error.code), error.message)
        else -> AttemptFailure(permanent = false, message = error.message)
    }
}

/** A tunnel that could not be built, remembered until the attempt that asked for it ends. */
data class TunFailure(val kind: String, val message: String?)

/** Whether a start request may join the attempt that is already running. */
enum class AttemptDecision { START, KEEP, REPLACE }

object AttemptGate {
    /** The same profile in flight is kept (no cancel, no second connect); another profile replaces it. */
    fun decide(running: Boolean, runningProfile: Long?, requested: Long?): AttemptDecision = when {
        !running -> AttemptDecision.START
        runningProfile == requested -> AttemptDecision.KEEP
        else -> AttemptDecision.REPLACE
    }
}
