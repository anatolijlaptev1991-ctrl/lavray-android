// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Saves the per-app choice for the whole app. Calls are serial and the newest choice wins, so a burst of taps is
 * written in order and the last tap is never lost; the caller runs it on the app's scope, not the screen's, so
 * leaving the picker straight after a tap does not cancel the write. [write] returns whether the :vpn process took
 * the choice; [onSaved] runs after each successful write (the running tunnel does not have it yet).
 */
class AppSelectionSaver(
    private val write: suspend (AppSelection) -> Boolean,
    private val onSaved: () -> Unit,
) {
    private val lock = Mutex()
    private var latest: AppSelection? = null

    /** True when the choice (or a newer one that replaced it) is saved; false when the write failed. */
    suspend fun save(selection: AppSelection): Boolean {
        synchronized(this) { latest = selection }
        return lock.withLock {
            val next = synchronized(this) { latest.also { latest = null } } ?: return@withLock true
            write(next).also { if (it) onSaved() }
        }
    }
}
