// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import java.util.Locale
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Checks the committed output of tools/i18n on the JVM, the way the app sees it: the five languages carry the same
 * names, the same positional arguments, plural categories that fit the language, and none of the words that must
 * never reach the screen. (The generator has its own Node tests; this guards the files themselves.)
 */
class GeneratedStringsTest {
    private val dirs = linkedMapOf("ru" to "values", "en" to "values-en", "zh" to "values-zh", "es" to "values-es", "de" to "values-de")

    private class Res(val strings: Map<String, String>, val plurals: Map<String, Map<String, String>>)

    private fun load(dir: String): Res {
        val file = listOf(File("src/main/res/$dir/strings.xml"), File("app/src/main/res/$dir/strings.xml")).first { it.exists() }
        val root = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file).documentElement
        val strings = LinkedHashMap<String, String>()
        val plurals = LinkedHashMap<String, Map<String, String>>()
        val nodes = root.childNodes
        for (i in 0 until nodes.length) {
            val e = nodes.item(i) as? Element ?: continue
            when (e.tagName) {
                "string" -> strings[e.getAttribute("name")] = e.textContent
                "plurals" -> {
                    val items = e.getElementsByTagName("item")
                    plurals[e.getAttribute("name")] = (0 until items.length).associate {
                        val item = items.item(it) as Element
                        item.getAttribute("quantity") to item.textContent
                    }
                }
            }
        }
        return Res(strings, plurals)
    }

    private val all: Map<String, Res> by lazy { dirs.mapValues { load(it.value) } }
    private val positional = Regex("%(\\d+)[$]s")

    @Test
    fun allFiveLanguagesCarryTheSameNames() {
        val ru = all.getValue("ru")
        assertTrue("ru should not be empty", ru.strings.size > 100)
        for ((lang, res) in all) {
            assertEquals("string names of $lang", ru.strings.keys, res.strings.keys)
            assertEquals("plural names of $lang", ru.plurals.keys, res.plurals.keys)
        }
    }

    @Test
    fun positionalArgumentsAreTheSameInEveryLanguage() {
        val ru = all.getValue("ru")
        for ((lang, res) in all) {
            for ((name, text) in res.strings) {
                val expected = positional.findAll(ru.strings.getValue(name)).map { it.groupValues[1] }.toSet()
                val actual = positional.findAll(text).map { it.groupValues[1] }.toSet()
                assertEquals("args of $name in $lang", expected, actual)
            }
        }
    }

    @Test
    fun argumentNumbersAreDenseFromOne() {
        for ((lang, res) in all) {
            for ((name, text) in res.strings) {
                val numbers = positional.findAll(text).map { it.groupValues[1].toInt() }.toSet()
                if (numbers.isNotEmpty()) assertEquals("dense args of $name in $lang", (1..numbers.max()).toSet(), numbers)
            }
        }
    }

    @Test
    fun russianPluralsHaveFourCategoriesAndPickTheRightForm() {
        val p = all.getValue("ru").plurals.getValue("pl_srv")
        assertEquals(setOf("one", "few", "many", "other"), p.keys)
        fun render(category: String, n: Int) = String.format(Locale.forLanguageTag("ru"), p.getValue(category), n.toString())
        assertEquals("1 сервер", render("one", 1))
        assertEquals("3 сервера", render("few", 3))
        assertEquals("5 серверов", render("many", 5))
    }

    @Test
    fun englishAndGermanPluralsHaveOneAndOther() {
        for (lang in listOf("en", "de", "es")) {
            val p = all.getValue(lang).plurals.getValue("pl_srv")
            assertTrue("$lang one", "one" in p && "other" in p)
        }
    }

    @Test
    fun placeholdersFormatWithJavaFormatter() {
        val text = all.getValue("en").strings.getValue("srv_selected")
        assertEquals("Selected: 3", String.format(Locale.US, text, "3"))
        val two = all.getValue("ru").strings.getValue("test_progress")
        assertEquals("Проверено 4 из 9", String.format(Locale.forLanguageTag("ru"), two, "4", "9"))
    }

    @Test
    fun theWordHappAndTheCoreNameNeverReachTheScreen() {
        val happ = Regex("\\bhapp\\b", RegexOption.IGNORE_CASE)
        val core = Regex("sing[- ]?box", RegexOption.IGNORE_CASE)
        for ((lang, res) in all) {
            val texts = res.strings.values + res.plurals.values.flatMap { it.values }
            assertFalse("Happ in $lang", texts.any { happ.containsMatchIn(it) })
            // The licenses screen is the one place where the engine's origin is named (desktop key lic.core).
            val named = res.strings.filter { core.containsMatchIn(it.value) }.keys + res.plurals.filter { e -> e.value.values.any { core.containsMatchIn(it) } }.keys
            assertTrue("core name outside the licenses screen in $lang: $named", named.all { it == "lic_core" })
        }
    }

    @Test
    fun noStringContainsAnUnescapedApostropheOrRawBrace() {
        for ((lang, res) in all) {
            for ((name, text) in res.strings) {
                assertFalse("raw {name} placeholder left in $name ($lang)", Regex("\\{[A-Za-z_]+}").containsMatchIn(text))
            }
        }
    }

    /** The desktop wording "Computer clock" reached the phone's checks screen; only the preset note may mention a computer. */
    @Test
    fun englishPhoneTextDoesNotSpeakOfAComputerExceptWhereItMeansTheDesktopApp() {
        val computer = Regex("computer", RegexOption.IGNORE_CASE)
        val named = all.getValue("en").strings.filter { computer.containsMatchIn(it.value) }.keys
        assertTrue("computer wording in: $named", named.all { it == "ra_proc_note" })
    }

    /** Desktop-only features must not be named on the phone (the restore dialog once spoke of AI agents and the web panel). */
    @Test
    fun englishPhoneTextNamesNoDesktopOnlyFeature() {
        val desktop = Regex("AI agents|web panel|system tray|hotkey|administrator|system proxy", RegexOption.IGNORE_CASE)
        val named = all.getValue("en").strings.filter { desktop.containsMatchIn(it.value) }.keys
        assertTrue("desktop wording in: $named", named.isEmpty())
    }
}
