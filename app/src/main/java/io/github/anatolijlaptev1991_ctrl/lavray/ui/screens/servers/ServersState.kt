// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.ServerSort

/** Outcome of the last latency test of a server. */
enum class LatencyStatus { Unknown, Ok, Down, NotApplicable }

@Immutable
data class LatencyCell(val status: LatencyStatus, val ms: Int = 0) {
    companion object {
        val Unknown = LatencyCell(LatencyStatus.Unknown)

        /** The core marks a test that does not apply (TCP ping of a UDP protocol) with this prefix in last_error. */
        private const val NA_PREFIX = "n/a: "

        fun from(latencyMs: Int?, lastError: String?): LatencyCell = when {
            lastError != null && lastError.startsWith(NA_PREFIX) -> LatencyCell(LatencyStatus.NotApplicable)
            !lastError.isNullOrEmpty() -> LatencyCell(LatencyStatus.Down)
            latencyMs != null && latencyMs > 0 -> LatencyCell(LatencyStatus.Ok, latencyMs)
            else -> Unknown
        }
    }
}

/** What the list needs of a profile. The full DTO (with its protocol details) is dropped right after loading. */
@Immutable
data class ServerRow(
    val id: Long,
    val groupId: Long,
    val name: String,
    val address: String,
    val port: Int,
    val type: ProfileType,
    val favorite: Boolean,
    val tags: List<String>,
    val unsupported: Boolean,
    val latency: LatencyCell,
)

@Immutable
data class GroupInfo(val id: Long, val name: String, val subscription: Boolean)

enum class ServerFilter { All, Favorites, Fast, Down }

/** Group filter: every group, only manually added servers, or one group. */
@Immutable
sealed interface GroupScope {
    data object All : GroupScope
    data object Manual : GroupScope
    data class One(val id: Long) : GroupScope
}

/** Items of the lazy list. Keys are stable: "g<id>" for headers, "p<id>" for rows. */
@Immutable
sealed interface ListItem {
    val key: String

    data class Header(val group: GroupInfo, val count: Int) : ListItem {
        override val key: String get() = "g${group.id}"
    }

    data class Row(val row: ServerRow) : ListItem {
        override val key: String get() = "p${row.id}"
    }
}

@Immutable
data class TestRun(val running: Boolean = false, val done: Int = 0, val total: Int = 0)

@Immutable
data class ServersState(
    val rows: List<ServerRow> = emptyList(),
    val groups: List<GroupInfo> = emptyList(),
    val query: String = "",
    val filter: ServerFilter = ServerFilter.All,
    val sort: ServerSort = ServerSort.Default,
    val groupBy: Boolean = true,
    val scope: GroupScope = GroupScope.All,
    val selection: Set<Long> = emptySet(),
    val test: TestRun = TestRun(),
    val activeId: Long = 0L,
    val selectedId: Long = 0L,
    val loading: Boolean = true,
    val error: ErrorKind? = null,
    val items: List<ListItem> = emptyList(),
) {
    val visibleCount: Int get() = items.count { it is ListItem.Row }
    val selecting: Boolean get() = selection.isNotEmpty()
    val filtering: Boolean get() = query.isNotBlank() || filter != ServerFilter.All || scope != GroupScope.All
}

sealed interface ServersAction {
    data class Loaded(val rows: List<ServerRow>, val groups: List<GroupInfo>) : ServersAction
    data class Failed(val kind: ErrorKind) : ServersAction
    data class Query(val text: String) : ServersAction
    data class Filter(val filter: ServerFilter) : ServersAction
    data class Sort(val sort: ServerSort) : ServersAction
    data class GroupBy(val on: Boolean) : ServersAction
    data class Scope(val scope: GroupScope) : ServersAction
    data class Toggle(val id: Long) : ServersAction
    data object SelectAllVisible : ServersAction
    data object ClearSelection : ServersAction
    data class Removed(val ids: Set<Long>) : ServersAction
    data class Active(val id: Long) : ServersAction
    data class Selected(val id: Long) : ServersAction
    data class TestStarted(val total: Int) : ServersAction
    data class Latency(val results: Map<Long, LatencyCell>, val done: Int, val total: Int) : ServersAction
    data object TestFinished : ServersAction
}

/**
 * The list state machine as pure functions: everything the screen filters, sorts, groups and selects
 * is decided here, on a background dispatcher, so the main thread only draws [ServersState.items].
 * While a test runs, latency updates do not re-sort the list under the finger; it settles when the test ends.
 */
object ServersReducer {
    /** A server counts as fast below this latency, the desktop threshold. */
    const val FAST_MS = 500

