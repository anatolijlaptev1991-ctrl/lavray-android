// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState

data class NotificationModel(val state: VpnState, val detail: String? = null, val busy: Boolean = false)

/**
 * Decides when the ongoing notification may be re-posted. While the screen is off
 * nothing is posted (no wake-up work for text nobody can read); the latest model is
 * held and released by [onScreenOn]. Identical consecutive models are never re-posted.
 */
class NotificationGate {
    private var posted: NotificationModel? = null
    private var held: NotificationModel? = null

    /** Returns the model to post now, or null to skip. */
    @Synchronized
    fun offer(model: NotificationModel, screenOn: Boolean): NotificationModel? {
        if (!screenOn) {
            held = model.takeIf { it != posted }
            return null
        }
        held = null
        if (model == posted) return null
        posted = model
        return model
    }

    @Synchronized
    fun onScreenOn(): NotificationModel? {
        val pending = held ?: return null
        held = null
        posted = pending
        return pending
    }

    /** The first post (required by startForeground) always goes out. */
    @Synchronized
    fun forcePost(model: NotificationModel): NotificationModel {
        posted = model
        held = null
        return model
    }
}
