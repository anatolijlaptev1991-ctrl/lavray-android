// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RunSelfCheckRequest.CheckKind
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SelfCheckResult

/** Row state of one self-check; mirrors desktop screen_diag.tsx RunState. */
enum class RowState { Pending, Ok, Warn, Fail, Skipped }

/** What a "fix" button does; the screen maps each to navigation or a system settings page. */
enum class FixAction { OpenDns, OpenSubscriptions, SyncClock, GoToConnection }

/** Text under a check title: a dictionary string, the core's own short value (ms, clock skew, version) or nothing. */
sealed interface DetailText {
    data object None : DetailText
    data class Res(@StringRes val id: Int) : DetailText
    data class Raw(val text: String) : DetailText
}

@Immutable
data class CheckRow(val kind: CheckKind, val state: RowState, val detail: DetailText, val fix: FixAction?)

/** Summary line under the check list (desktop chk.sum.*): never claims "no problems" while checks were skipped. */
sealed interface CheckSummary {
    data object AllGood : CheckSummary
    data class Problems(val count: Int) : CheckSummary
    data class Partial(val skipped: Int, val total: Int) : CheckSummary
}

object ChecksMapper {
    /** The core's local-port check has no meaning on the phone (no listener exists), so it is not shown. */
    private val hidden = setOf(CheckKind.CHECK_KIND_PORT_FREE, CheckKind.CHECK_KIND_UNSPECIFIED)

    /** Titles follow the real contract enum (desktop KIND_LABEL). */
    @StringRes
    fun title(kind: CheckKind): Int = when (kind) {
        CheckKind.CHECK_KIND_CORE_ALIVE -> R.string.chk_core_t
        CheckKind.CHECK_KIND_SERVER_REACHABLE -> R.string.chk_server_t
        CheckKind.CHECK_KIND_DNS_RESOLVES -> R.string.chk_dns_t
        CheckKind.CHECK_KIND_PROXY_ACTIVE -> R.string.chk_tunnel_t
        CheckKind.CHECK_KIND_CLOCK_SKEW -> R.string.chk_time_phone
        CheckKind.CHECK_KIND_SUBSCRIPTION_OK -> R.string.chk_sub_t
        CheckKind.CHECK_KIND_IPV6 -> R.string.chk_ipv6_t
        CheckKind.CHECK_KIND_PORT_FREE, CheckKind.CHECK_KIND_UNSPECIFIED -> R.string.chk_port_t
    }

    fun rows(results: List<SelfCheckResult>): List<CheckRow> =
        results.filter { it.kind !in hidden }.map { r ->
            val state = stateOf(r.status)
            CheckRow(r.kind, state, detailOf(r, state), fixOf(r.fixCode))
        }

    /** The rows shown before a run has an answer. */
    fun pending(): List<CheckRow> = ORDER.map { CheckRow(it, RowState.Pending, DetailText.None, null) }

    fun summary(rows: List<CheckRow>): CheckSummary? {
        if (rows.isEmpty() || rows.any { it.state == RowState.Pending }) return null
        val bad = rows.count { it.state == RowState.Fail || it.state == RowState.Warn }
        val skipped = rows.count { it.state == RowState.Skipped }
        return when {
            bad > 0 -> CheckSummary.Problems(bad)
            skipped > 0 -> CheckSummary.Partial(skipped, rows.size)
            else -> CheckSummary.AllGood
        }
    }

    internal fun stateOf(status: SelfCheckResult.Status): RowState = when (status) {
        SelfCheckResult.Status.STATUS_OK -> RowState.Ok
        SelfCheckResult.Status.STATUS_WARN -> RowState.Warn
        SelfCheckResult.Status.STATUS_FAIL -> RowState.Fail
        // UNSPECIFIED is "not checked" by the core's contract and must never read as OK.
        SelfCheckResult.Status.STATUS_UNSPECIFIED -> RowState.Skipped
    }

    /**
     * Codes the core puts in `detail` are translated; short measured values are shown as they are. Any other
     * text of a failed check is a Go error string: it is never shown, the log has it.
     */
    internal fun detailOf(r: SelfCheckResult, state: RowState): DetailText {
        val text = r.detail?.trim().orEmpty()
        if (text.isEmpty()) return DetailText.None
        codeText(r.kind, text)?.let { return DetailText.Res(it) }
        return when (state) {
            RowState.Ok -> DetailText.Raw(text)
            RowState.Skipped, RowState.Pending -> DetailText.None
            RowState.Warn, RowState.Fail ->
                if (measured(text) || r.kind == CheckKind.CHECK_KIND_SUBSCRIPTION_OK) DetailText.Raw(text) else DetailText.Res(R.string.chk_d_generic)
        }
    }

