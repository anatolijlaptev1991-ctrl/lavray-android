// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import androidx.compose.runtime.Immutable

/**
 * What a piece of pasted, scanned or shared text is, decided locally and cheaply before the core is asked.
 * The core (ImportContent with intent AUTO) has the last word; this only drives the preview, the button state
 * and the decision to accept a deep link or a share intent at all. Schemes mirror core/internal/links/parse.go.
 */
@Immutable
sealed interface ImportInput {
    data object Empty : ImportInput

    /**
     * One or more share links of known schemes; [skipped] lines had an unknown scheme. [issues] are links of a known
     * scheme that the core would refuse, so [readable] is what the import can really bring in.
     */
    data class Links(val links: List<String>, val schemes: List<String>, val skipped: Int, val issues: List<LinkIssue> = emptyList()) : ImportInput {
        val readable: Int get() = links.size - issues.size
    }

    /** A single http(s) address: a subscription. [insecure] is plain http (the provider and the network see it). */
    data class SubscriptionUrl(val url: String, val insecure: Boolean) : ImportInput

    /** An encrypted subscription link; the core decrypts it locally. */
    data class EncryptedLink(val link: String) : ImportInput

    /** A whole document: JSON config, YAML profile list or a base64 list of links. */
    data class Document(val kind: DocKind, val length: Int) : ImportInput

    data object Unrecognized : ImportInput
}

enum class DocKind { Json, Yaml, Base64 }

object LinkClassifier {
    /** Schemes the core parses as a profile link (parse.go parseScheme), lower case. */
    val shareSchemes: Set<String> = setOf(
        "ss", "vmess", "vless", "trojan", "hy2", "hysteria2", "tuic",
        "socks", "socks4", "socks4a", "socks5", "nekoray",
    )

    /** Schemes that may open the app by a deep link. http and https are deliberately absent: they would hijack every web link. */
    val deepLinkSchemes: List<String> = shareSchemes.toList() + ENCRYPTED_SCHEME

    /** The core refuses a request above 16 MiB; the same bound applies to text we are willing to hold. */
    const val MAX_TEXT_CHARS = 16 * 1024 * 1024

    private const val ENCRYPTED_SCHEME = "happ"
    private val schemeOf = Regex("^([A-Za-z][A-Za-z0-9+.-]*)://\\S+$")
    private val base64Body = Regex("^[A-Za-z0-9+/=_\\-\\s]{16,}$")

    fun classify(raw: String?): ImportInput {
        val text = raw?.trim().orEmpty()
        if (text.isEmpty()) return ImportInput.Empty
        if (text.length > MAX_TEXT_CHARS) return ImportInput.Unrecognized
        val numbered = text.lineSequence().mapIndexed { i, l -> (i + 1) to l.trim() }.filter { it.second.isNotEmpty() }.toList()
        val lines = numbered.map { it.second }
        if (lines.all { schemeOf.matches(it) }) return classifyUrls(lines, numbered.map { it.first })
        return classifyDocument(text)
    }

    private fun classifyUrls(lines: List<String>, lineNumbers: List<Int>): ImportInput {
        val schemes = lines.map { schemeOf.matchEntire(it)!!.groupValues[1].lowercase() }
        if (lines.size == 1) {
            val s = schemes[0]
            if (s == "http" || s == "https") return ImportInput.SubscriptionUrl(lines[0], insecure = s == "http")
            if (s == ENCRYPTED_SCHEME) return ImportInput.EncryptedLink(lines[0])
        }
        val known = lines.indices.filter { schemes[it] in shareSchemes }
        if (known.isEmpty()) return ImportInput.Unrecognized
        return ImportInput.Links(
            links = known.map { lines[it] },
            schemes = known.map { schemes[it] }.distinct(),
            skipped = lines.size - known.size,
            issues = known.mapNotNull { i ->
                if (schemes[i] != "ss") null else SsLinkCheck.check(lines[i])?.let { LinkIssue(lineNumbers[i], it, mask(lines[i])) }
            },
        )
    }

    private fun classifyDocument(text: String): ImportInput {
        val head = text.trimStart()
        return when {
            head.startsWith("{") || head.startsWith("[") -> ImportInput.Document(DocKind.Json, text.length)
            YAML_MARKERS.any { head.contains(it) } -> ImportInput.Document(DocKind.Yaml, text.length)
            base64Body.matches(text) -> ImportInput.Document(DocKind.Base64, text.length)
            else -> ImportInput.Unrecognized
        }
    }

    private val YAML_MARKERS = listOf("proxies:", "outbounds:", "proxy-groups:")

    /** True when a deep link or shared text is worth opening the import sheet for. */
    fun isImportable(raw: String?): Boolean = classify(raw).let { it != ImportInput.Empty && it != ImportInput.Unrecognized }

    /**
     * A link made safe to show on screen: the scheme and the ends of the address, never the keys
     * in the middle (links carry passwords and UUIDs).
     */
    fun mask(link: String): String {
        val t = link.trim()
        val i = t.indexOf("://")
        if (i < 0 || t.length <= i + 3 + 12) return t.take(MASK_LIMIT)
        val rest = t.substring(i + 3)
        return t.substring(0, i + 3) + rest.take(6) + "…" + rest.takeLast(6)
    }

    private const val MASK_LIMIT = 40
}
