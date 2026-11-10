// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RollbackSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateAllSubscriptionsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileUpdateGroup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsGetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionDeleteSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionListSubscriptions
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionRollbackSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionUpdateAllSubscriptions
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionUpdateSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Subscriptions: loads the groups, runs updates as streams that live only while the screen collects them
 * (the screen cancels them when it stops), applies list changes through the pure [SubscriptionsReducer]
 * on a background dispatcher and publishes immutable state. Nothing here touches the main thread.
 */
class SubscriptionsModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val store: SubFailureStorage,
    /** Called when an update, a rollback or a delete changed the servers, so screens that list them can reload. */
    private val onServersChanged: () -> Unit = {},
) {
    private val registry = SubscriptionRegistry(session)
    private val actions = Channel<SubscriptionsAction>(Channel.UNLIMITED)
    private val mutable = MutableStateFlow(SubscriptionsState())
    private val mutableEvents = MutableSharedFlow<SubscriptionsEvent>(extraBufferCapacity = 8)
    private var rawGroups: Map<Long, Group> = emptyMap()
    private var runJob: Job? = null
    private val running = AtomicBoolean(false)

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
                val counts = SubscriptionCounts.perGroup(session, groups)
                dispatch(SubscriptionsAction.Loaded(groups.map { SubRow.from(it, counts[it.id] ?: it.profileOrder.size) }))
            }
        }
    }

    // ---- updates

    /**
     * One update at a time, decided here and not by the published state (that one is set a moment later): a second request
     * while one runs is refused AND said, so a button that did nothing never looks like it worked.
     */
    private fun claimRun(): Boolean {
        if (running.compareAndSet(false, true)) return true
        mutableEvents.tryEmit(SubscriptionsEvent.Busy)
        return false
    }

    fun update(id: Long, confirm: Boolean = false) {
        if (!claimRun()) return
        val name = rawGroups[id]?.name.orEmpty()
        runJob = scope.launch(Dispatchers.IO) {
            try {
                settle(id, name, confirm, runUpdate(id, name, confirm))
            } finally {
                running.set(false)
            }
        }
    }

    fun updateAll() {
        if (!claimRun()) return
        runJob = scope.launch(Dispatchers.IO) {
            try {
                runUpdateAll()
            } finally {
                running.set(false)
            }
        }
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
                out.note(ev)
            }
        } catch (e: CancellationException) {
            dispatch(SubscriptionsAction.UpdateFinished)
            throw e
        } catch (e: StreamFailure) {
            out.failed(e)
        }
        dispatch(SubscriptionsAction.UpdateFinished)
        return out.toResult()
    }

    private suspend fun settle(id: Long, name: String, confirmed: Boolean, result: FetchResult) {
        when {
            result is FetchResult.NeedsConfirm && !confirmed -> askToConfirm(id, name, result.remove)
            result is FetchResult.Done -> {
                dispatch(SubscriptionsAction.DropFailure(id))
                mutableEvents.tryEmit(SubscriptionsEvent.Updated(name))
                load()
                onServersChanged()
            }
            // The scheduler of the core is updating it: not a failure, so no failure badge is left on the card.
            result is FetchResult.AlreadyRunning -> mutableEvents.tryEmit(SubscriptionsEvent.Busy)
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
        onServersChanged()
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
        scope.launch(Dispatchers.IO) {
            val g = freshGroup(row.id) ?: return@launch
            finish(session.call { profileUpdateGroup(UpdateGroupRequest(SubscriptionEdit.withAutoUpdate(g, !row.autoUpdate))) }) { load() }
        }
    }

    /**
     * UpdateGroup replaces the whole group, so it must be sent what the core holds now: a name or a switch changed on
     * another screen since this list was loaded would otherwise be put back. Falls back to the loaded copy.
     */
    private suspend fun freshGroup(id: Long): Group? =
        ((session.call { subscriptionListSubscriptions() } as? CoreResult.Ok)?.value?.groups?.firstOrNull { it.id == id }) ?: rawGroups[id]

    fun delete(row: SubRow) {
        scope.launch(Dispatchers.IO) {
            val r = session.call { subscriptionDeleteSubscription(DeleteSubscriptionRequest(row.id)) }
            if (r is CoreResult.Fail && r.kind == ErrorKind.Precondition) {
                mutableEvents.tryEmit(SubscriptionsEvent.InUse)
                return@launch
            }
            finish(r) {
                dispatch(SubscriptionsAction.Removed(row.id))
                mutableEvents.tryEmit(SubscriptionsEvent.Deleted)
                load()
                onServersChanged()
            }
        }
    }

    fun rollback(row: SubRow) {
        scope.launch(Dispatchers.IO) {
            when (val r = session.call { subscriptionRollbackSubscription(RollbackSubscriptionRequest(row.id)) }) {
                // The snapshot to go back to lives in the memory of the core only: after a core restart there is none, and while an
                // update of this subscription runs there is no rollback either. Both come as FAILED_PRECONDITION.
                is CoreResult.Fail -> mutableEvents.tryEmit(
                    if (r.kind == ErrorKind.Precondition) SubscriptionsEvent.RollbackUnavailable else SubscriptionsEvent.Failed(r.kind),
                )
                // The core answers a rollback that worked with an empty message (success stays false, no count): only a
                // reported error means it did not work. The number of servers is then read from the group itself.
                is CoreResult.Ok -> if (r.value.error != null) {
                    mutableEvents.tryEmit(SubscriptionsEvent.RollbackFailed)
                } else {
                    val count = r.value.restoredCount ?: SubscriptionCounts.of(session, row.id)
                    mutableEvents.tryEmit(SubscriptionsEvent.RolledBack(row.name, count))
                    load()
                    onServersChanged()
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
        // The same claim as every other update: two imports in the window before the state says "busy" would make two groups.
        if (!running.compareAndSet(false, true)) return SubscribeResult.Busy
        try {
            val registered = when (val r = registry.register(link.trim())) {
                is CoreResult.Fail -> return SubscribeResult.Refused(r.kind, r.line)
                is CoreResult.Ok -> r.value
            }
            val group = registered.group
            rawGroups = rawGroups + (group.id to group)
            val notes = SubNotes(registered.duplicateLeft, insecure = group.subscriptionUrl.orEmpty().trimStart().startsWith("http://", ignoreCase = true))
            return finishAdd(group, registered.existed, notes, runUpdate(group.id, group.name, confirm = false))
        } finally {
            running.set(false)
        }
    }

    /** Reloads the subscription list only: whoever called [addAndFetch] reloads the servers once, when it knows what changed. */
    private suspend fun finishAdd(group: Group, existed: Boolean, notes: SubNotes, result: FetchResult): SubscribeResult {
        val id = group.id
        val out = when (result) {
            is FetchResult.Done -> {
                dispatch(SubscriptionsAction.DropFailure(id))
                SubscribeResult.Fetched(group.name, existed, result.added, result.removed, result.total, result.unparsedLines, notes)
            }
            is FetchResult.NeedsConfirm -> {
                askToConfirm(id, group.name, result.remove)
                SubscribeResult.NeedsConfirm(group.name, existed, result.remove, notes)
            }
            is FetchResult.AlreadyRunning -> SubscribeResult.Updating(group.name, existed, notes)
            is FetchResult.Failed -> {
                if (result.coreProblem == null) dispatch(SubscriptionsAction.NoteFailure(id, result.failure))
                SubscribeResult.NotFetched(group.name, existed, result.failure, deviceLimitMayApply(result.failure), result.coreProblem, notes)
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
        val g = freshGroup(row.id) ?: return CoreResult.Fail(ErrorKind.NotFound)
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

    private inline fun <T> finish(result: CoreResult<T>, onOk: () -> Unit) {
        when (result) {
            is CoreResult.Fail -> mutableEvents.tryEmit(SubscriptionsEvent.Failed(result.kind))
            is CoreResult.Ok -> onOk()
        }
    }
}
