// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.nav

import androidx.compose.runtime.Immutable

/**
 * The whole navigation state as plain data, so it can be tested on the JVM and saved across process
 * death. A section keeps its own selected tab; [pushed] is the stack of full-screen destinations above
 * the current section (statistics, backup, QR scan, ...). Tabs are not back entries.
 *
 * Back order: pop a pushed screen, then leave a non-start section for the start section, then exit.
 */
@Immutable
data class NavState(
    val section: String = START_SECTION,
    val tabs: Map<String, String> = emptyMap(),
    val pushed: List<String> = emptyList(),
) {
    val canPop: Boolean get() = pushed.isNotEmpty() || section != START_SECTION

    /** Re-selecting the current section clears pushed screens; a different one switches and clears them too. */
    fun selectSection(id: String): NavState =
        if (id == section && pushed.isEmpty()) this else copy(section = id, pushed = emptyList())

    fun selectTab(sectionId: String, tabId: String): NavState =
        if (tabs[sectionId] == tabId) this else copy(tabs = tabs + (sectionId to tabId))

    fun push(id: String): NavState = if (pushed.lastOrNull() == id) this else copy(pushed = pushed + id)

    /** The state after Back, or null when Back should leave the app. */
    fun pop(): NavState? = when {
        pushed.isNotEmpty() -> copy(pushed = pushed.dropLast(1))
        section != START_SECTION -> copy(section = START_SECTION)
        else -> null
    }

    fun tabOf(sectionId: String, default: String): String = tabs[sectionId] ?: default

    /** Flat form for saved instance state; [fromList] is its inverse. */
    fun toList(): List<String> = buildList {
        add(section)
        add(pushed.joinToString(SEP))
        tabs.forEach { (s, t) -> add("$s$KV$t") }
    }

    companion object {
        const val START_SECTION = "connection"
        private const val SEP = ","
        private const val KV = "="

        fun fromList(raw: List<String>): NavState {
            if (raw.size < 2) return NavState()
            val tabs = raw.drop(2).mapNotNull { e -> e.split(KV, limit = 2).takeIf { it.size == 2 }?.let { it[0] to it[1] } }.toMap()
            return NavState(raw[0], tabs, raw[1].split(SEP).filter { it.isNotEmpty() })
        }
    }
}
