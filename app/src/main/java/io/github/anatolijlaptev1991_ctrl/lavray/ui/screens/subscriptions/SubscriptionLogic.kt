// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress

/** The wording of a failed update; the dictionary text for each is picked by the screen. */
enum class SubErrorKind { Auth, NotFound, Timeout, Zero, Generic }

/** Last failed update of one subscription: a short code (an HTTP status, "timeout", "zero", "error") and its kind. */
@Immutable
data class SubFailure(val kind: SubErrorKind, val code: String) {
    /** The code is worth showing next to the text only when it says more than "something failed". */
    val showCode: Boolean get() = code != GENERIC_CODE

    companion object {
        const val GENERIC_CODE = "error"
        fun fromCode(code: String): SubFailure = SubFailure(SubscriptionLogic.kindOf(code), code)
    }
}

/** Where the core is in an update; only the phases a person can follow are kept. */
enum class UpdatePhase { Requesting, Decoding, Parsing, Diffing, Saving }

/** Totals of an "update all" run, counted from the final frame of each subscription. */
@Immutable
data class UpdateTally(val ok: Int = 0, val skip: Int = 0, val fail: Int = 0, val confirm: Int = 0)

/**
 * Pure decisions of the subscriptions screen, ported from the desktop (screen_subscriptions.tsx,
 * the helper files next to it) so that the phone words an error, schedules the next update and counts a run the same way.
 */
object SubscriptionLogic {
    /** Intervals offered in the settings sheet, in hours (desktop HOURS). */
    val HOURS: List<Int> = listOf(1, 3, 6, 12, 24)
    const val DEFAULT_HOURS = 6
    const val UA_MAX = 256

    private val httpStatus = Regex("""HTTP\s+(\d{3})""", RegexOption.IGNORE_CASE)
    private val leadingStatus = Regex("""^(\d{3})\b""")
    private val zeroServers = Regex("0 (профил|сервер|профилей|profile|server)|zero", RegexOption.IGNORE_CASE)
    private val timeout = Regex("timeout|deadline|не ответил|timed out", RegexOption.IGNORE_CASE)

    /**
     * Failure text from the core, reduced to a short code. The text itself is never stored or shown: a
     * provider message can carry the link with its token.
     */
    fun errorCode(raw: String): String {
        val s = raw.trim()
        val status = httpStatus.find(s)?.groupValues?.get(1) ?: leadingStatus.find(s)?.groupValues?.get(1)
        return when {
            status != null -> status
            Regex("unauthor", RegexOption.IGNORE_CASE).containsMatchIn(s) -> "401"
            Regex("forbidden", RegexOption.IGNORE_CASE).containsMatchIn(s) -> "403"
            Regex("not found", RegexOption.IGNORE_CASE).containsMatchIn(s) -> "404"
            timeout.containsMatchIn(s) -> "timeout"
            zeroServers.containsMatchIn(s) -> "zero"
            else -> SubFailure.GENERIC_CODE
        }
    }

    fun kindOf(code: String): SubErrorKind = when (code) {
        "401", "403" -> SubErrorKind.Auth
        "404" -> SubErrorKind.NotFound
        "timeout" -> SubErrorKind.Timeout
        "zero" -> SubErrorKind.Zero
        else -> SubErrorKind.Generic
    }

    fun failureOf(raw: String): SubFailure = SubFailure.fromCode(errorCode(raw))

    /**
     * When the subscription updates next: last update plus the interval (its own, otherwise the one the
     * provider recommends). Null means no automatic update.
     */
    fun nextUpdateMs(skipAutoUpdate: Boolean, lastUpdateMs: Long?, ownIntervalMin: Int?, providerHours: Int?): Long? {
        if (skipAutoUpdate || lastUpdateMs == null) return null
        val own = ownIntervalMin ?: 0
        val minutes = if (own > 0) own else (providerHours ?: 0) * 60
        return if (minutes > 0) lastUpdateMs + minutes * MINUTE_MS else null
    }

    /** The preset interval nearest to a stored value; 6 hours when nothing is stored. */
    fun nearestHours(minutes: Int): Int =
        if (minutes <= 0) DEFAULT_HOURS else HOURS.minByOrNull { kotlin.math.abs(it * 60 - minutes) } ?: DEFAULT_HOURS

    /** Expires within three days counts as "soon" (desktop SubscriptionCard). */
    fun expiresSoon(expireMs: Long, nowMs: Long): Boolean = expireMs > 0 && expireMs - nowMs < 3 * DAY_MS

    /** Share of the quota used, 0 when there is no limit. */
    fun usedPercent(used: Long, limit: Long): Int =
        if (limit > 0) kotlin.math.min(100.0, kotlin.math.round(used * 100.0 / limit)).toInt() else 0

    /**
     * The address as shown on a card: host and path, with credentials and the query cut off. Subscription
     * links carry a token there, and a phone screen is easy to photograph or share. Copy gives the full link.
     */
    fun displayUrl(url: String): String {
        val trimmed = url.trim()
        val withoutQuery = trimmed.substringBefore('?').substringBefore('#')
        val scheme = withoutQuery.substringBefore("://", "")
        val rest = if (scheme.isEmpty()) withoutQuery else withoutQuery.substringAfter("://")
        val hostAndPath = rest.substringAfterLast('@')
        val path = hostAndPath.substringAfter('/', "")
        val host = hostAndPath.substringBefore('/')
        val shortPath = when {
            path.isEmpty() -> ""
            path.length <= MAX_PATH -> "/$path"
            else -> "/" + path.take(MAX_PATH) + "…"
        }
        val cut = trimmed.length != withoutQuery.length
        return host + shortPath + if (cut && shortPath.isEmpty()) "/…" else if (cut) "?…" else ""
    }

    /** Only http and https links are opened in a browser. */
    fun isWebLink(url: String): Boolean {
        val s = url.trim().lowercase()
        return s.startsWith("http://") || s.startsWith("https://")
    }

    /** Default name of a new subscription: the host of its link, empty when the link is not a URL. */
    fun hostOf(url: String): String {
        val s = url.trim()
        if (!isWebLink(s)) return ""
        return s.substringAfter("://").substringBefore('/').substringBefore('?').substringBefore('#').substringAfterLast('@').substringBefore(':')
    }

    fun tally(t: UpdateTally, phase: SubscriptionProgress.Phase): UpdateTally = when (phase) {
        SubscriptionProgress.Phase.PHASE_DONE -> t.copy(ok = t.ok + 1)
        SubscriptionProgress.Phase.PHASE_SKIPPED -> t.copy(skip = t.skip + 1)
        SubscriptionProgress.Phase.PHASE_FAILED -> t.copy(fail = t.fail + 1)
        SubscriptionProgress.Phase.PHASE_CONFIRM_REQUIRED -> t.copy(confirm = t.confirm + 1)
        else -> t
    }

    fun phaseOf(p: SubscriptionProgress.Phase): UpdatePhase? = when (p) {
        SubscriptionProgress.Phase.PHASE_REQUESTING -> UpdatePhase.Requesting
        SubscriptionProgress.Phase.PHASE_DECRYPTING_HAPP -> UpdatePhase.Decoding
        SubscriptionProgress.Phase.PHASE_PARSING -> UpdatePhase.Parsing
        SubscriptionProgress.Phase.PHASE_DIFFING -> UpdatePhase.Diffing
        SubscriptionProgress.Phase.PHASE_SAVING -> UpdatePhase.Saving
        else -> null
    }

    const val MINUTE_MS = 60_000L
    const val HOUR_MS = 3_600_000L
    const val DAY_MS = 86_400_000L
    private const val MAX_PATH = 18
}
