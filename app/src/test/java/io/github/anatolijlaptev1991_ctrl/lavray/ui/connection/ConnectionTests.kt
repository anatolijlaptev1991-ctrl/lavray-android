// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SessionStat
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StatsSnapshot
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TagStat
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.errorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.AppLanguage
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitLabels
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TrafficReading
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.apply
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.isLive
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.util.Locale

class PhaseTest {
    @Test
    fun everyServiceStateMapsToAPhaseAndMarkState() {
        assertEquals(Phase.Off, phaseOf(VpnState.IDLE))
        assertEquals(Phase.On, phaseOf(VpnState.CONNECTED))
        assertEquals(MarkState.Connecting, phaseOf(VpnState.CONNECTING).mark)
        assertEquals(MarkState.Connecting, phaseOf(VpnState.WAITING_NETWORK).mark)
        assertEquals(MarkState.Connecting, phaseOf(VpnState.DISCONNECTING).mark)
        assertEquals(MarkState.Error, phaseOf(VpnState.ERROR).mark)
        assertEquals(MarkState.Error, phaseOf(VpnState.NEEDS_PERMISSION).mark)
        assertEquals(VpnState.entries.size, VpnState.entries.map { phaseOf(it) }.size)
    }

    @Test
    fun buttonCapabilities() {
        assertTrue(Phase.Off.canConnect && !Phase.Off.canDisconnect)
        assertTrue(Phase.On.canDisconnect && !Phase.On.canConnect)
        assertTrue(Phase.Connecting.canDisconnect)
        assertFalse(Phase.Disconnecting.canDisconnect || Phase.Disconnecting.canConnect)
        assertTrue(Phase.Error.canConnect && Phase.NeedsPermission.canConnect)
    }

    @Test
    fun onlyAnUpTunnelSubscribesToStats() {
        assertTrue(Phase.On.isLive)
        assertFalse(Phase.Off.isLive || Phase.Connecting.isLive || Phase.Error.isLive)
    }
}

class TrafficReadingTest {
    private fun snap(tags: List<TagStat>, session: SessionStat? = null, final: Boolean = false) =
        StatsSnapshot(tsMs = 1, tags = tags, session = session, final = final)

    @Test
    fun speedIsTheSumOverAllTagsAndTotalsComeFromTheSession() {
        val r = TrafficReading.Empty.apply(
            snap(
                listOf(TagStat("proxy", uplinkBps = 10.0, downlinkBps = 100.0), TagStat("direct", uplinkBps = 5.0, downlinkBps = 50.0)),
                SessionStat(startedAtMs = 1, uplinkTotal = 1000, downlinkTotal = 9000),
            ),
        )
        assertEquals(150.0, r.downBps, 0.0)
        assertEquals(15.0, r.upBps, 0.0)
        assertEquals(9000L, r.downTotal)
        assertEquals(1000L, r.upTotal)
    }

    @Test
    fun aFrameWithoutSessionKeepsTheTotalsAndAFinalFrameZeroesTheSpeed() {
        val a = TrafficReading.Empty.apply(snap(listOf(TagStat("p", downlinkBps = 7.0)), SessionStat(startedAtMs = 1, downlinkTotal = 70)))
        val b = a.apply(snap(listOf(TagStat("p", downlinkBps = 9.0))))
        assertEquals(70L, b.downTotal)
        assertEquals(9.0, b.downBps, 0.0)
        val c = b.apply(snap(listOf(TagStat("p", downlinkBps = 9.0)), final = true))
        assertEquals(0.0, c.downBps, 0.0)
        assertEquals(70L, c.downTotal)
    }
}

class UnitFormatTest {
    private val ru = UnitFormat(
        UnitLabels("Б", "КБ", "МБ", "ГБ", "Б/с", "КБ/с", "МБ/с", "ГБ/с", "мс"),
        Locale.forLanguageTag("ru-RU"),
    )
    private val en = UnitFormat(
        UnitLabels("B", "KB", "MB", "GB", "B/s", "KB/s", "MB/s", "GB/s", "ms"),
        Locale.US,
    )
    private val nb = " "

    @Test
    fun bytesUseDecimalScaleDictionaryUnitsAndANonBreakingSpace() {
        assertEquals("0${nb}B", en.bytes(0))
        assertEquals("999${nb}B", en.bytes(999))
        assertEquals("1.2${nb}KB", en.bytes(1_234))
        assertEquals("1.5${nb}GB", en.bytes(1_500_000_000))
        assertEquals("1,2${nb}МБ", ru.bytes(1_234_567))
    }

    @Test
    fun speedAndLatency() {
        assertEquals("0${nb}B/s", en.speed(0.0))
        assertEquals("0${nb}B/s", en.speed(Double.NaN))
        assertEquals("2.5${nb}MB/s", en.speed(2_500_000.0))
        assertEquals("43${nb}ms", en.latency(43))
        assertEquals("43${nb}мс", ru.latency(43))
    }

    @Test
    fun clockIsMinutesSecondsThenHours() {
        assertEquals("00:00", en.clock(0))
        assertEquals("01:05", en.clock(65))
        assertEquals("59:59", en.clock(3599))
        assertEquals("1:00:00", en.clock(3600))
        assertEquals("00:00", en.clock(-5))
    }
}

class ErrorKindTest {
    @Test
    fun coreAndTransportFailuresMapToKinds() {
        assertEquals(ErrorKind.CoreUnavailable, ApiException(ConnectCode.UNAVAILABLE, "x").errorKind())
        assertEquals(ErrorKind.CoreUnavailable, ApiException(ConnectCode.UNAUTHENTICATED, "x").errorKind())
        assertEquals(ErrorKind.NotImplemented, ApiException(ConnectCode.UNIMPLEMENTED, "x").errorKind())
        assertEquals(ErrorKind.Precondition, ApiException(ConnectCode.FAILED_PRECONDITION, "x").errorKind())
        assertEquals(ErrorKind.InvalidInput, ApiException(ConnectCode.INVALID_ARGUMENT, "x").errorKind())
        assertEquals(ErrorKind.NotFound, ApiException(ConnectCode.NOT_FOUND, "x").errorKind())
        assertEquals(ErrorKind.Generic, ApiException(ConnectCode.INTERNAL, "x").errorKind())
        assertEquals(ErrorKind.CoreUnavailable, IOException("socket").errorKind())
        assertEquals(ErrorKind.Generic, IllegalStateException("boom").errorKind())
    }
}

class AppLanguageTest {
    @Test
    fun codesAndTagsResolveToShippedLanguages() {
        assertEquals(AppLanguage.Zh, AppLanguage.fromTag("zh-CN"))
        assertEquals(AppLanguage.De, AppLanguage.fromTag("de-DE"))
        assertEquals(AppLanguage.System, AppLanguage.fromTag("fr"))
        assertEquals(AppLanguage.System, AppLanguage.fromTag(null))
        assertEquals(AppLanguage.Es, AppLanguage.fromCode("es"))
        assertEquals(AppLanguage.System, AppLanguage.fromCode("nope"))
        assertTrue(AppLanguage.Zh.isCjk && !AppLanguage.Ru.isCjk)
    }
}
