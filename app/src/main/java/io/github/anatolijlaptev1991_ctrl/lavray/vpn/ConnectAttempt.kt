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
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsGetSettings

class NoServerException : Exception("no server selected")

data class AttemptFailure(val permanent: Boolean, val message: String?)

/** One connection attempt against a running core, as the desktop's ConnectLast does it. */
internal object ConnectAttempt {
    private const val SETTING_REMEMBER_ID = "remember.id"

    /**
     * Connects [profileId], or the last connected server when null. Returns the core
     * status to publish. An already connecting or connected core counts as success.
     */
    suspend fun run(client: LavrayClient, profileId: Long?): CoreStatus {
        val current = client.coreGetStatus()
        if (current.state == CoreState.CORE_STATE_CONNECTING || current.state == CoreState.CORE_STATE_CONNECTED) {
            return current
        }
        val id = profileId ?: lastProfileId(client) ?: throw NoServerException()
        val response = client.coreConnect(ConnectRequest(profileId = id, mode = ConnectMode.CONNECT_MODE_TUN))
        return response.status ?: current
    }

    private suspend fun lastProfileId(client: LavrayClient): Long? {
        val reply = client.settingsGetSettings(GetSettingsRequest(keys = listOf(SETTING_REMEMBER_ID)))
        val value = reply.values[SETTING_REMEMBER_ID] ?: return null
        return (value.intValue ?: value.stringValue?.toLongOrNull())?.takeIf { it > 0 }
    }

    fun classify(error: Throwable): AttemptFailure = when (error) {
        is NoServerException -> AttemptFailure(permanent = true, message = error.message)
        is CoreStartException -> AttemptFailure(permanent = true, message = error.message)
        is ApiException -> AttemptFailure(CoreStatusMapper.isPermanent(error.code), error.message)
        else -> AttemptFailure(permanent = false, message = error.message)
    }
}
