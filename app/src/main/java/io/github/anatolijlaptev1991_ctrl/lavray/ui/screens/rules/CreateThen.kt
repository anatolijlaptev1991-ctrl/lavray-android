// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext

/**
 * A multi-step change that starts by creating something (a copy of a preset, then its name, then its activation) must
 * not leave the half-made thing behind when a later step fails: the person is told the action failed, and the list
 * must say the same. [undo] removes [created] if [rest] throws (also when it is cancelled) and the failure is rethrown.
 */
suspend fun <T, R> afterCreate(created: T, undo: suspend (T) -> Unit, rest: suspend (T) -> R): R =
    try {
        rest(created)
    } catch (e: Throwable) {
        withContext(NonCancellable) { runCatching { undo(created) } }
        throw e
    }
