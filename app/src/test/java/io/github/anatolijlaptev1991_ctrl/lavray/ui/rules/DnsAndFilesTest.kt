// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsReducer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsRequests
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetFiles
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DnsAndFilesTest {
    private val simple = DnsSettings(mode = DnsSettings.Mode.MODE_SIMPLE, choice = "doh", remoteDns = "https://dns.google/dns-query", directDns = "local", routingEnabled = true)

    @Test
    fun theActiveCardIsTheStoredChoiceAutoByDefaultAndAdvancedInJsonMode() {
        assertEquals("", DnsRequests.activeCard(null))
        assertEquals("doh", DnsRequests.activeCard(simple))
        assertEquals("auto", DnsRequests.activeCard(DnsSettings(mode = DnsSettings.Mode.MODE_SIMPLE)))
        assertEquals("advanced", DnsRequests.activeCard(DnsSettings(mode = DnsSettings.Mode.MODE_ADVANCED, choice = "doh")))
        assertEquals("custom", DnsRequests.activeCard(DnsSettings(choice = "custom")))
    }

    @Test
    fun aCardRequestCarriesOnlyTheModeAndTheChoice() {
        val r = DnsRequests.card(DnsCard.Tunnel)
        assertEquals(DnsSettings.Mode.MODE_SIMPLE, r.mode)
        assertEquals("tunnel", r.choice)
        assertNull(r.remoteDns)
        assertEquals(listOf("auto", "tunnel", "doh", "dot", "local"), DnsCard.entries.map { it.id })
    }

    @Test
    fun ownServersKeepTheDirectDnsAndDnsRoutingOfTheCurrentSettings() {
        val r = DnsRequests.custom(simple, "  tls://1.1.1.1 ")
        assertEquals("custom", r.choice)
        assertEquals("tls://1.1.1.1", r.remoteDns)
        assertEquals("local", r.directDns)
        assertTrue(r.routingEnabled)
        assertFalse(DnsRequests.custom(null, "x").routingEnabled)
    }

    @Test
    fun theAdvancedRequestCarriesTheJsonAndKeepsDnsRouting() {
        val r = DnsRequests.advanced(simple, "{\"servers\":[]}")
        assertEquals(DnsSettings.Mode.MODE_ADVANCED, r.mode)
        assertEquals("{\"servers\":[]}", r.advancedJson)
        assertTrue(r.routingEnabled)
    }

    @Test
    fun loadingOpensTheJsonEditorOnlyForAdvancedMode() {
        val simpleState = DnsReducer.reduce(DnsState(), DnsAction.Loaded(simple))
        assertFalse(simpleState.loading)
        assertFalse(simpleState.advanced)
        assertEquals("https://dns.google/dns-query", simpleState.remote)
        val adv = DnsReducer.reduce(DnsState(), DnsAction.Loaded(DnsSettings(mode = DnsSettings.Mode.MODE_ADVANCED, advancedJson = "{}")))
        assertTrue(adv.advanced)
        assertEquals("{}", adv.json)
    }

    @Test
    fun problemsFromTheCoreShowUntilTheTextChanges() {
        var s = DnsReducer.reduce(DnsState(), DnsAction.Problems(listOf("dns.servers[0]: bad type", " ")))
        assertTrue(s.invalid)
        assertEquals(listOf("dns.servers[0]: bad type"), s.problems)
        s = DnsReducer.reduce(s, DnsAction.Json("{}"))
        assertFalse(s.invalid)
        assertTrue(s.problems.isEmpty())
    }

    @Test
    fun aRejectionWithoutAMessageStillCountsAsInvalid() {
        val s = DnsReducer.reduce(DnsState(), DnsAction.Problems(emptyList()))
        assertTrue(s.invalid)
        assertTrue(s.problems.isEmpty())
    }

    @Test
    fun aSavedValueReplacesTheCoresCopyAndKeepsWhatIsBeingTyped() {
        val typing = DnsReducer.reduce(DnsReducer.reduce(DnsState(), DnsAction.Loaded(simple)), DnsAction.Json("{ \"a\": 1 }"))
        val saved = DnsReducer.reduce(typing, DnsAction.Applied(DnsSettings(mode = DnsSettings.Mode.MODE_SIMPLE, choice = "local")))
        assertEquals("local", saved.activeCard)
        assertEquals("{ \"a\": 1 }", saved.json)
        assertEquals("https://dns.google/dns-query", saved.remote)
    }

    @Test
    fun aFailedLoadShowsTheProblemOnly() {
        val s = DnsReducer.reduce(DnsState(), DnsAction.Failed(ErrorKind.CoreUnavailable))
        assertFalse(s.loading)
        assertEquals(ErrorKind.CoreUnavailable, s.error)
        assertNull(s.dns)
    }

    @Test
    fun fakeDnsIsReadOnlyInformation() {
        assertTrue(DnsReducer.reduce(DnsState(), DnsAction.Loaded(DnsSettings(fakeDnsEnabled = true))).fakeDns)
        assertFalse(DnsState().fakeDns)
    }

    // ---- preset files

    @Test
    fun aFileUnderTheLimitIsReadAsUtf8() {
        val text = "{\"name\":\"Привет\"}"
        assertEquals(text, PresetFiles.readLimited(text.toByteArray(Charsets.UTF_8).inputStream()))
    }

    @Test
    fun aByteOrderMarkIsDropped() {
        val bytes = byteArrayOf(0xEF.toByte(), 0xBB.toByte(), 0xBF.toByte()) + "{}".toByteArray()
        assertEquals("{}", PresetFiles.readLimited(bytes.inputStream()))
    }

    @Test
    fun aFileOverTheLimitIsRefusedWithoutReadingItAll() {
        assertNull(PresetFiles.readLimited(ByteArray(11) { 'a'.code.toByte() }.inputStream(), max = 10))
        assertEquals("aaaaaaaaaa", PresetFiles.readLimited(ByteArray(10) { 'a'.code.toByte() }.inputStream(), max = 10))
        assertEquals(1 shl 20, PresetFiles.MAX_BYTES)
    }

    @Test
    fun bytesThatAreNotUtf8AreRefused() {
        assertNull(PresetFiles.readLimited(byteArrayOf(0xC3.toByte(), 0x28).inputStream()))
    }

    @Test
    fun theSaveNameIsSafeAndEndsInJson() {
        assertEquals("lav-ru.json", PresetFiles.exportFileName("lav-ru"))
        assertEquals("my-set.json", PresetFiles.exportFileName("my set.json"))
        assertEquals("preset.json", PresetFiles.exportFileName("  "))
        assertEquals("a-b.json", PresetFiles.exportFileName("../a/b"))
    }
}
