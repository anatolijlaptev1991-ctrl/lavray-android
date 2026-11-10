// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure

/** What a single-subscription update stream said, collected while it ran and turned into a [FetchResult] at its end. */
internal class UpdateOutcome(private val nowMs: () -> Long = System::currentTimeMillis) {
    private var done = false
    private var wouldRemove: Int? = null
    private var failure: SubFailure? = null
    private var coreProblem: ErrorKind? = null
    private var alreadyRunning = false
    private var added = 0
    private var removed = 0
    private var total = 0
    private var detail: String? = null

    fun note(ev: SubscriptionProgress) {
        when (ev.phase) {
            SubscriptionProgress.Phase.PHASE_DIFFING -> {
                added = ev.addedCount ?: 0
                removed = ev.removedCount ?: 0
                total = ev.totalCount ?: 0
            }
            SubscriptionProgress.Phase.PHASE_CONFIRM_REQUIRED -> wouldRemove = ev.wouldRemoveCount ?: 0
            SubscriptionProgress.Phase.PHASE_FAILED -> failure = SubscriptionLogic.failureOf(ev.detail.orEmpty(), nowMs())
            SubscriptionProgress.Phase.PHASE_DONE -> {
                done = true
                detail = ev.detail
            }
            else -> Unit
        }
    }

    /**
     * The stream ended with an error. A core that closes it with an error after the confirm frame is asking, not failing. The
     * core refuses a second update of a group it already updates (its own scheduler holds it) with ALREADY_EXISTS: that is
     * "being updated right now", neither a failure of the provider nor of the core.
     */
    fun failed(e: StreamFailure) {
        if (wouldRemove != null) return
        if ((e.cause as? ApiException)?.code == ConnectCode.ALREADY_EXISTS) {
            alreadyRunning = true
            return
        }
        if (e.kind == ErrorKind.CoreUnavailable) coreProblem = e.kind
        failure = SubscriptionLogic.failureOf(e.cause?.message.orEmpty(), nowMs())
    }

    fun toResult(): FetchResult {
        val ask = wouldRemove
        return when {
            done -> FetchResult.Done(added, removed, total, SubscriptionLogic.skippedLinesOf(detail) ?: 0)
            ask != null -> FetchResult.NeedsConfirm(ask)
            alreadyRunning -> FetchResult.AlreadyRunning
            else -> FetchResult.Failed(failure ?: SubFailure.fromCode(SubFailure.GENERIC_CODE, nowMs()), coreProblem)
        }
    }
}
