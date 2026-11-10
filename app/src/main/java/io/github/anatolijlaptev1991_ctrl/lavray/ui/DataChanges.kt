// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * One signal for "the servers or subscriptions changed in the core": whoever changes them (an import) calls [notifyChanged],
 * whoever lists them (the Connection screen) keys its reload on [version]. Models do not call each other.
 */
class DataChanges {
    private val counter = MutableStateFlow(0)
    val version: StateFlow<Int> = counter.asStateFlow()

    fun notifyChanged() = counter.update { it + 1 }
}
