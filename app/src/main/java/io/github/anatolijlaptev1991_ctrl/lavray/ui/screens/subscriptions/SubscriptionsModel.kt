// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CreateSubscriptionRequest
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
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionCreateSubscription
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

/** How one single-subscription update ended, collected while its stream ran. */
private class UpdateOutcome {
    var done = false
    var wouldRemove: Int? = null
    var failure: SubFailure? = null
    var coreProblem: ErrorKind? = null
}

/**
 * Subscriptions: loads the groups, runs updates as streams that live only while the screen collects them
 * (the screen cancels them when it stops), applies list changes through the pure [SubscriptionsReducer]
 * on a background dispatcher and publishes immutable state. Nothing here touches the main thread.
 */
class SubscriptionsModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val store: SubFailureStore,
) {
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
        runJob = scope.launch(Dispatchers.IO) { runUpdate(id, name, confirm) }
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

    private suspend fun runUpdate(id: Long, name: String, confirm: Boolean) {
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
        settle(id, name, confirm, out)
    }

    private fun note(out: UpdateOutcome, ev: SubscriptionProgress) {
        when (ev.phase) {
            SubscriptionProgress.Phase.PHASE_CONFIRM_REQUIRED -> out.wouldRemove = ev.wouldRemoveCount ?: 0
            SubscriptionProgress.Phase.PHASE_FAILED -> out.failure = SubscriptionLogic.failureOf(ev.detail.orEmpty(), System.currentTimeMillis())
            SubscriptionProgress.Phase.PHASE_DONE -> out.done = true
            else -> Unit
        }
    }

    private suspend fun settle(id: Long, name: String, confirmed: Boolean, out: UpdateOutcome) {
        val ask = out.wouldRemove
        when {
            ask != null && !confirmed -> {
                val known = mutable.value.rows.firstOrNull { it.id == id }?.count ?: 0
                dispatch(SubscriptionsAction.Confirm(SubscriptionsReducer.confirmOf(id, name, ask, known)))
            }
            out.done -> {
                dispatch(SubscriptionsAction.DropFailure(id))
                mutableEvents.tryEmit(SubscriptionsEvent.Updated(name))
                load()
            }
            out.coreProblem != null -> mutableEvents.tryEmit(SubscriptionsEvent.Failed(out.coreProblem ?: ErrorKind.Generic))
            else -> {
                val f = out.failure ?: SubFailure.fromCode(SubFailure.GENERIC_CODE, System.currentTimeMillis())
                dispatch(SubscriptionsAction.NoteFailure(id, f))
                mutableEvents.tryEmit(SubscriptionsEvent.UpdateFailed(f))
            }
        }
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

    /** Creates the subscription and starts its first update: the core makes only an empty group. */
    suspend fun create(name: String, url: String): CoreResult<Unit> {
        val link = url.trim()
        val title = name.trim().ifEmpty { SubscriptionLogic.hostOf(link) }
        return when (val r = session.call { subscriptionCreateSubscription(CreateSubscriptionRequest(title, link)) }) {
            is CoreResult.Fail -> r
            is CoreResult.Ok -> {
                mutableEvents.tryEmit(SubscriptionsEvent.Added(r.value.name))
                rawGroups = rawGroups + (r.value.id to r.value)
                load()
                update(r.value.id)
                CoreResult.Ok(Unit)
            }
        }
    }

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
