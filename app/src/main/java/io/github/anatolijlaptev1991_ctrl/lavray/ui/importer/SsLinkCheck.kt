// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import java.nio.charset.StandardCharsets
import java.util.Base64

/** Why a link line would not be accepted. Each reason has its own sentence in the import sheet. */
enum class LinkProblem {
    /** The key part is not base64 the core accepts: most often the closing "=" signs were cut off. */
    SsBase64,

    /** The key part decodes, but there is no "method:password" in it. */
    SsPair,

    /** The cipher is not one the core knows. */
    SsMethod,
}

/** One refused line: [line] counts from 1 in the pasted text, blank lines included. */
data class LinkIssue(val line: Int, val problem: LinkProblem, val masked: String)

/**
 * The checks the core makes on an `ss://` link, done before the core is asked so that the preview never promises
 * what the core will refuse. Mirrors core/internal/links/parse.go parseSS: the key part is padded base64 (standard or
 * URL alphabet, "=" required), or plain `method:password` in the 2022 form; a v2rayN link is one padded base64 blob.
 */
object SsLinkCheck {
    private val methods = setOf(
        "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305", "none",
        "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
        "aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
        "rc4-md5", "chacha20-ietf", "xchacha20",
    )
    private val stdChars = Regex("^[A-Za-z0-9+/]*={0,2}$")
    private val urlChars = Regex("^[A-Za-z0-9_-]*={0,2}$")

    /** Null when the link is fine as far as can be told here; the core still has the last word. */
    fun check(link: String): LinkProblem? {
        val body = link.trim().substringAfter("://", "")
        val noFragment = body.substringBefore('#')
        if (!noFragment.contains('@')) return checkBlob(noFragment)
        val authority = noFragment.substringBefore('?').substringBefore('/')
        val userInfo = percentDecode(authority.substringBeforeLast('@'))
        val colon = userInfo.indexOf(':')
        // "method:password" in the clear is the 2022 form: no base64 involved.
        if (colon >= 0 && colon < userInfo.length - 1) return checkMethod(userInfo.substring(0, colon))
        val decoded = decodeStrict(userInfo) ?: return LinkProblem.SsBase64
        val pair = decoded.indexOf(':')
        if (pair <= 0 || pair == decoded.length - 1) return LinkProblem.SsPair
        return checkMethod(decoded.substring(0, pair))
    }

    private fun checkBlob(blob: String): LinkProblem? {
        val decoded = decodeStrict(blob) ?: return LinkProblem.SsBase64
        val at = decoded.lastIndexOf('@')
        val pair = decoded.indexOf(':')
        if (at < 0 || pair <= 0 || pair > at) return LinkProblem.SsPair
        return checkMethod(decoded.substring(0, pair))
    }

    private fun checkMethod(method: String): LinkProblem? = if (method in methods) null else LinkProblem.SsMethod

    /** Go's base64.StdEncoding / URLEncoding: the length is a multiple of four and "=" pads the end. Java alone would accept a short tail. */
    private fun decodeStrict(text: String): String? {
        if (text.isEmpty() || text.length % 4 != 0 || text.any { it == '\r' || it == '\n' }) return null
        val bytes = try {
            when {
                stdChars.matches(text) -> Base64.getDecoder().decode(text)
                urlChars.matches(text) -> Base64.getUrlDecoder().decode(text)
                else -> return null
            }
        } catch (_: IllegalArgumentException) {
            return null
        }
        return String(bytes, StandardCharsets.UTF_8)
    }

    private fun percentDecode(text: String): String {
        if (!text.contains('%')) return text
        val out = java.io.ByteArrayOutputStream()
        var i = 0
        while (i < text.length) {
            val ch = text[i]
            val hex = if (ch == '%' && i + 2 < text.length) text.substring(i + 1, i + 3).toIntOrNull(16) else null
            if (hex != null) {
                out.write(hex)
                i += 3
            } else {
                out.write(ch.toString().toByteArray(StandardCharsets.UTF_8))
                i += 1
            }
        }
        return String(out.toByteArray(), StandardCharsets.UTF_8)
    }
}