    fun reduce(s: ServersState, a: ServersAction): ServersState = when (a) {
        is ServersAction.Loaded -> rebuild(s.copy(rows = a.rows, groups = a.groups, loading = false, error = null))
        is ServersAction.Failed -> s.copy(loading = false, error = a.kind)
        is ServersAction.Query -> rebuild(s.copy(query = a.text))
        is ServersAction.Filter -> rebuild(s.copy(filter = a.filter))
        is ServersAction.Sort -> rebuild(s.copy(sort = a.sort))
        is ServersAction.GroupBy -> rebuild(s.copy(groupBy = a.on))
        is ServersAction.Scope -> rebuild(s.copy(scope = a.scope))
        is ServersAction.Toggle -> s.copy(selection = if (a.id in s.selection) s.selection - a.id else s.selection + a.id)
        ServersAction.SelectAllVisible -> s.copy(selection = s.items.mapNotNullTo(HashSet()) { (it as? ListItem.Row)?.row?.id })
        ServersAction.ClearSelection -> s.copy(selection = emptySet())
        is ServersAction.Removed -> rebuild(
            s.copy(rows = s.rows.filterNot { it.id in a.ids }, selection = s.selection - a.ids),
        )
        is ServersAction.Active -> s.copy(activeId = a.id)
        is ServersAction.Selected -> s.copy(selectedId = a.id)
        is ServersAction.TestStarted -> s.copy(test = TestRun(true, 0, a.total))
        is ServersAction.Latency -> applyLatency(s, a)
        ServersAction.TestFinished -> rebuild(s.copy(test = s.test.copy(running = false)))
    }

    private fun applyLatency(s: ServersState, a: ServersAction.Latency): ServersState {
        val rows = s.rows.map { r -> a.results[r.id]?.let { r.copy(latency = it) } ?: r }
        val next = s.copy(rows = rows, test = TestRun(true, a.done, a.total))
        return if (s.test.running) next.copy(items = refreshRows(s.items, a.results)) else rebuild(next)
    }

    /** Updates latency inside existing list items without changing their order. */
    private fun refreshRows(items: List<ListItem>, results: Map<Long, LatencyCell>): List<ListItem> =
        items.map { item ->
            if (item is ListItem.Row) results[item.row.id]?.let { ListItem.Row(item.row.copy(latency = it)) } ?: item else item
        }

    private fun rebuild(s: ServersState): ServersState = s.copy(items = buildItems(s))

    fun buildItems(s: ServersState): List<ListItem> {
        val subscriptionIds = s.groups.filter { it.subscription }.mapTo(HashSet()) { it.id }
        val query = s.query.trim()
        val filtered = s.rows.filter { r ->
            matchesScope(r, s.scope, subscriptionIds) &&
                matchesFilter(r, s.filter) &&
                (query.isEmpty() || r.name.contains(query, ignoreCase = true) || r.address.contains(query, ignoreCase = true))
        }
        val sorted = sortRows(filtered, s.sort)
        return if (s.groupBy && s.scope == GroupScope.All) grouped(sorted, s.groups) else sorted.map { ListItem.Row(it) }
    }

    private fun matchesScope(r: ServerRow, scope: GroupScope, subscriptionIds: Set<Long>): Boolean = when (scope) {
        GroupScope.All -> true
        GroupScope.Manual -> r.groupId !in subscriptionIds
        is GroupScope.One -> r.groupId == scope.id
    }

    private fun matchesFilter(r: ServerRow, f: ServerFilter): Boolean = when (f) {
        ServerFilter.All -> true
        ServerFilter.Favorites -> r.favorite
        ServerFilter.Fast -> r.latency.status == LatencyStatus.Ok && r.latency.ms < FAST_MS
        ServerFilter.Down -> r.latency.status == LatencyStatus.Down
    }

    /** Down and untested servers go last in latency order, whatever the direction (desktop rule). */
    fun sortRows(rows: List<ServerRow>, sort: ServerSort): List<ServerRow> = when (sort) {
        ServerSort.Default -> rows
        ServerSort.Name -> rows.sortedWith(compareBy(String.CASE_INSENSITIVE_ORDER) { it.name })
        ServerSort.Latency -> rows.sortedWith(
            compareBy<ServerRow> { if (it.latency.status == LatencyStatus.Ok) it.latency.ms else Int.MAX_VALUE }
                .thenBy(String.CASE_INSENSITIVE_ORDER) { it.name },
        )
    }

    private fun grouped(rows: List<ServerRow>, groups: List<GroupInfo>): List<ListItem> {
        val byGroup = rows.groupBy { it.groupId }
        val out = ArrayList<ListItem>(rows.size + groups.size)
        for (g in groups) {
            val members = byGroup[g.id] ?: continue
            out += ListItem.Header(g, members.size)
            members.mapTo(out) { ListItem.Row(it) }
        }
        val known = groups.mapTo(HashSet()) { it.id }
        rows.filter { it.groupId !in known }.mapTo(out) { ListItem.Row(it) }
        return out
    }
}
