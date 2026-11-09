// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProcessRule
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset

/** The six rule lists of a preset: where a list sends traffic and whether its entries are domains or addresses. */
enum class ListKind(val outbound: Outbound, val ip: Boolean) {
    BlockDomain(Outbound.OUTBOUND_BLOCK, false),
    ProxyDomain(Outbound.OUTBOUND_PROXY, false),
    DirectDomain(Outbound.OUTBOUND_DIRECT, false),
    BlockIp(Outbound.OUTBOUND_BLOCK, true),
    ProxyIp(Outbound.OUTBOUND_PROXY, true),
    DirectIp(Outbound.OUTBOUND_DIRECT, true);

    fun of(p: RoutingPreset): List<String> = when (this) {
        BlockDomain -> p.blockDomain
        ProxyDomain -> p.proxyDomain
        DirectDomain -> p.directDomain
        BlockIp -> p.blockIp
        ProxyIp -> p.proxyIp
        DirectIp -> p.directIp
    }

    fun with(p: RoutingPreset, entries: List<String>): RoutingPreset = when (this) {
        BlockDomain -> p.copy(blockDomain = entries)
        ProxyDomain -> p.copy(proxyDomain = entries)
        DirectDomain -> p.copy(directDomain = entries)
        BlockIp -> p.copy(blockIp = entries)
        ProxyIp -> p.copy(proxyIp = entries)
        DirectIp -> p.copy(directIp = entries)
    }
}

/** The outcomes offered for "all other traffic", in the desktop order. */
val OUTBOUND_CHOICES: List<Outbound> = listOf(Outbound.OUTBOUND_PROXY, Outbound.OUTBOUND_DIRECT, Outbound.OUTBOUND_BLOCK)

/** One change to a rule list, applied to the newest copy of the preset so queued edits never overwrite each other. */
sealed interface ListEdit {
    val kind: ListKind

    data class Add(override val kind: ListKind, val entry: String) : ListEdit
    data class Remove(override val kind: ListKind, val entry: String) : ListEdit
}

/** What one edit changes: name, default outcome, one list entry, or one app rule. Null fields stay as they are. */
data class PresetPatch(
    val displayName: String? = null,
    val defaultOutbound: Outbound? = null,
    val listEdit: ListEdit? = null,
    val removeProcessRule: ProcessRule? = null,
)

sealed interface AddResult {
    data class Added(val list: List<String>) : AddResult
    data object Exists : AddResult
    data object Empty : AddResult
}

/** Preset edits as pure functions (desktop presetDraft.ts): the edited copy, "did it change", list entry rules. */
object PresetDraft {
    const val MAX_NAME_LENGTH = 64

    /** The preset with the patch applied; the original is not touched. */
    fun apply(base: RoutingPreset, patch: PresetPatch): RoutingPreset {
        var next = base
        patch.displayName?.let { next = next.copy(displayName = it.trim()) }
        patch.defaultOutbound?.let { next = next.copy(defaultOutbound = it) }
        patch.listEdit?.let { next = applyListEdit(next, it) }
        patch.removeProcessRule?.let { rule -> next = next.copy(processRules = removeFirst(next.processRules, rule)) }
        return next
    }

    private fun applyListEdit(p: RoutingPreset, edit: ListEdit): RoutingPreset {
        val current = edit.kind.of(p)
        val entries = when (edit) {
            is ListEdit.Add -> (addEntry(current, edit.entry) as? AddResult.Added)?.list ?: current
            is ListEdit.Remove -> removeEntry(current, edit.entry)
        }
        return edit.kind.with(p, entries)
    }

    private fun <T> removeFirst(list: List<T>, item: T): List<T> {
        val at = list.indexOf(item)
        return if (at < 0) list else list.filterIndexed { i, _ -> i != at }
    }

    /** Source, revision and the core's own flags do not count: an edit that changes only those is not sent. */
    fun isChanged(base: RoutingPreset, next: RoutingPreset): Boolean = strip(base) != strip(next)

    private fun strip(p: RoutingPreset): RoutingPreset =
        p.copy(revision = 0, builtinSourceId = "", builtinSourceRevision = 0, sourceUpdated = false)

    /** Appends an entry; an empty line and a repeat (ignoring case) change nothing. */
    fun addEntry(list: List<String>, raw: String): AddResult {
        val entry = raw.trim()
        return when {
            entry.isEmpty() -> AddResult.Empty
            list.any { it.equals(entry, ignoreCase = true) } -> AddResult.Exists
            else -> AddResult.Added(list + entry)
        }
    }

    fun removeEntry(list: List<String>, entry: String): List<String> = list.filter { it != entry }

    /** Name of a copy: the dictionary pattern "{name} (copy)" is applied by the caller. */
    fun cleanName(raw: String): String = raw.trim().take(MAX_NAME_LENGTH)

    /** A name is acceptable when something is left after trimming. */
    fun nameValid(raw: String): Boolean = raw.isNotBlank()
}
