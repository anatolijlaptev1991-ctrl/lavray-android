// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Connection

/** Chips of the desktop list (lib/conns/filter.ts): the final outbound of the chain decides. */
enum class ConnChip { All, Proxy, Direct, Block }

enum class ConnStatus { Active, Closed, Blocked }

/** A connection as the list draws it: plain immutable values, mapped once per frame from the core's message. */
@Immutable
data class ConnRow(
    val id: String,
    val status: ConnStatus,
    val destination: String,
    val resolved: String?,
    val outbound: String,
    val chain: List<String>,
    val rule: String?,
    val process: String?,
    val inbound: String?,
    val network: String,
    val uploadBytes: Long,
    val downloadBytes: Long,
    val startedAtMs: Long,
    val endedAtMs: Long?,
) {
    /** Where the traffic really went: the last outbound of the chain, else the first one. */
    val finalOutbound: String get() = chain.lastOrNull()?.takeIf { it.isNotEmpty() } ?: outbound

    val kind: ConnChip
        get() = when {
            status == ConnStatus.Blocked || finalOutbound == BLOCK || outbound == BLOCK -> ConnChip.Block
            finalOutbound == DIRECT || finalOutbound == BYPASS -> ConnChip.Direct
            else -> ConnChip.Proxy
        }

    /** The address to copy: the name the app asked for, which is what a person can recognise. */
    val address: String get() = destination

    fun durationSeconds(nowMs: Long): Long = (((endedAtMs ?: nowMs) - startedAtMs) / 1000).coerceAtLeast(0)

    /** The clock value a row needs: a closed row does not depend on it, so it gets a constant. */
    fun clockFor(nowMs: Long): Long = if (endedAtMs != null) 0L else nowMs

    companion object {
        private const val BLOCK = "block"
        private const val DIRECT = "direct"
        private const val BYPASS = "bypass"

        fun from(c: Connection): ConnRow = ConnRow(
            id = c.id,
            status = when (c.status) {
                Connection.Status.STATUS_CLOSED -> ConnStatus.Closed
                Connection.Status.STATUS_BLOCKED -> ConnStatus.Blocked
                else -> ConnStatus.Active
            },
            destination = c.destination,
            resolved = c.resolvedDestination?.takeIf { it.isNotBlank() && it != c.destination },
            outbound = c.outbound,
            chain = c.chain,
            rule = c.rule?.takeIf { it.isNotBlank() },
            process = c.process?.takeIf { it.isNotBlank() },
            inbound = c.inbound?.takeIf { it.isNotBlank() },
            network = c.network,
            uploadBytes = c.uploadBytes,
            downloadBytes = c.downloadBytes,
            startedAtMs = c.startedAtMs,
            endedAtMs = c.endedAtMs,
        )
    }
}

/** One frame of the stream: the full active list, ids closed since the last frame, and the cap marker. */
@Immutable
data class ConnFrame(val active: List<ConnRow>, val closedIds: Set<String>, val truncated: Boolean)

/** What the screen shows: the active list and the closed history, newest first. */
@Immutable
data class ConnsView(
    val active: List<ConnRow> = emptyList(),
    val closed: List<ConnRow> = emptyList(),
    val truncated: Boolean = false,
    val nowMs: Long = 0L,
)

/** The live view plus, while paused, the snapshot taken at the pause. The stream keeps feeding [live] meanwhile. */
@Immutable
data class ConnsState(val live: ConnsView = ConnsView(), val frozen: ConnsView? = null) {
    val shown: ConnsView get() = frozen ?: live
    val paused: Boolean get() = frozen != null
}

sealed interface ConnsAction {
    data class Frame(val frame: ConnFrame, val nowMs: Long) : ConnsAction
    data class Pause(val paused: Boolean) : ConnsAction
    data object Reset : ConnsAction
}

/**
 * Pure state machine of the Connections list, a port of desktop lib/conns/model.ts. A frame replaces the active
 * list; connections that disappeared go to the history by their last known copy. Pausing freezes what is shown,
 * not what is received, so nothing is lost.
 */
object ConnsReducer {
    /** The UI remembers this many closed connections (desktop 1000; a phone list needs far fewer). */
    const val HISTORY_CAPACITY = 300

    fun reduce(state: ConnsState, action: ConnsAction): ConnsState = when (action) {
        is ConnsAction.Frame -> state.copy(live = applyFrame(state.live, action.frame, action.nowMs))
        is ConnsAction.Pause ->
            if (action.paused) state.copy(frozen = state.frozen ?: state.live) else state.copy(frozen = null)
        ConnsAction.Reset -> ConnsState(live = ConnsView(), frozen = null)
    }

    private fun applyFrame(view: ConnsView, frame: ConnFrame, nowMs: Long): ConnsView {
        val moved = closedSince(view, frame).map { markClosed(it, nowMs) }
        return ConnsView(frame.active, pushHistory(view.closed, moved), frame.truncated, nowMs)
    }

    /**
     * Previously active connections that are gone: named in closedIds or, when the list is complete (not truncated),
     * simply missing (a reconnected stream computes closedIds against its own previous frame).
     */
    private fun closedSince(view: ConnsView, frame: ConnFrame): List<ConnRow> {
        val still = frame.active.mapTo(HashSet()) { it.id }
        return view.active.filter { it.id !in still && (it.id in frame.closedIds || !frame.truncated) }
    }

    private fun markClosed(c: ConnRow, nowMs: Long): ConnRow = c.copy(
        status = if (c.status == ConnStatus.Blocked) c.status else ConnStatus.Closed,
        endedAtMs = c.endedAtMs ?: nowMs,
    )

    /** New ids only, newest first, oldest dropped over the capacity; repeating an id changes nothing. */
    fun pushHistory(history: List<ConnRow>, moved: List<ConnRow>, capacity: Int = HISTORY_CAPACITY): List<ConnRow> {
        if (moved.isEmpty()) return history
        val known = history.mapTo(HashSet()) { it.id }
        val fresh = moved.filter { known.add(it.id) }.sortedByDescending { it.endedAtMs ?: 0L }
        if (fresh.isEmpty()) return history
        return (fresh + history).take(capacity)
    }
}

/** Chip and search filter (desktop lib/conns/filter.ts), applied to the active list and the history alike. */
object ConnFilter {
    fun apply(list: List<ConnRow>, chip: ConnChip, query: String): List<ConnRow> {
        val needle = query.trim().lowercase()
        if (chip == ConnChip.All && needle.isEmpty()) return list
        return list.filter { (chip == ConnChip.All || it.kind == chip) && matches(it, needle) }
    }

    private fun matches(c: ConnRow, needle: String): Boolean {
        if (needle.isEmpty()) return true
        return sequenceOf(c.destination, c.resolved, c.outbound, c.chain.joinToString(" "), c.rule, c.process)
            .any { it?.lowercase()?.contains(needle) == true }
    }
}
