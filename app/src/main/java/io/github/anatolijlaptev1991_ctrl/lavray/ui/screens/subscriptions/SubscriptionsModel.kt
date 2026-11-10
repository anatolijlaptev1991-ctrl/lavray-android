// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Page
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RollbackSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateAllSubscriptionsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateSubscriptionSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileUpdateGroup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsGetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsSetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionDeleteSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionListSubscriptions
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionRollbackSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionUpdateAllSubscriptions
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionUpdateSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionUpdateSubscriptionSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/** One-shot results the screen words as a toast. */
sealed interface SubscriptionsEvent {
    data class Added(val name: String) : SubscriptionsEvent
    data class Updated(val name: String) : SubscriptionsEvent
    data class UpdateFailed(val failure: SubFailure) : SubscriptionsEvent
    data class UpdateAllDone(val tally: UpdateTally) : SubscriptionsEvent
    data class RolledBack(val name: String, val count: Int) : SubscriptionsEvent
    data object RollbackFailed : SubscriptionsEvent
    data object Saved : SubscriptionsEvent
    data object Deleted : SubscriptionsEvent
    data class Failed(val kind: ErrorKind) : SubscriptionsEvent
}

/** Settings shared by all subscriptions that the per-subscription sheet shows. */
data class SharedSubSettings(val hwid: Boolean, val tunnel: Boolean)

/** What a single-subscription update stream said, collected while it ran and turned into a [FetchResult] at its end. */
private class UpdateOutcome {
    var done = false
    var wouldRemove: Int? = null
    var failure: SubFailure? = null
    var coreProblem: ErrorKind? = null
    var added = 0
    var removed = 0
    var total = 0
    var detail: String? = null

    fun toResult(): FetchResult {
        val ask = wouldRemove
        val f = failure ?: SubFailure.fromCode(SubFailure.GENERIC_CODE, System.currentTimeMillis())
        return when {
            done -> FetchResult.Done(added, removed, total, SubscriptionLogic.skippedLinesOf(detail) ?: 0)
            ask != null -> FetchResult.NeedsConfirm(ask)
            else -> FetchResult.Failed(f, coreProblem)
        }
    }
}

/**
 * Subscriptions: loads the groups, runs updates as streams that live only while the screen collects them
 * (the screen cancels them when it stops), applies list changes through the pure [SubscriptionsReducer]
 * on a background dispatcher and publishes immutable state. Nothing here touches the main thread.
 */
class SubscriptionsModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val store: SubFailureStorage,
) {
    private val registry = SubscriptionRegistry(session)
    private val actions = Channel<SubscriptionsAction>(Channel.UNLIMITED)
    private val mutable = MutableStateFlow(SubscriptionsState())
    private val mutableEvents = MutableSharedFlow<SubscriptionsEvent>(extraBufferCapacity = 8)
    private var rawGroups: Map<Long, Group> = emptyMap()
    private var runJob: Job? = null

    val state: StateFlow<SubscriptionsState> = mutable.asStateFlow()
    val events: SharedFlow<SubscriptionsEvent> = mutableEvents.asSharedFlow()

    init {
        scope.launch(Dispatchers.Default) {
            for (action in actions) {
                val before = mutable.value.failures
                mutable.value = SubscriptionsReducer.reduce(mutable.value, action)
                val after = mutable.value.failures
                if (after != before) launch(Dispatchers.IO) { store.save(after) }
            }
        }
        scope.launch(Dispatchers.IO) { dispatch(SubscriptionsAction.FailuresRestored(store.load())) }
    }

    fun dispatch(action: SubscriptionsAction) {
        actions.trySend(action)
    }

    // ---- list

    fun refresh() {
        scope.launch(Dispatchers.IO) { load() }
    }

    private suspend fun load() {
        when (val r = session.call { subscriptionListSubscriptions() }) {
            is CoreResult.Fail -> dispatch(SubscriptionsAction.Failed(r.kind))
            is CoreResult.Ok -> {
                val groups = r.value.groups
                rawGroups = groups.associateBy { it.id }
                val counts = countsOf(groups)
                dispatch(SubscriptionsAction.Loaded(groups.map { SubRow.from(it, counts[it.id] ?: it.profileOrder.size) }))
            }
        }
    }

    /** Servers per subscription: the core's total for the group, the group's own order when that call fails. */
    private suspend fun countsOf(groups: List<Group>): Map<Long, Int> = coroutineScope {
        groups.map { g ->
            async {
                val r = session.call { profileListProfiles(ListProfilesRequest(groupId = g.id, page = Page(offset = 0, limit = 1))) }
                g.id to ((r as? CoreResult.Ok)?.value?.total?.toInt() ?: g.profileOrder.size)
            }
        }.awaitAll().toMap()
    }

    // ---- updates

    fun update(id: Long, confirm: Boolean = false) {
        if (mutable.value.isBusy) return
        val name = rawGroups[id]?.name.orEmpty()
        runJob = scope.launch(Dispatchers.IO) { settle(id, name, confirm, runUpdate(id, name, confirm)) }
    }

    fun updateAll() {
        if (mutable.value.isBusy) return
        runJob = scope.launch(Dispatchers.IO) { runUpdateAll() }
    }

    /** Called when the screen stops: a running stream is cancelled, which aborts its socket. */
    fun cancelRunning() {
        runJob?.cancel()
        runJob = null
    }

    /** Runs one update to its end, publishing its progress to [state], and says how it ended. What to do with the result is the caller's. */
    private suspend fun runUpdate(id: Long, name: String, confirm: Boolean): FetchResult {
        dispatch(SubscriptionsAction.UpdateStarted(id))
        val out = UpdateOutcome()
        try {
            val request = UpdateSubscriptionRequest(groupId = id, confirmDestructive = if (confirm) true else null)
            session.stream { subscriptionUpdateSubscription(request) }.collect { ev ->
                publishProgress(ev)
                note(out, ev)
            }
        } catch (e: CancellationException) {
            dispatch(SubscriptionsAction.UpdateFinished)
            throw e
        } catch (e: StreamFailure) {
            // A core that closes the stream with an error after the confirm frame is asking, not failing.
            if (out.wouldRemove == null) {
                if (e.kind == ErrorKind.CoreUnavailable) out.coreProblem = e.kind
                out.failure = SubscriptionLogic.failureOf(e.cause?.message.orEmpty(), System.currentTimeMillis())
            }
        }
        dispatch(SubscriptionsAction.UpdateFinished)
        return out.toResult()
    }

    private fun note(out: UpdateOutcome, ev: SubscriptionProgress) {
        when (ev.phase) {
            SubscriptionProgress.Phase.PHASE_DIFFING -> {
                out.added = ev.addedCount ?: 0
                out.removed = ev.removedCount ?: 0
                out.total = ev.totalCount ?: 0
            }
            SubscriptionProgress.Phase.PHASE_CONFIRM_REQUIRED -> out.wouldRemove = ev.wouldRemoveCount ?: 0
            SubscriptionProgress.Phase.PHASE_FAILED -> out.failure = SubscriptionLogic.failureOf(ev.detail.orEmpty(), System.currentTimeMillis())
            SubscriptionProgress.Phase.PHASE_DONE -> {
                out.done = true
                out.detail = ev.detail
            }
            else -> Unit
        }
    }

    private suspend fun settle(id: Long, name: String, confirmed: Boolean, result: FetchResult) {
        when {
            result is FetchResult.NeedsConfirm && !confirmed -> askToConfirm(id, name, result.remove)
            result is FetchResult.Done -> {
                dispatch(SubscriptionsAction.DropFailure(id))
                mutableEvents.tryEmit(SubscriptionsEvent.Updated(name))
                load()
            }
            result is FetchResult.Failed && result.coreProblem != null -> mutableEvents.tryEmit(SubscriptionsEvent.Failed(result.coreProblem))
            else -> {
                val f = (result as? FetchResult.Failed)?.failure ?: SubFailure.fromCode(SubFailure.GENERIC_CODE, System.currentTimeMillis())
                dispatch(SubscriptionsAction.NoteFailure(id, f))
                mutableEvents.tryEmit(SubscriptionsEvent.UpdateFailed(f))
            }
        }
    }

    /** Puts the "this update removes N of M servers" question where the Subscriptions screen shows it. */
    private fun askToConfirm(id: Long, name: String, remove: Int) {
        val known = mutable.value.rows.firstOrNull { it.id == id }?.count ?: 0
        dispatch(SubscriptionsAction.Confirm(SubscriptionsReducer.confirmOf(id, name, remove, known)))
    }

    private suspend fun runUpdateAll() {
        dispatch(SubscriptionsAction.UpdateAllStarted)
        var tally = UpdateTally()
        try {
            session.stream { subscriptionUpdateAllSubscriptions(UpdateAllSubscriptionsRequest) }.collect { ev ->
                publishProgress(ev)
                tally = SubscriptionLogic.tally(tally, ev.phase)
                when (ev.phase) {
                    SubscriptionProgress.Phase.PHASE_FAILED ->
                        dispatch(SubscriptionsAction.NoteFailure(ev.groupId, SubscriptionLogic.failureOf(ev.detail.orEmpty(), System.currentTimeMillis())))
                    SubscriptionProgress.Phase.PHASE_DONE -> dispatch(SubscriptionsAction.DropFailure(ev.groupId))
                    else -> Unit
                }
            }
            mutableEvents.tryEmit(SubscriptionsEvent.UpdateAllDone(tally))
        } catch (e: CancellationException) {
            dispatch(SubscriptionsAction.UpdateFinished)
            throw e
        } catch (e: StreamFailure) {
            mutableEvents.tryEmit(SubscriptionsEvent.Failed(e.kind))
        }
        dispatch(SubscriptionsAction.UpdateFinished)
        load()
    }

    private fun publishProgress(ev: SubscriptionProgress) {
        dispatch(SubscriptionsAction.Progress(ev.groupId, SubscriptionLogic.phaseOf(ev.phase), ev.addedCount, ev.removedCount, ev.totalCount))
    }

    fun dismissConfirm() = dispatch(SubscriptionsAction.Confirm(null))

    fun acceptConfirm(ask: PendingConfirm) {
        dispatch(SubscriptionsAction.Confirm(null))
        update(ask.id, confirm = true)
    }

    // ---- row actions

    fun toggleAuto(row: SubRow) {
        val g = rawGroups[row.id] ?: return
        scope.launch(Dispatchers.IO) {
            finish(session.call { profileUpdateGroup(UpdateGroupRequest(SubscriptionEdit.withAutoUpdate(g, !row.autoUpdate))) }) { load() }
        }
    }

    fun delete(row: SubRow) {
        scope.launch(Dispatchers.IO) {
            finish(session.call { subscriptionDeleteSubscription(DeleteSubscriptionRequest(row.id)) }) {
                dispatch(SubscriptionsAction.Removed(row.id))
                mutableEvents.tryEmit(SubscriptionsEvent.Deleted)
                load()
            }
        }
    }

    fun rollback(row: SubRow) {
        scope.launch(Dispatchers.IO) {
            when (val r = session.call { subscriptionRollbackSubscription(RollbackSubscriptionRequest(row.id)) }) {
                is CoreResult.Fail -> mutableEvents.tryEmit(SubscriptionsEvent.Failed(r.kind))
                is CoreResult.Ok -> if (r.value.success) {
                    mutableEvents.tryEmit(SubscriptionsEvent.RolledBack(row.name, r.value.restoredCount ?: 0))
                    load()
                } else {
                    mutableEvents.tryEmit(SubscriptionsEvent.RollbackFailed)
                }
            }
        }
    }

    // ---- sheets: these return the outcome so the sheet can show the error under its field

    /**
     * Creates the subscription and starts its first update: the core makes only an empty group. A link that already has a
     * subscription updates that one instead (a typed name is then not applied: the subscription keeps its own).
     */
    suspend fun create(name: String, url: String): CoreResult<Unit> {
        return when (val r = registry.register(url.trim(), name.trim())) {
            is CoreResult.Fail -> r
            is CoreResult.Ok -> {
                val group = r.value.group
                if (!r.value.existed) mutableEvents.tryEmit(SubscriptionsEvent.Added(group.name))
                rawGroups = rawGroups + (group.id to group)
                load()
                update(group.id)
                CoreResult.Ok(Unit)
            }
        }
    }

    /**
     * "Add this subscription link and load it" in one action, for the import sheet: finds or makes the subscription, runs its
     * update here (progress goes to [state] as for any update) and says what came of it. A subscription whose first fetch
     * failed stays: the link is the part that cannot be retyped by hand, and the cause is often a setting (the device-limit
     * switch) that the person changes before updating it again.
     */
    suspend fun addAndFetch(link: String): SubscribeResult {
        if (mutable.value.isBusy) return SubscribeResult.Busy
        val registered = when (val r = registry.register(link.trim())) {
            is CoreResult.Fail -> return SubscribeResult.Refused(r.kind, r.line)
            is CoreResult.Ok -> r.value
        }
        val group = registered.group
        rawGroups = rawGroups + (group.id to group)
        return finishAdd(group, registered.existed, runUpdate(group.id, group.name, confirm = false))
    }

    private suspend fun finishAdd(group: Group, existed: Boolean, result: FetchResult): SubscribeResult {
        val id = group.id
        val out = when (result) {
            is FetchResult.Done -> {
                dispatch(SubscriptionsAction.DropFailure(id))
                SubscribeResult.Fetched(group.name, existed, result.added, result.removed, result.total, result.unparsedLines)
            }
            is FetchResult.NeedsConfirm -> {
                askToConfirm(id, group.name, result.remove)
                SubscribeResult.NeedsConfirm(group.name, existed, result.remove)
            }
            is FetchResult.Failed -> {
                if (result.coreProblem == null) dispatch(SubscriptionsAction.NoteFailure(id, result.failure))
                SubscribeResult.NotFetched(group.name, existed, result.failure, deviceLimitMayApply(result.failure))
            }
        }
        load()
        return out
    }

    /** The provider may limit devices when the answer is a refusal, an empty list or a stub, and the switch that answers it is off. */
    private suspend fun deviceLimitMayApply(failure: SubFailure): Boolean =
        failure.kind in DEVICE_LIMIT_SUSPECTS && (loadShared() as? CoreResult.Ok)?.value?.hwid != true

    suspend fun loadShared(): CoreResult<SharedSubSettings> {
        val keys = listOf(SubSettingKeys.USE_PROXY, SubSettingKeys.HWID)
        return when (val r = session.call { settingsGetSettings(GetSettingsRequest(keys)) }) {
            is CoreResult.Fail -> r
            is CoreResult.Ok -> CoreResult.Ok(
                SharedSubSettings(
                    hwid = r.value.values[SubSettingKeys.HWID]?.boolValue ?: false,
                    tunnel = r.value.values[SubSettingKeys.USE_PROXY]?.boolValue ?: false,
                ),
            )
        }
    }

    suspend fun save(row: SubRow, plan: EditPlan): CoreResult<Unit> {
        if (plan.isEmpty) return CoreResult.Ok(Unit)
        val g = rawGroups[row.id] ?: return CoreResult.Fail(ErrorKind.NotFound)
        val r = session.call { applyPlan(g, plan) }
        return when (r) {
            is CoreResult.Fail -> r
            is CoreResult.Ok -> {
                mutableEvents.tryEmit(SubscriptionsEvent.Saved)
                load()
                CoreResult.Ok(Unit)
            }
        }
    }

    private suspend fun LavrayClient.applyPlan(g: Group, plan: EditPlan) {
        var group = g
        plan.newUrl?.let { group = subscriptionUpdateSubscriptionSettings(UpdateSubscriptionSettingsRequest(g.id, it)) }
        if (plan.touchesGroup) profileUpdateGroup(UpdateGroupRequest(SubscriptionEdit.apply(group, plan)))
        if (plan.settings.isNotEmpty()) {
            settingsSetSettings(SetSettingsRequest(plan.settings.mapValues { SettingValue(boolValue = it.value) }))
        }
    }

    private inline fun <T> finish(result: CoreResult<T>, onOk: () -> Unit) {
        when (result) {
            is CoreResult.Fail -> mutableEvents.tryEmit(SubscriptionsEvent.Failed(result.kind))
            is CoreResult.Ok -> onOk()
        }
    }
}
