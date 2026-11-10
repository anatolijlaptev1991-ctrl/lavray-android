// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RunSelfCheckRequest.CheckKind
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SelfCheckResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ChecksMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.CheckSummary
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.DetailText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.FixAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LeakVerdict
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.RouteMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.RowState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ChecksTests {
    private fun result(kind: CheckKind, status: SelfCheckResult.Status, detail: String? = null, fix: String = "") =
        SelfCheckResult(kind, status, fix, detail)

    @Test fun portCheckIsNotShownOnThePhone() {
        val rows = ChecksMapper.rows(
            listOf(
                result(CheckKind.CHECK_KIND_CORE_ALIVE, SelfCheckResult.Status.STATUS_OK, "0.9.3"),
                result(CheckKind.CHECK_KIND_PORT_FREE, SelfCheckResult.Status.STATUS_UNSPECIFIED, "not_applicable"),
            ),
        )
        assertEquals(listOf(CheckKind.CHECK_KIND_CORE_ALIVE), rows.map { it.kind })
    }

    @Test fun unspecifiedStatusIsNotChecked_neverOk() {
        assertEquals(RowState.Skipped, ChecksMapper.stateOf(SelfCheckResult.Status.STATUS_UNSPECIFIED))
        assertEquals(RowState.Ok, ChecksMapper.stateOf(SelfCheckResult.Status.STATUS_OK))
        assertEquals(RowState.Warn, ChecksMapper.stateOf(SelfCheckResult.Status.STATUS_WARN))
        assertEquals(RowState.Fail, ChecksMapper.stateOf(SelfCheckResult.Status.STATUS_FAIL))
    }

    @Test fun reasonCodesAreTranslated() {
        val r = result(CheckKind.CHECK_KIND_DNS_RESOLVES, SelfCheckResult.Status.STATUS_UNSPECIFIED, "no_connection")
        assertEquals(DetailText.Res(R.string.chk_d_no_connection), ChecksMapper.detailOf(r, RowState.Skipped))
    }

    @Test fun ipv6NotApplicableGetsThePhoneWording() {
        val r = result(CheckKind.CHECK_KIND_IPV6, SelfCheckResult.Status.STATUS_UNSPECIFIED, "not_applicable")
        assertEquals(DetailText.Res(R.string.chk_d_v6_unmeasured), ChecksMapper.detailOf(r, RowState.Skipped))
        val other = result(CheckKind.CHECK_KIND_SERVER_REACHABLE, SelfCheckResult.Status.STATUS_UNSPECIFIED, "not_applicable")
        assertEquals(DetailText.Res(R.string.chk_d_not_applicable), ChecksMapper.detailOf(other, RowState.Skipped))
    }

    @Test fun aRawErrorStringOfAFailedCheckIsNeverShown() {
        val r = result(CheckKind.CHECK_KIND_PROXY_ACTIVE, SelfCheckResult.Status.STATUS_FAIL, "Head \"https://1.1.1.1/\": context deadline exceeded")
        assertEquals(DetailText.Res(R.string.chk_d_generic), ChecksMapper.detailOf(r, RowState.Fail))
    }

    @Test fun measuredValuesAreShownAsTheyAre() {
        assertEquals(DetailText.Raw("123 ms"), ChecksMapper.detailOf(result(CheckKind.CHECK_KIND_SERVER_REACHABLE, SelfCheckResult.Status.STATUS_OK, "123 ms"), RowState.Ok))
        assertEquals(DetailText.Raw("+12.3 s"), ChecksMapper.detailOf(result(CheckKind.CHECK_KIND_CLOCK_SKEW, SelfCheckResult.Status.STATUS_WARN, "+12.3 s"), RowState.Warn))
        assertEquals(DetailText.Raw("Provider · 2026-11-30"), ChecksMapper.detailOf(result(CheckKind.CHECK_KIND_SUBSCRIPTION_OK, SelfCheckResult.Status.STATUS_WARN, "Provider · 2026-11-30"), RowState.Warn))
        assertTrue(ChecksMapper.measured("0.9.3"))
        assertFalse(ChecksMapper.measured("connection refused"))
    }

    @Test fun summaryNeverSaysAllGoodWhileSomethingWasSkipped() {
        fun rows(vararg s: SelfCheckResult.Status) = ChecksMapper.rows(
            s.mapIndexed { i, st -> result(CheckKind.entries[i + 1], st) },
        )
        assertEquals(CheckSummary.AllGood, ChecksMapper.summary(rows(SelfCheckResult.Status.STATUS_OK, SelfCheckResult.Status.STATUS_OK)))
        assertEquals(CheckSummary.Partial(1, 2), ChecksMapper.summary(rows(SelfCheckResult.Status.STATUS_OK, SelfCheckResult.Status.STATUS_UNSPECIFIED)))
        assertEquals(CheckSummary.Problems(2), ChecksMapper.summary(rows(SelfCheckResult.Status.STATUS_FAIL, SelfCheckResult.Status.STATUS_WARN, SelfCheckResult.Status.STATUS_OK)))
        assertNull(ChecksMapper.summary(ChecksMapper.pending()))
        assertNull(ChecksMapper.summary(emptyList()))
    }

    @Test fun fixCodesMapToScreensAndPortFixIsDropped() {
        assertEquals(FixAction.OpenDns, ChecksMapper.fixOf("open_dns"))
        assertEquals(FixAction.OpenSubscriptions, ChecksMapper.fixOf("open_subscription"))
        assertEquals(FixAction.SyncClock, ChecksMapper.fixOf("sync_clock"))
        assertNull(ChecksMapper.fixOf("change_port"))
        assertNull(ChecksMapper.fixOf(""))
    }

    @Test fun leakVerdictFlagsEqualAddressesOnlyWithALiveTunnel() {
        val same = LeakVerdict.of(LeakReport(tunnelIp = "203.0.113.7", directIp = "203.0.113.7", tunnelActive = true, unchecked = listOf("dns", "ipv6")))
        assertTrue(same.sameAddress)
        assertTrue(same.dnsUnchecked)
        assertTrue(same.ipv6Unchecked)
        val down = LeakVerdict.of(LeakReport(directIp = "203.0.113.7", tunnelActive = false, unchecked = listOf("tunnel_ip")))
        assertFalse(down.sameAddress)
        assertTrue(down.tunnelUnchecked)
        val ok = LeakVerdict.of(LeakReport(tunnelIp = "198.51.100.1", directIp = "203.0.113.7", tunnelActive = true))
        assertFalse(ok.sameAddress)
        assertFalse(ok.directUnchecked)
    }

    @Test fun routeInputIsTrimmedAndBounded() {
        assertEquals("example.com", RouteMapper.normalize("  example.com \n"))
        assertNull(RouteMapper.normalize("   "))
        assertNull(RouteMapper.normalize("a".repeat(3000)))
        assertEquals(R.string.out_proxy, RouteMapper.outboundName(Outbound.OUTBOUND_PROXY))
        assertEquals(R.string.out_block, RouteMapper.outboundName(Outbound.OUTBOUND_BLOCK))
        assertNull(RouteMapper.outboundName(Outbound.OUTBOUND_UNSPECIFIED))
    }
}

class ChecksTitleTest {
    @Test
    fun theClockCheckIsAboutThePhoneNotAComputer() {
        assertEquals(R.string.chk_time_phone, ChecksMapper.title(CheckKind.CHECK_KIND_CLOCK_SKEW))
    }
}
