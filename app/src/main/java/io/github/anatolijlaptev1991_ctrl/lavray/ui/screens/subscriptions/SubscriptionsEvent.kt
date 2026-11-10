// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind

/** One-shot results the screen words as a toast. */
sealed interface SubscriptionsEvent {
    data class Added(val name: String) : SubscriptionsEvent
    data class Updated(val name: String) : SubscriptionsEvent
    data class UpdateFailed(val failure: SubFailure) : SubscriptionsEvent
    data class UpdateAllDone(val tally: UpdateTally) : SubscriptionsEvent

    /** [count] is null when the core did not say and the list could not be counted: the toast then names no number. */
    data class RolledBack(val name: String, val count: Int?) : SubscriptionsEvent
    data object RollbackFailed : SubscriptionsEvent

    /** The core has no snapshot to go back to (it is kept in memory only, so a core restart loses it) or an update still runs. */
    data object RollbackUnavailable : SubscriptionsEvent
    data object Saved : SubscriptionsEvent
    data object Deleted : SubscriptionsEvent

    /** The core refused to delete the group: the connected server, a group that holds it, or the last group. */
    data object InUse : SubscriptionsEvent

    /** An update was asked for while another runs (here or in the scheduler of the core): this one did not start, and that is said. */
    data object Busy : SubscriptionsEvent
    data class Failed(val kind: ErrorKind) : SubscriptionsEvent
}

/** Settings shared by all subscriptions that the per-subscription sheet shows. */
data class SharedSubSettings(val hwid: Boolean, val tunnel: Boolean)
