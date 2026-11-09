// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import java.util.Base64

/**
 * The reason the core attaches to an error as `google.rpc.ErrorInfo { reason, domain }` (desktop lib/errorInfo.ts). The
 * detail travels as base64 of the protobuf message and there is no generated schema for it, so the two string fields
 * are read from the wire format directly. Anything malformed gives no reason, never an exception.
 */
internal object ErrorInfoReader {
    private const val TYPE = "google.rpc.ErrorInfo"
    private const val DOMAIN = "lavray.core"

    fun reason(details: List<ErrorDetailBody>): String? {
        for (d in details) {
            if (d.type != TYPE) continue
            val bytes = try {
                Base64.getDecoder().decode(d.value)
            } catch (_: IllegalArgumentException) {
                continue
            }
            val info = decode(bytes)
            if (info.domain == DOMAIN && info.reason.isNotEmpty()) return info.reason
        }
        return null
    }

    internal data class Info(val reason: String = "", val domain: String = "")

    /** Fields reason (1) and domain (2) of ErrorInfo; every other field is skipped, a damaged tail stops the read. */
    internal fun decode(buf: ByteArray): Info {
        var reason = ""
        var domain = ""
        var at = 0
        while (at < buf.size) {
            val tag = varint(buf, at) ?: break
            val field = (tag.first / 8).toInt()
            if (tag.first % 8 != 2L) break // every ErrorInfo field is a string or a map: length and bytes
            val len = varint(buf, tag.second) ?: break
            val start = len.second
            val size = len.first
            if (size < 0 || start + size > buf.size) break
            val text = String(buf, start, size.toInt(), Charsets.UTF_8)
            if (field == 1) reason = text
            if (field == 2) domain = text
            at = start + size.toInt()
        }
        return Info(reason, domain)
    }

    /** Value and the offset after it, or null for a varint that is cut off or longer than five bytes. */
    private fun varint(buf: ByteArray, at: Int): Pair<Long, Int>? {
        var value = 0L
        var shift = 0
        for (i in 0 until MAX_VARINT_BYTES) {
            if (at + i >= buf.size) return null
            val b = buf[at + i].toInt() and 0xff
            value += (b and 0x7f).toLong() shl shift
            if (b and 0x80 == 0) return value to (at + i + 1)
            shift += 7
        }
        return null
    }

    private const val MAX_VARINT_BYTES = 5
}
