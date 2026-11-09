// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.nav

import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue

/** Screens talk to navigation only through this; the state itself is the pure [NavState]. */
@Stable
class Navigator(initial: NavState = NavState()) {
    var state: NavState by mutableStateOf(initial)
        private set

    fun openSection(id: String, tab: String? = null) {
        var next = state.selectSection(id)
        if (tab != null) next = next.selectTab(id, tab)
        state = next
    }

    fun openTab(sectionId: String, tabId: String) {
        state = state.selectTab(sectionId, tabId)
    }

    fun push(id: String) {
        state = state.push(id)
    }

    /** Back: returns false when nothing is left to pop and the system should leave the app. */
    fun back(): Boolean {
        val next = state.pop() ?: return false
        state = next
        return true
    }

    companion object {
        val Saver: Saver<Navigator, List<String>> = Saver(
            save = { it.state.toList() },
            restore = { Navigator(NavState.fromList(it)) },
        )
    }
}

val LocalNavigator = compositionLocalOf { Navigator() }

@Composable
fun rememberNavigator(): Navigator = rememberSaveable(saver = Navigator.Saver) { Navigator() }
