// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Page
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateSubscriptionSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileUpdateGroup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsSetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionUpdateSubscriptionSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope

/** How many servers a subscription holds, asked of the core (the group's own order is only a fallback). */
internal object SubscriptionCounts {
    /** Servers per subscription: the core's total for the group, the group's own order when that call fails. */
    suspend fun perGroup(session: CoreSession, groups: List<Group>): Map<Long, Int> = coroutineScope {
        groups.map { g ->
            async { g.id to (of(session, g.id) ?: g.profileOrder.size) }
        }.awaitAll().toMap()
    }

    /** Null when the core could not be asked. */
    suspend fun of(session: CoreSession, groupId: Long): Int? =
        (session.call { profileListProfiles(ListProfilesRequest(groupId = groupId, page = Page(offset = 0, limit = 1))) } as? CoreResult.Ok)?.value?.total?.toInt()
}

/** Writes what the edit sheet changed: the address, the group fields (one whole-group write) and the shared settings. */
internal suspend fun LavrayClient.applyPlan(g: Group, plan: EditPlan) {
    var group = g
    plan.newUrl?.let { group = subscriptionUpdateSubscriptionSettings(UpdateSubscriptionSettingsRequest(g.id, it)) }
    if (plan.touchesGroup) profileUpdateGroup(UpdateGroupRequest(SubscriptionEdit.apply(group, plan)))
    if (plan.settings.isNotEmpty()) {
        settingsSetSettings(SetSettingsRequest(plan.settings.mapValues { SettingValue(boolValue = it.value) }))
    }
}
