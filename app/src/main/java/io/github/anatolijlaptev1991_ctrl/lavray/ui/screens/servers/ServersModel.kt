// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CloneProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CreateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteProfileRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ExportLinksRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.MoveProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Profile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ReorderProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetProfileMetaRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StopTestsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TestLatencyRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileCloneProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileCreateGroup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileDeleteGroup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileDeleteProfile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileExportLinks
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListGroups
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileMoveProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileReorderProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileSetProfileMeta
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileStopTests
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileTestLatency
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileUpdateGroup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionLogic
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.ServerSort
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

/** One-shot results the screen turns into a localized toast. */
sealed interface ServersEvent {
    data class Cloned(val count: Int) : ServersEvent
    data class Deleted(val count: Int) : ServersEvent
    data object Moved : ServersEvent
    data object GroupSaved : ServersEvent

    /** The core refused to delete the connected server, or a group that holds it. */
    data object InUse : ServersEvent
    data class Failed(val kind: ErrorKind) : ServersEvent
}

/**
 * Servers and groups: loads them from the core, applies list actions through the pure [ServersReducer] on a
 * background dispatcher, runs the latency test as a stream (stopped by cancelling it) and publishes immutable
 * state. Nothing here touches the main thread; the screen collects [state] with lifecycle awareness.
 */
class ServersModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
) {
    private val actions = Channel<ServersAction>(Channel.UNLIMITED)
    private val mutable = MutableStateFlow(ServersState())
    private val mutableEvents = MutableSharedFlow<ServersEvent>(extraBufferCapacity = 8)
    private var rawGroups: Map<Long, Group> = emptyMap()
    private var testJob: Job? = null

    val state: StateFlow<ServersState> = mutable.asStateFlow()
    val events: SharedFlow<ServersEvent> = mutableEvents.asSharedFlow()

    init {
        scope.launch(Dispatchers.Default) {
            for (action in actions) mutable.value = ServersReducer.reduce(mutable.value, action)
        }
    }

    fun dispatch(action: ServersAction) {
        actions.trySend(action)
    }

    /** Applies the remembered look settings once, before the first list is built. */
    fun configure(sort: ServerSort, groupBy: Boolean, selectedId: Long) {
        dispatch(ServersAction.Sort(sort))
        dispatch(ServersAction.GroupBy(groupBy))
        dispatch(ServersAction.Selected(selectedId))
    }

    fun refresh() {
        scope.launch(Dispatchers.IO) {
            val groups = session.call { profileListGroups() }
            val profiles = session.call { profileListProfiles(ListProfilesRequest()) }
            when {
                groups is CoreResult.Fail -> dispatch(ServersAction.Failed(groups.kind))
                profiles is CoreResult.Fail -> dispatch(ServersAction.Failed(profiles.kind))
                groups is CoreResult.Ok && profiles is CoreResult.Ok -> {
                    rawGroups = groups.value.groups.associateBy { it.id }
                    dispatch(
                        ServersAction.Loaded(
                            rows = profiles.value.profiles.map(::toRow),
                            groups = groups.value.groups.map { GroupInfo(it.id, SubscriptionLogic.displayName(it.name, it.subscriptionUrl), it.kind == Group.Kind.KIND_SUBSCRIPTION) },
                        ),
                    )
                }
            }
        }
    }

    /** Which server the running tunnel uses, for the "Active" marker. Called when the VPN state changes. */
    fun refreshActive(connectedOrConnecting: Boolean) {
        if (!connectedOrConnecting) {
            dispatch(ServersAction.Active(0L))
            return
        }
        scope.launch(Dispatchers.IO) {
            val st = session.call { coreGetStatus() }
            if (st is CoreResult.Ok) {
                val live = st.value.state == CoreState.CORE_STATE_CONNECTED || st.value.state == CoreState.CORE_STATE_CONNECTING
                dispatch(ServersAction.Active(if (live) st.value.activeProfileId ?: 0L else 0L))
            }
        }
    }

    // ---- latency test

    fun startTest(ids: List<Long>, mode: TestLatencyRequest.Mode) {
        if (ids.isEmpty() || mutable.value.test.running) return
        dispatch(ServersAction.TestStarted(ids.size))
        testJob = scope.launch(Dispatchers.IO) { runTest(ids, mode) }
    }

    private suspend fun runTest(ids: List<Long>, mode: TestLatencyRequest.Mode) {
        val pending = HashMap<Long, LatencyCell>()
        var done = 0
        var total = ids.size
        var lastFlush = 0L
        fun flush() {
            if (pending.isEmpty()) return
            dispatch(ServersAction.Latency(HashMap(pending), done, total))
            pending.clear()
            lastFlush = System.nanoTime()
        }
        try {
            session.stream { profileTestLatency(TestLatencyRequest(profileIds = ids, mode = mode)) }.collect { r ->
                pending[r.profileId] = LatencyCell.from(r.result?.latencyMs, r.result?.lastError)
                done += 1
                if (r.total > 0) total = r.total
                if (System.nanoTime() - lastFlush >= FLUSH_NANOS) flush()
            }
        } catch (e: CancellationException) {
            flush()
            throw e
        } catch (e: StreamFailure) {
            mutableEvents.tryEmit(ServersEvent.Failed(e.kind))
        } finally {
            flush()
            dispatch(ServersAction.TestFinished)
        }
    }

    /** Stops the running test: the stream is cancelled (its socket is aborted) and the core is told to stop. */
    fun stopTest() {
        testJob?.cancel()
        scope.launch(Dispatchers.IO) { session.call { profileStopTests(StopTestsRequest) } }
    }

    // ---- row and group actions

    fun toggleFavorite(row: ServerRow) {
        scope.launch(Dispatchers.IO) {
            val r = session.call { profileSetProfileMeta(SetProfileMetaRequest(id = row.id, favorite = !row.favorite, tags = row.tags)) }
            finish(r) { refresh() }
        }
    }

    fun delete(ids: Set<Long>) {
        scope.launch(Dispatchers.IO) {
            finish(session.call { profileDeleteProfile(DeleteProfileRequest(ids.toList())) }, inUseWhenRefused = true) {
                dispatch(ServersAction.Removed(ids))
                mutableEvents.tryEmit(ServersEvent.Deleted(ids.size))
            }
        }
    }

    fun clone(ids: Set<Long>) {
        scope.launch(Dispatchers.IO) {
            val r = session.call { profileCloneProfiles(CloneProfilesRequest(ids.toList())) }
            finish(r) { mutableEvents.tryEmit(ServersEvent.Cloned((r as CoreResult.Ok).value.newIds.size)); refresh() }
        }
    }

    fun move(ids: Set<Long>, groupId: Long) {
        scope.launch(Dispatchers.IO) {
            finish(session.call { profileMoveProfiles(MoveProfilesRequest(ids.toList(), groupId)) }) {
                mutableEvents.tryEmit(ServersEvent.Moved)
                refresh()
            }
        }
    }

    /** Moves one server a step up or down inside its group, in the group's own order. */
    fun reorder(id: Long, up: Boolean) {
        val rows = mutable.value.rows
        val row = rows.firstOrNull { it.id == id } ?: return
        val order = rows.filter { it.groupId == row.groupId }.map { it.id }.toMutableList()
        val at = order.indexOf(id)
        val to = if (up) at - 1 else at + 1
        if (at < 0 || to !in order.indices) return
        order[at] = order[to].also { order[to] = order[at] }
        scope.launch(Dispatchers.IO) {
            finish(session.call { profileReorderProfiles(ReorderProfilesRequest(row.groupId, order)) }) { refresh() }
        }
    }

    fun createGroup(name: String) {
        scope.launch(Dispatchers.IO) {
            finish(session.call { profileCreateGroup(CreateGroupRequest(Group(name = name.trim(), kind = Group.Kind.KIND_BASIC))) }) {
                mutableEvents.tryEmit(ServersEvent.GroupSaved)
                refresh()
            }
        }
    }

    fun renameGroup(id: Long, name: String) {
        scope.launch(Dispatchers.IO) {
            // UpdateGroup replaces the whole group: send what the core holds now, not the copy loaded earlier.
            val fresh = (session.call { profileListGroups() } as? CoreResult.Ok)?.value?.groups?.firstOrNull { it.id == id }
            val current = fresh ?: rawGroups[id] ?: return@launch
            finish(session.call { profileUpdateGroup(UpdateGroupRequest(current.copy(name = name.trim()))) }) {
                mutableEvents.tryEmit(ServersEvent.GroupSaved)
                refresh()
            }
        }
    }

    fun deleteGroup(id: Long) {
        scope.launch(Dispatchers.IO) {
            finish(session.call { profileDeleteGroup(DeleteGroupRequest(id)) }, inUseWhenRefused = true) { refresh() }
        }
    }

    /** The share link of one server, or null (the failure is reported as an event). */
    suspend fun shareLink(id: Long): String? = when (val r = session.call { profileExportLinks(ExportLinksRequest(ids = listOf(id))) }) {
        is CoreResult.Ok -> r.value.links.firstOrNull()?.takeIf { it.isNotBlank() }
        is CoreResult.Fail -> null
    }.also { if (it == null) mutableEvents.tryEmit(ServersEvent.Failed(ErrorKind.NotFound)) }


    /**
     * A refused change is reported, and the list is read again: the core applies a batch one item at a time and stops
     * at the first refusal, so some of the items may have changed before it said no.
     */
    private inline fun <T> finish(result: CoreResult<T>, inUseWhenRefused: Boolean = false, onOk: () -> Unit) {
        when (result) {
            is CoreResult.Ok -> onOk()
            is CoreResult.Fail -> {
                val inUse = inUseWhenRefused && result.kind == ErrorKind.Precondition
                mutableEvents.tryEmit(if (inUse) ServersEvent.InUse else ServersEvent.Failed(result.kind))
                refresh()
            }
        }
    }

    private fun toRow(p: Profile) = ServerRow(
        id = p.id,
        groupId = p.groupId,
        name = p.name,
        address = p.address,
        port = p.port,
        type = p.type,
        favorite = p.favorite,
        tags = p.tags,
        unsupported = p.unsupportedReason.isNotEmpty(),
        latency = LatencyCell.from(p.testResult?.latencyMs, p.testResult?.lastError),
    )

    private companion object {
        const val FLUSH_NANOS = 150_000_000L
    }
}
