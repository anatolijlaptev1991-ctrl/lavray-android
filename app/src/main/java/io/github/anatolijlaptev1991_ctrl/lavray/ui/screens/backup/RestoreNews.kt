// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup

import androidx.annotation.StringRes
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.getAndUpdate

/**
 * The outcome of a restore that ended after its Activity was gone (Back from the root screen while the core was still
 * replacing the data). The restore runs to its end regardless; its result is one sentence, kept here for the process and
 * told once by the next screen. Nothing but a string resource id is held, and nothing is written to disk: if the process
 * dies too, the servers list itself shows what the data is now.
 *
 * "The next screen" may already exist when the outcome arrives: the person pressed Back during the restore and opened the
 * app again a second later. So a screen does not ask once when it is created, it watches [waiting]: the outcome is told
 * the moment it is left here, by whichever screen is alive.
 */
class RestoreNews {
    private val pending = MutableStateFlow<Int?>(null)

    /** Non-null while an outcome waits to be told. Whoever sees it calls [take]; only one caller gets it. */
    val waiting: StateFlow<Int?> = pending.asStateFlow()

    fun leave(@StringRes text: Int) {
        pending.value = text
    }

    /** The sentence to tell, once. */
    fun take(): Int? = pending.getAndUpdate { null }

    companion object {
        /** The one instance of the app process. */
        val process = RestoreNews()
    }
}
