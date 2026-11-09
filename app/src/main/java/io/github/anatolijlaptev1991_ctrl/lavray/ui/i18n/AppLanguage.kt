// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n

import android.app.Activity
import android.app.LocaleManager
import android.content.Context
import android.content.res.Configuration
import android.os.Build
import android.os.LocaleList
import java.util.Locale

/**
 * UI languages. [System] follows the device. The others are named in themselves (endonyms, section 5 of
 * the design system: no flags and no translated language names).
 */
enum class AppLanguage(val code: String, val tag: String, val endonym: String) {
    System("system", "", ""),
    Ru("ru", "ru", "Русский"),
    En("en", "en", "English"),
    Zh("zh", "zh-CN", "简体中文"),
    Es("es", "es", "Español"),
    De("de", "de", "Deutsch");

    val isCjk: Boolean get() = this == Zh

    companion object {
        fun fromCode(code: String?): AppLanguage = entries.firstOrNull { it.code == code } ?: System

        /** The language of [tag] (such as zh-CN), if it is one we ship. */
        fun fromTag(tag: String?): AppLanguage =
            entries.firstOrNull { it != System && !tag.isNullOrEmpty() && tag.substringBefore('-').equals(it.code, true) } ?: System
    }
}

/**
 * In-app language without AppCompat. From API 33 the system keeps the per-app locale (and shows it in
 * system settings, see res/xml/locales_config.xml); on API 26-32 the activity wraps its base context in a
 * configuration with the chosen locale and is recreated when the choice changes.
 */
object AppLocale {
    /** Wrap for attachBaseContext; a no-op for [AppLanguage.System] and on API 33+. */
    fun wrap(base: Context, language: AppLanguage): Context {
        if (language == AppLanguage.System || Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) return base
        val locale = Locale.forLanguageTag(language.tag)
        Locale.setDefault(locale)
        val config = Configuration(base.resources.configuration)
        config.setLocale(locale)
        config.setLayoutDirection(locale)
        return base.createConfigurationContext(config)
    }

    /** The language in force on API 33+ (the system owns it there); null below, where the saved choice is the truth. */
    fun systemChoice(context: Context): AppLanguage? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return null
        val locales = context.getSystemService(LocaleManager::class.java)?.applicationLocales ?: return null
        return if (locales.isEmpty) AppLanguage.System else AppLanguage.fromTag(locales[0].toLanguageTag())
    }

    /** Applies [language]; on API 33+ the system recreates the activity, below we do it. */
    fun apply(activity: Activity, language: AppLanguage) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val list = if (language == AppLanguage.System) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(language.tag)
            activity.getSystemService(LocaleManager::class.java)?.applicationLocales = list
        } else {
            activity.recreate()
        }
    }

    /** The language the UI is shown in right now, for formatting and CJK line height. */
    fun effective(context: Context, chosen: AppLanguage): AppLanguage {
        if (chosen != AppLanguage.System) return chosen
        return AppLanguage.fromTag(context.resources.configuration.locales[0].toLanguageTag())
    }
}
