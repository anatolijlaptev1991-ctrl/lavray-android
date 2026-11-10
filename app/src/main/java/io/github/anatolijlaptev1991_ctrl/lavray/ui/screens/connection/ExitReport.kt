// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LeakVerdict
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyCell
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyStatus
import java.util.Locale

/**
 * What the leak check concluded. [Clear]: the address seen directly differs from the one seen through the tunnel.
 * [Visible]: they are the same, so traffic may leave around the tunnel. [Unchecked]: one of the two was not measured,
 * which is never read as "fine".
 */
enum class LeakStatus { Clear, Visible, Unchecked }

/** The remote DNS of the active route preset as a person would name it: a mode card, a host or "advanced". */
sealed interface DnsLabel {
    data class Card(val card: DnsCard) : DnsLabel
    data class Host(val host: String) : DnsLabel
    data object Advanced : DnsLabel
}

/**
 * Everything the Connection screen says about the live tunnel, taken from one pass of one-shot calls. Each field is
 * null when it was not measured: nothing is guessed or carried over from another connection.
 */
@Immutable
data class ExitReport(
    val key: String,
    val checkedAtMs: Long,
    val exitIp: String?,
    val leak: LeakStatus,
    val dns: DnsLabel?,
    val latencyMs: Int?,
) {
    companion object {
        fun of(key: String, checkedAtMs: Long, report: LeakReport, dns: DnsSettings?, latency: LatencyCell?): ExitReport {
            val verdict = LeakVerdict.of(report)
            return ExitReport(
                key = key,
                checkedAtMs = checkedAtMs,
                exitIp = report.tunnelIp?.trim()?.takeIf { it.isNotEmpty() && !verdict.tunnelUnchecked },
                leak = leakStatusOf(verdict),
                dns = dnsLabelOf(dns),
                latencyMs = latency?.takeIf { it.status == LatencyStatus.Ok }?.ms,
            )
        }
    }
}

fun leakStatusOf(v: LeakVerdict): LeakStatus = when {
    !v.tunnelActive -> LeakStatus.Unchecked
    v.sameAddress -> LeakStatus.Visible
    v.directIp != null && v.tunnelIp != null && !v.directUnchecked && !v.tunnelUnchecked -> LeakStatus.Clear
    else -> LeakStatus.Unchecked
}

private val hostOfAddress = Regex("""^[a-z][a-z0-9+.-]*://(?:\[([^\]]+)]|([^/:?#]+))""", RegexOption.IGNORE_CASE)

/** Host of a DNS address (`https://dns.google/dns-query` gives `dns.google`); anything else is returned as it is. */
fun dnsHost(address: String): String {
    val text = address.trim()
    val m = hostOfAddress.find(text) ?: return text
    return m.groupValues[1].ifEmpty { m.groupValues[2] }
}

/** Desktop dnsLabel.ts: advanced mode, a known card, or the host of the own server; null until the settings are read. */
fun dnsLabelOf(dns: DnsSettings?): DnsLabel? {
    if (dns == null) return null
    if (dns.mode == DnsSettings.Mode.MODE_ADVANCED) return DnsLabel.Advanced
    DnsCard.entries.firstOrNull { it.id == dns.choice }?.let { return DnsLabel.Card(it) }
    val host = dnsHost(dns.remoteDns.orEmpty())
    return if (host.isEmpty()) DnsLabel.Card(DnsCard.Auto) else DnsLabel.Host(host)
}

/** Country names and flags from an ISO 3166 code, as the desktop's geo.ts shows them. */
object Geo {
    /** Regional-indicator flag; null for anything that is not two ASCII letters. */
    fun flag(code: String): String? {
        val cc = code.trim().uppercase(Locale.ROOT)
        if (cc.length != 2 || cc.any { it !in 'A'..'Z' }) return null
        return String(Character.toChars(FLAG_BASE + (cc[0] - 'A'))) + String(Character.toChars(FLAG_BASE + (cc[1] - 'A')))
    }

    /** The name in [locale]; the upper-case code when the platform does not know it; null for a malformed code. */
    fun name(code: String, locale: Locale): String? {
        val cc = code.trim().uppercase(Locale.ROOT)
        if (cc.length != 2 || cc.any { it !in 'A'..'Z' }) return null
        val region = try {
            Locale.Builder().setRegion(cc).build()
        } catch (_: java.util.IllformedLocaleException) {
            return null
        }
        val shown = region.getDisplayCountry(locale)
        return if (shown.isBlank() || shown.equals(cc, ignoreCase = true)) cc else shown
    }

    private const val FLAG_BASE = 0x1F1E6
}
