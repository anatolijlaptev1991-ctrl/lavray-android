// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup

import androidx.annotation.StringRes
import java.util.concurrent.atomic.AtomicReference

/**
 * The outcome of a restore that ended after its Activity was gone (Back from the root screen while the core was still
 * replacing the data). The restore runs to its end regardless; its result is one sentence, kept here for the process and
 * told once by the next screen. Nothing but a string resource id is held, and nothing is written to disk: if the process
 * dies too, the servers list itself shows what the data is now.
 */
class RestoreNews {
    private val pending = AtomicReference<Int?>(null)

    fun leave(@StringRes text: Int) = pending.set(text)

    /** The sentence to tell, once. */
    fun take(): Int? = pending.getAndSet(null)

    companion object {
        /** The one instance of the app process. */
        val process = RestoreNews()
    }
}
