// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.core

/** Reads the little the UI takes from a core error message. The text itself is never shown. */
object CoreMessages {
    private val lineNumber = Regex("""(?:строка|line)\s+(\d{1,6})""", RegexOption.IGNORE_CASE)

    /** The line the core blamed ("... (первая ошибка: строка 2: ...)"), or null. */
    fun lineOf(message: String?): Int? = message?.let { lineNumber.find(it)?.groupValues?.get(1)?.toIntOrNull() }?.takeIf { it > 0 }
}
