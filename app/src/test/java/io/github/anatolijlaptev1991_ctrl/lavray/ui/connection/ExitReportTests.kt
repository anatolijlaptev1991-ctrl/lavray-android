// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.DnsLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ExitReport
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Geo
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.LeakStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.dnsHost
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.dnsLabelOf
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyCell
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.util.Locale

class ExitReportTests {
    private fun report(leak: LeakReport, dns: DnsSettings? = null, latency: LatencyCell? = null) =
        ExitReport.of("1:1", 5L, leak, dns, latency)

    @Test fun differentAddressesWithALiveTunnelAreClear() {
        val r = report(LeakReport(tunnelIp = "198.51.100.9", directIp = "203.0.113.5", tunnelActive = true))
        assertEquals(LeakStatus.Clear, r.leak)
        assertEquals("198.51.100.9", r.exitIp)
    }

    @Test fun sameAddressIsVisible() {
        val r = report(LeakReport(tunnelIp = "203.0.113.5", directIp = "203.0.113.5", tunnelActive = true))
        assertEquals(LeakStatus.Visible, r.leak)
    }

    @Test fun anUnmeasuredAddressIsNeverClear() {
        assertEquals(LeakStatus.Unchecked, report(LeakReport(tunnelIp = "198.51.100.9", tunnelActive = true, unchecked = listOf("direct_ip"))).leak)
        assertEquals(LeakStatus.Unchecked, report(LeakReport(directIp = "203.0.113.5", tunnelActive = true, unchecked = listOf("tunnel_ip"))).leak)
        assertEquals(LeakStatus.Unchecked, report(LeakReport(tunnelIp = "198.51.100.9", directIp = "203.0.113.5", tunnelActive = false)).leak)
    }

    @Test fun anUncheckedTunnelAddressIsNotShownAsTheExit() {
        val r = report(LeakReport(tunnelIp = "", directIp = "203.0.113.5", tunnelActive = true, unchecked = listOf("tunnel_ip")))
        assertNull(r.exitIp)
        assertNull(report(LeakReport(tunnelIp = "  ", tunnelActive = true)).exitIp)
    }

    @Test fun onlyAMeasuredDelayIsKept() {
        assertEquals(87, report(LeakReport(tunnelActive = true), latency = LatencyCell(LatencyStatus.Ok, 87)).latencyMs)
        assertNull(report(LeakReport(tunnelActive = true), latency = LatencyCell(LatencyStatus.Down)).latencyMs)
        assertNull(report(LeakReport(tunnelActive = true), latency = LatencyCell.Unknown).latencyMs)
        assertNull(report(LeakReport(tunnelActive = true), latency = null).latencyMs)
    }

    @Test fun dnsLabelFollowsTheDesktopRules() {
        assertNull(dnsLabelOf(null))
        assertEquals(DnsLabel.Advanced, dnsLabelOf(DnsSettings(mode = DnsSettings.Mode.MODE_ADVANCED)))
        assertEquals(DnsLabel.Card(DnsCard.Tunnel), dnsLabelOf(DnsSettings(choice = "tunnel")))
        assertEquals(DnsLabel.Host("dns.example"), dnsLabelOf(DnsSettings(choice = "custom", remoteDns = "https://dns.example/dns-query")))
        assertEquals(DnsLabel.Card(DnsCard.Auto), dnsLabelOf(DnsSettings()))
    }

    @Test fun dnsHostCutsTheAddress() {
        assertEquals("dns.google", dnsHost("https://dns.google/dns-query"))
        assertEquals("2001:db8::1", dnsHost("tls://[2001:db8::1]:853"))
        assertEquals("1.1.1.1", dnsHost(" 1.1.1.1 "))
    }

    @Test fun countryFlagAndName() {
        assertEquals("🇩🇪", Geo.flag("de"))
        assertNull(Geo.flag(""))
        assertNull(Geo.flag("d1"))
        assertNull(Geo.flag("deu"))
        assertEquals("Germany", Geo.name("de", Locale.ENGLISH))
        assertNull(Geo.name("1x", Locale.ENGLISH))
    }
}