    @StringRes
    private fun codeText(kind: CheckKind, code: String): Int? = when (code) {
        "no_connection" -> R.string.chk_d_no_connection
        "blocked" -> R.string.chk_d_blocked
        "no_subscription" -> R.string.chk_d_no_subscription
        "time_unreachable" -> R.string.chk_d_time_unreachable
        "v6_direct" -> R.string.chk_d_v6_direct
        // The core says "not applicable" for IPv6 because the app itself runs outside the tunnel on Android.
        "not_applicable" -> if (kind == CheckKind.CHECK_KIND_IPV6) R.string.chk_d_v6_unmeasured else R.string.chk_d_not_applicable
        else -> null
    }

    private val measuredShape = Regex("""^[+-]?\d+(\.\d+)? (ms|s)$|^v?\d+\.\d+(\.\d+)?([-+][0-9A-Za-z.]+)?$""")

    /** Latency ("123 ms"), clock skew ("+0.3 s") and a version number are the core's own short values. */
    internal fun measured(text: String): Boolean = measuredShape.matches(text)

    internal fun fixOf(code: String): FixAction? = when (code) {
        "open_dns" -> FixAction.OpenDns
        "open_subscriptions", "open_subscription" -> FixAction.OpenSubscriptions
        "fix_clock", "sync_clock" -> FixAction.SyncClock
        "restart_profile" -> FixAction.GoToConnection
        else -> null // change_port has no meaning without a local port
    }

    @StringRes
    fun fixLabel(action: FixAction): Int = when (action) {
        FixAction.OpenDns -> R.string.chk_fix_dns
        FixAction.OpenSubscriptions -> R.string.chk_fix_sub
        FixAction.SyncClock -> R.string.chk_fix_time
        FixAction.GoToConnection -> R.string.err_retry
    }

    private val ORDER = listOf(
        CheckKind.CHECK_KIND_CORE_ALIVE, CheckKind.CHECK_KIND_SERVER_REACHABLE, CheckKind.CHECK_KIND_DNS_RESOLVES,
        CheckKind.CHECK_KIND_PROXY_ACTIVE, CheckKind.CHECK_KIND_CLOCK_SKEW, CheckKind.CHECK_KIND_SUBSCRIPTION_OK,
        CheckKind.CHECK_KIND_IPV6,
    )
}

/** What the leak card says; derived from the core's report, with every unmeasured value named as such. */
@Immutable
data class LeakVerdict(
    val tunnelActive: Boolean,
    val directIp: String?,
    val tunnelIp: String?,
    val sameAddress: Boolean,
    val directUnchecked: Boolean,
    val tunnelUnchecked: Boolean,
    val ipv6Unchecked: Boolean,
    val dnsUnchecked: Boolean,
    val ipv6Direct: Boolean,
) {
    companion object {
        fun of(report: LeakReport): LeakVerdict {
            val direct = report.directIp
            val tunnel = report.tunnelIp
            return LeakVerdict(
                tunnelActive = report.tunnelActive,
                directIp = direct,
                tunnelIp = tunnel,
                // Equal addresses with a live tunnel mean the traffic of other apps may leave around it.
                sameAddress = report.tunnelActive && direct != null && tunnel != null && direct == tunnel,
                directUnchecked = "direct_ip" in report.unchecked,
                tunnelUnchecked = "tunnel_ip" in report.unchecked,
                ipv6Unchecked = "ipv6" in report.unchecked,
                dnsUnchecked = "dns" in report.unchecked,
                ipv6Direct = report.ipv6DirectAvailable,
            )
        }
    }
}

/** Where a probed address goes: the outbound name and the rule the core matched (null: the default applies). */
@Immutable
data class RouteAnswer(val address: String, @StringRes val outbound: Int?, val rule: String?)

object RouteMapper {
    @StringRes
    fun outboundName(o: Outbound): Int? = when (o) {
        Outbound.OUTBOUND_PROXY -> R.string.out_proxy
        Outbound.OUTBOUND_DIRECT -> R.string.out_direct
        Outbound.OUTBOUND_BLOCK -> R.string.out_block
        Outbound.OUTBOUND_UNSPECIFIED -> null
    }

    /** Input as typed: the core accepts a domain, an IP, host:port or a URL. Blank or oversized means nothing to check. */
    fun normalize(input: String): String? = input.trim().takeIf { it.isNotEmpty() && it.length <= MAX_ADDRESS }

    private const val MAX_ADDRESS = 2048
}
