// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import android.icu.text.RelativeDateTimeFormatter
import android.icu.text.RelativeDateTimeFormatter.AbsoluteUnit
import android.icu.text.RelativeDateTimeFormatter.Direction
import android.icu.text.RelativeDateTimeFormatter.RelativeUnit
import android.icu.util.ULocale
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalConfiguration

enum class RelUnit { Minute, Hour, Day }

/** A moment as "N units from now", negative in the past. */
data class RelativeSpan(val amount: Long, val unit: RelUnit)

/** Picks the unit as the desktop does (lib relDate): minutes under an hour, hours under a day, else days. */
object RelativeTime {
    fun span(targetMs: Long, nowMs: Long): RelativeSpan {
        val diff = targetMs - nowMs
        val abs = kotlin.math.abs(diff)
        return when {
            abs < SubscriptionLogic.HOUR_MS -> RelativeSpan(roundHalfUp(diff / SubscriptionLogic.MINUTE_MS.toDouble()), RelUnit.Minute)
            abs < SubscriptionLogic.DAY_MS -> RelativeSpan(roundHalfUp(diff / SubscriptionLogic.HOUR_MS.toDouble()), RelUnit.Hour)
            else -> RelativeSpan(roundHalfUp(diff / SubscriptionLogic.DAY_MS.toDouble()), RelUnit.Day)
        }
    }

    /** JavaScript Math.round: halves go towards positive infinity, so "-0.5 minutes" reads as "this minute". */
    private fun roundHalfUp(x: Double): Long = kotlin.math.floor(x + 0.5).toLong()
}

/**
 * Formats a moment relative to now in the app language ("in 2 days", "yesterday"). Only the android.icu calls of
 * API 24 are used: the offset based overloads need API 28 and the app supports API 26.
 */
@Composable
fun rememberRelativeTime(): (Long) -> String {
    val config = LocalConfiguration.current
    return remember(config) {
        val formatter = RelativeDateTimeFormatter.getInstance(ULocale.forLocale(config.locales[0]))
        val format: (Long) -> String = { ms -> formatSpan(formatter, RelativeTime.span(ms, System.currentTimeMillis())) }
        format
    }
}

private fun formatSpan(f: RelativeDateTimeFormatter, s: RelativeSpan): String {
    val future = s.amount > 0
    val direction = if (future) Direction.NEXT else Direction.LAST
    val quantity = kotlin.math.abs(s.amount)
    return when {
        s.amount == 0L && s.unit == RelUnit.Day -> f.format(Direction.THIS, AbsoluteUnit.DAY)
        s.amount == 0L -> f.format(Direction.THIS, AbsoluteUnit.NOW)
        s.unit == RelUnit.Day && quantity == 1L -> f.format(direction, AbsoluteUnit.DAY)
        else -> f.format(quantity.toDouble(), direction, relativeUnit(s.unit))
    }
}

private fun relativeUnit(u: RelUnit): RelativeUnit = when (u) {
    RelUnit.Minute -> RelativeUnit.MINUTES
    RelUnit.Hour -> RelativeUnit.HOURS
    RelUnit.Day -> RelativeUnit.DAYS
}
