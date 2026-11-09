// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs

import android.content.Context
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.AccentChoice
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ThemeMode
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.AppLanguage

/** How the servers list is sorted; persisted as its name. */
enum class ServerSort { Default, Latency, Name }

/** Everything the UI remembers about looks and list behaviour. Immutable; replace it to change it. */
@Immutable
data class UiSettings(
    val theme: ThemeMode = ThemeMode.SYSTEM,
    val accent: AccentChoice = AccentChoice.AUTO,
    val language: AppLanguage = AppLanguage.System,
    val compactList: Boolean = false,
    val sort: ServerSort = ServerSort.Default,
    val groupBySubscription: Boolean = true,
    val selectedProfileId: Long = 0L,
)

/**
 * SharedPreferences of the UI process (the :vpn process keeps its own). One small file, read once at
 * start because the theme and the language are needed for the first frame; writes are asynchronous.
 */
class UiPrefs(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    fun load(): UiSettings = UiSettings(
        theme = enumOr(KEY_THEME, ThemeMode.SYSTEM),
        accent = enumOr(KEY_ACCENT, AccentChoice.AUTO),
        language = AppLanguage.fromCode(prefs.getString(KEY_LANG, null)),
        compactList = prefs.getBoolean(KEY_COMPACT, false),
        sort = enumOr(KEY_SORT, ServerSort.Default),
        groupBySubscription = prefs.getBoolean(KEY_GROUP, true),
        selectedProfileId = prefs.getLong(KEY_SELECTED, 0L),
    )

    fun save(s: UiSettings) {
        prefs.edit()
            .putString(KEY_THEME, s.theme.name)
            .putString(KEY_ACCENT, s.accent.name)
            .putString(KEY_LANG, s.language.code)
            .putBoolean(KEY_COMPACT, s.compactList)
            .putString(KEY_SORT, s.sort.name)
            .putBoolean(KEY_GROUP, s.groupBySubscription)
            .putLong(KEY_SELECTED, s.selectedProfileId)
            .apply()
    }

    private inline fun <reified E : Enum<E>> enumOr(key: String, fallback: E): E =
        prefs.getString(key, null)?.let { name -> enumValues<E>().firstOrNull { it.name == name } } ?: fallback

    private companion object {
        const val FILE = "ui"
        const val KEY_THEME = "theme"
        const val KEY_ACCENT = "accent"
        const val KEY_LANG = "lang"
        const val KEY_COMPACT = "compact"
        const val KEY_SORT = "sort"
        const val KEY_GROUP = "group_sub"
        const val KEY_SELECTED = "selected_profile"
    }
}
