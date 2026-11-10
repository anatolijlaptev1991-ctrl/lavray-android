// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group

/** User-Agent choices of the settings sheet; the header value of each preset is in [header]. */
enum class UaPreset(val header: String?) {
    Default(""),
    Clash("clash.meta"),
    V2rayN("v2rayN/7.0"),
    Custom(null);

    companion object {
        /** Which choice a stored header corresponds to; anything unknown is shown as a custom value. */
        fun of(stored: String): UaPreset = entries.firstOrNull { it.header == stored } ?: Custom
    }
}

/** Core settings shared by all subscriptions (settings keys), written from the per-subscription sheet. */
object SubSettingKeys {
    const val USE_PROXY = "sub.use_proxy"
    const val HWID = "sub.hwid_enabled"
}

/** Values of the settings sheet with what the person touched; only touched fields are sent. */
@Immutable
data class EditForm(
    val name: String,
    val url: String,
    val hours: Int,
    val intervalTouched: Boolean = false,
    val ua: UaPreset,
    val uaCustom: String,
    val uaTouched: Boolean = false,
    val hwid: Boolean = false,
    val hwidTouched: Boolean = false,
) {
    /**
     * Custom User-Agent needs a value, a name must not be blank, header length is capped as in the core. The link must be
     * http(s): the core stores an edited address as typed and cannot open an encrypted link there (only when one is added).
     */
    val valid: Boolean
        get() = name.isNotBlank() && SubscriptionLogic.isWebLink(url) &&
            (ua != UaPreset.Custom || uaCustom.isNotBlank()) && uaCustom.length <= SubscriptionLogic.UA_MAX

    companion object {
        fun from(row: SubRow, hwid: Boolean = false): EditForm {
            val preset = UaPreset.of(row.userAgent)
            return EditForm(
                name = row.name,
                url = row.url,
                hours = SubscriptionLogic.nearestHours(row.intervalMin),
                ua = preset,
                uaCustom = if (preset == UaPreset.Custom) row.userAgent else "",
                hwid = hwid,
            )
        }
    }
}

/** The calls a saved form needs. Untouched fields stay null so a default never overwrites a stored value. */
@Immutable
data class EditPlan(
    val newName: String?,
    val newUrl: String?,
    val intervalMin: Int?,
    val userAgent: String?,
    val settings: Map<String, Boolean>,
) {
    val touchesGroup: Boolean get() = newName != null || intervalMin != null || userAgent != null
    val isEmpty: Boolean get() = newUrl == null && !touchesGroup && settings.isEmpty()
}

object SubscriptionEdit {
    fun plan(row: SubRow, f: EditForm): EditPlan {
        val name = f.name.trim()
        val url = f.url.trim()
        val ua = if (f.ua == UaPreset.Custom) f.uaCustom.trim() else f.ua.header.orEmpty()
        return EditPlan(
            newName = name.takeIf { it != row.name },
            newUrl = url.takeIf { it != row.url },
            intervalMin = if (f.intervalTouched) f.hours * 60 else null,
            userAgent = if (f.uaTouched) ua else null,
            settings = if (f.hwidTouched) mapOf(SubSettingKeys.HWID to f.hwid) else emptyMap(),
        )
    }

    /** The group with the planned changes; every other field is carried over because UpdateGroup replaces the whole group. */
    fun apply(g: Group, p: EditPlan): Group = g.copy(
        name = p.newName ?: g.name,
        updateIntervalMin = p.intervalMin ?: g.updateIntervalMin,
        userAgent = p.userAgent ?: g.userAgent,
    )

    /** Group sent to UpdateGroup to flip automatic updates, everything else untouched. */
    fun withAutoUpdate(g: Group, on: Boolean): Group = g.copy(skipAutoUpdate = !on)
}
