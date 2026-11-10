// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext

/**
 * A multi-step change that starts by creating something (a copy of a preset, then its name, then its activation) must
 * not leave the half-made thing behind when a later step fails: the person is told the action failed, and the list
 * must say the same. [undo] removes [created] if [rest] throws (also when it is cancelled) and the failure is rethrown.
 *
 * When the undo itself fails the copy stays. That is not hidden: the undo's failure is attached to the one rethrown
 * ([Throwable.suppressed]) and the caller reloads the list after any failure, so the leftover copy is on screen next to
 * the "failed" message instead of being a surprise later.
 */
suspend fun <T, R> afterCreate(created: T, undo: suspend (T) -> Unit, rest: suspend (T) -> R): R =
    try {
        rest(created)
    } catch (e: Throwable) {
        withContext(NonCancellable) {
            try {
                undo(created)
            } catch (undoFailure: Throwable) {
                e.addSuppressed(undoFailure)
            }
        }
        throw e
    }
