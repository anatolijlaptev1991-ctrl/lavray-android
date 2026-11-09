// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n

import java.text.NumberFormat
import java.util.Locale

/** Unit labels from the dictionary (keys ub.* and u.*): units are never produced by the platform formatter. */
class UnitLabels(
    val byte: String,
    val kb: String,
    val mb: String,
    val gb: String,
    val bytePerSec: String,
    val kbPerSec: String,
    val mbPerSec: String,
    val gbPerSec: String,
    val ms: String,
)

/**
 * Volume, speed, latency and clock formatting as on the desktop (lib/i18n.ts): decimal scale 1000,
 * one decimal above bytes, number and unit separated by U+00A0, digits by the locale NumberFormat.
 */
class UnitFormat(private val units: UnitLabels, locale: Locale) {
    private val one = NumberFormat.getNumberInstance(locale).apply { maximumFractionDigits = 1; minimumFractionDigits = 0 }
    private val whole = NumberFormat.getNumberInstance(locale).apply { maximumFractionDigits = 0 }
    private val pad2 = NumberFormat.getIntegerInstance(locale).apply { minimumIntegerDigits = 2; isGroupingUsed = false }

    fun bytes(value: Long): String = when {
        value >= GB -> one.format(value / GB.toDouble()) + NB + units.gb
        value >= MB -> one.format(value / MB.toDouble()) + NB + units.mb
        value >= KB -> one.format(value / KB.toDouble()) + NB + units.kb
        else -> whole.format(value.coerceAtLeast(0)) + NB + units.byte
    }

    fun speed(bytesPerSecond: Double): String = when {
        !(bytesPerSecond >= 1.0) -> whole.format(0) + NB + units.bytePerSec
        bytesPerSecond >= GB -> one.format(bytesPerSecond / GB) + NB + units.gbPerSec
        bytesPerSecond >= MB -> one.format(bytesPerSecond / MB) + NB + units.mbPerSec
        bytesPerSecond >= KB -> one.format(bytesPerSecond / KB) + NB + units.kbPerSec
        else -> whole.format(bytesPerSecond) + NB + units.bytePerSec
    }

    fun latency(ms: Int): String = whole.format(ms) + NB + units.ms

    /** m:ss, or h:mm:ss from an hour on (desktop connDur). */
    fun clock(seconds: Long): String {
        val total = seconds.coerceAtLeast(0)
        val h = total / 3600
        val m = (total % 3600) / 60
        val s = total % 60
        val tail = pad2.format(m) + ":" + pad2.format(s)
        return if (h > 0) whole.format(h) + ":" + tail else tail
    }

    private companion object {
        const val KB = 1_000L
        const val MB = 1_000_000L
        const val GB = 1_000_000_000L
        const val NB = " "
    }
}
