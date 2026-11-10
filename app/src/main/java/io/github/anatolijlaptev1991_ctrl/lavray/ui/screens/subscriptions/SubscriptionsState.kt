// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind

/** What a card needs of a subscription. The full [Group] stays in the model, for edits that must send it whole. */
@Immutable
data class SubRow(
    val id: Long,
    val name: String,
    val url: String,
    val count: Int,
    val lastUpdateMs: Long?,
    val nextUpdateMs: Long?,
    val expireMs: Long,
    val downloadBytes: Long,
    val uploadBytes: Long,
    val totalBytes: Long,
    val autoUpdate: Boolean,
    val intervalMin: Int,
    val userAgent: String,
    val failure: SubFailure? = null,
) {
    val usedBytes: Long get() = downloadBytes + uploadBytes
    val usedPercent: Int get() = SubscriptionLogic.usedPercent(usedBytes, totalBytes)

    companion object {
        fun from(g: Group, count: Int, failure: SubFailure? = null): SubRow {
            val info = g.subscriptionInfo
            return SubRow(
                id = g.id,
                name = g.name,
                url = g.subscriptionUrl.orEmpty(),
                count = count,
                lastUpdateMs = g.lastUpdateMs,
                nextUpdateMs = SubscriptionLogic.nextUpdateMs(g.skipAutoUpdate, g.lastUpdateMs, g.updateIntervalMin, info?.updateIntervalHours),
                expireMs = info?.expireMs ?: 0L,
                downloadBytes = info?.downloadBytes ?: 0L,
                uploadBytes = info?.uploadBytes ?: 0L,
                totalBytes = info?.totalBytes ?: 0L,
                autoUpdate = !g.skipAutoUpdate,
                intervalMin = g.updateIntervalMin ?: 0,
                userAgent = g.userAgent.orEmpty(),
                failure = failure,
            )
        }
    }
}

/** An update stopped at "this will remove N of M servers"; it goes on only after the person agrees. */
@Immutable
data class PendingConfirm(val id: Long, val name: String, val remove: Int, val total: Int)

/** Which update is running, if any. A single update and "update all" never run together. */
@Immutable
sealed interface Busy {
    data object Idle : Busy
    data class One(val id: Long) : Busy
    data object All : Busy
}

/** Progress shown while an update runs: the phase, and for "update all" the subscription it is on. */
@Immutable
data class UpdateProgress(
    val phase: UpdatePhase? = null,
    val groupId: Long = 0L,
    val added: Int? = null,
    val removed: Int? = null,
    val total: Int? = null,
)

@Immutable
data class SubscriptionsState(
    val rows: List<SubRow> = emptyList(),
    val loading: Boolean = true,
    val error: ErrorKind? = null,
    val failures: Map<Long, SubFailure> = emptyMap(),
    val busy: Busy = Busy.Idle,
    val progress: UpdateProgress = UpdateProgress(),
    val confirm: PendingConfirm? = null,
) {
    val isBusy: Boolean get() = busy != Busy.Idle
}

sealed interface SubscriptionsAction {
    data class Loaded(val rows: List<SubRow>) : SubscriptionsAction
    data class Failed(val kind: ErrorKind) : SubscriptionsAction
    data class FailuresRestored(val failures: Map<Long, SubFailure>) : SubscriptionsAction
    data class NoteFailure(val id: Long, val failure: SubFailure) : SubscriptionsAction
    data class DropFailure(val id: Long) : SubscriptionsAction
    data class UpdateStarted(val id: Long) : SubscriptionsAction
    data object UpdateAllStarted : SubscriptionsAction
    data class Progress(val groupId: Long, val phase: UpdatePhase?, val added: Int?, val removed: Int?, val total: Int?) : SubscriptionsAction
    data object UpdateFinished : SubscriptionsAction
    data class Confirm(val ask: PendingConfirm?) : SubscriptionsAction
    data class Removed(val id: Long) : SubscriptionsAction
}

/** The screen's state machine as pure functions; the model runs it off the main thread. */
object SubscriptionsReducer {
    fun reduce(s: SubscriptionsState, a: SubscriptionsAction): SubscriptionsState = when (a) {
        is SubscriptionsAction.Loaded -> {
            val failures = liveFailures(a.rows, s.failures)
            s.copy(rows = withFailures(a.rows, failures), failures = failures, loading = false, error = null)
        }
        is SubscriptionsAction.Failed -> s.copy(loading = false, error = a.kind)
        is SubscriptionsAction.FailuresRestored -> {
            val failures = if (s.rows.isEmpty()) a.failures else liveFailures(s.rows, a.failures)
            s.copy(failures = failures, rows = withFailures(s.rows, failures))
        }
        is SubscriptionsAction.NoteFailure -> setFailures(s, s.failures + (a.id to a.failure))
        is SubscriptionsAction.DropFailure -> setFailures(s, s.failures - a.id)
        is SubscriptionsAction.UpdateStarted -> s.copy(busy = Busy.One(a.id), progress = UpdateProgress(groupId = a.id))
        SubscriptionsAction.UpdateAllStarted -> s.copy(busy = Busy.All, progress = UpdateProgress())
        is SubscriptionsAction.Progress -> s.copy(
            progress = UpdateProgress(a.phase ?: s.progress.phase, a.groupId, a.added ?: s.progress.added, a.removed ?: s.progress.removed, a.total ?: s.progress.total),
        )
        SubscriptionsAction.UpdateFinished -> s.copy(busy = Busy.Idle, progress = UpdateProgress())
        is SubscriptionsAction.Confirm -> s.copy(confirm = a.ask)
        is SubscriptionsAction.Removed -> setFailures(s.copy(rows = s.rows.filterNot { it.id == a.id }), s.failures - a.id)
    }

    /**
     * A failure of a subscription that no longer exists is forgotten, as on the desktop; so is one that a later successful
     * update (seen in the group's own update time) has made out of date.
     */
    internal fun liveFailures(rows: List<SubRow>, failures: Map<Long, SubFailure>): Map<Long, SubFailure> {
        val byId = rows.associateBy { it.id }
        return failures.filter { (id, f) -> byId[id]?.let { !SubscriptionLogic.superseded(f, it.lastUpdateMs) } == true }
    }

    private fun setFailures(s: SubscriptionsState, failures: Map<Long, SubFailure>): SubscriptionsState =
        s.copy(failures = failures, rows = withFailures(s.rows, failures))

    private fun withFailures(rows: List<SubRow>, failures: Map<Long, SubFailure>): List<SubRow> =
        rows.map { r ->
            val f = failures[r.id]
            if (r.failure == f) r else r.copy(failure = f)
        }

    /** What "this update would remove" asks: the total is never smaller than the number removed (desktop rule). */
    fun confirmOf(id: Long, name: String, remove: Int, knownCount: Int): PendingConfirm =
        PendingConfirm(id, name, remove, kotlin.math.max(knownCount, remove))
}
