// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind

/** The resolution modes of the screen. The core picks servers and the route of the queries from the id. */
enum class DnsCard(val id: String, @StringRes val title: Int, @StringRes val desc: Int) {
    Auto("auto", R.string.dns_auto, R.string.dns_auto_d),
    Tunnel("tunnel", R.string.dns_tunnel, R.string.dns_tunnel_d),
    Doh("doh", R.string.dns_doh, R.string.dns_doh_d),
    Dot("dot", R.string.dns_dot, R.string.dns_dot_d),
    Local("local", R.string.dns_local, R.string.dns_local_d),
}

/** The settings the screen sends, built as the desktop builds them (screen_dns.tsx). */
object DnsRequests {
    const val CUSTOM = "custom"
    const val ADVANCED = "advanced"

    /** Id of the card to mark: "advanced" in JSON mode, otherwise the choice the core stored (auto by default). */
    fun activeCard(dns: DnsSettings?): String = when {
        dns == null -> ""
        dns.mode == DnsSettings.Mode.MODE_ADVANCED -> ADVANCED
        else -> dns.choice ?: DnsCard.Auto.id
    }

    fun card(card: DnsCard): DnsSettings = DnsSettings(mode = DnsSettings.Mode.MODE_SIMPLE, choice = card.id)

    /** Own server address; the direct DNS and DNS routing of the current settings are kept. */
    fun custom(current: DnsSettings?, remote: String): DnsSettings = DnsSettings(
        mode = DnsSettings.Mode.MODE_SIMPLE,
        choice = CUSTOM,
        remoteDns = remote.trim(),
        directDns = current?.directDns,
        routingEnabled = current?.routingEnabled ?: false,
    )

    fun advanced(current: DnsSettings?, json: String): DnsSettings = DnsSettings(
        mode = DnsSettings.Mode.MODE_ADVANCED,
        advancedJson = json,
        routingEnabled = current?.routingEnabled ?: false,
    )
}

@Immutable
data class DnsState(
    val dns: DnsSettings? = null,
    val loading: Boolean = true,
    val error: ErrorKind? = null,
    /** The JSON editor is open (it is not the same as the saved mode until saved). */
    val advanced: Boolean = false,
    val json: String = "",
    val remote: String = "",
    val busy: Boolean = false,
    /** Messages from the core's check of the JSON, each "path: reason". Empty when it passed or was not run. */
    val problems: List<String> = emptyList(),
    /** The core rejected the JSON (possibly without a message): the editor shows the notice. */
    val invalid: Boolean = false,
) {
    val activeCard: String get() = DnsRequests.activeCard(dns)
    val fakeDns: Boolean get() = dns?.fakeDnsEnabled == true
}

sealed interface DnsAction {
    data class Loaded(val dns: DnsSettings) : DnsAction
    data class Failed(val kind: ErrorKind) : DnsAction
    data class Applied(val dns: DnsSettings) : DnsAction
    data class Advanced(val on: Boolean) : DnsAction
    data class Json(val text: String) : DnsAction
    data class Remote(val text: String) : DnsAction
    data class Busy(val on: Boolean) : DnsAction
    data class Problems(val messages: List<String>) : DnsAction
}

object DnsReducer {
    /**
     * The screen reloads on every return to the app. A field the person changed and did not save keeps their text
     * (and the JSON editor stays open); a field they did not touch follows the core.
     */
    private fun reload(s: DnsState, dns: DnsSettings): DnsState {
        val before = s.dns
        val jsonEdited = before != null && s.json != before.advancedJson.orEmpty()
        val remoteEdited = before != null && s.remote != before.remoteDns.orEmpty()
        return s.copy(
            dns = dns, loading = false, error = null,
            advanced = if (jsonEdited) s.advanced else dns.mode == DnsSettings.Mode.MODE_ADVANCED,
            json = if (jsonEdited) s.json else dns.advancedJson.orEmpty(),
            remote = if (remoteEdited) s.remote else dns.remoteDns.orEmpty(),
            problems = if (jsonEdited) s.problems else emptyList(),
            invalid = if (jsonEdited) s.invalid else false,
        )
    }

    fun reduce(s: DnsState, a: DnsAction): DnsState = when (a) {
        is DnsAction.Loaded -> reload(s, a.dns)
        is DnsAction.Failed -> s.copy(loading = false, error = a.kind)
        // After a save the core's normalized value replaces ours; the text the person is typing is kept.
        is DnsAction.Applied -> s.copy(dns = a.dns, remote = a.dns.remoteDns ?: s.remote, problems = emptyList(), invalid = false)
        is DnsAction.Advanced -> s.copy(advanced = a.on, problems = emptyList(), invalid = false)
        is DnsAction.Json -> s.copy(json = a.text, problems = emptyList(), invalid = false)
        is DnsAction.Remote -> s.copy(remote = a.text)
        is DnsAction.Busy -> s.copy(busy = a.on)
        is DnsAction.Problems -> s.copy(problems = a.messages.filter { it.isNotBlank() }, invalid = true)
    }
}
