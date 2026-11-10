// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CreateSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionCreateSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionDeleteSubscription
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionListSubscriptions
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind

/** How one single-subscription update ended, for callers that wait for it (the import sheet) and for the screen's own toast. */
sealed interface FetchResult {
    /** [added], [removed] and [total] are the core's counts after it saved; [unparsedLines] is what it could not read. */
    data class Done(val added: Int, val removed: Int, val total: Int, val unparsedLines: Int) : FetchResult

    /** The update would remove [remove] servers; nothing changed until the person agrees. */
    data class NeedsConfirm(val remove: Int) : FetchResult

    /** [coreProblem] is set when the core itself did not answer, so [failure] says nothing about the provider. */
    data class Failed(val failure: SubFailure, val coreProblem: ErrorKind?) : FetchResult
}

/** What "add this subscription link and load it" came to. The subscription is kept in every case except [Refused] and [Busy]. */
@Immutable
sealed interface SubscribeResult {
    data class Fetched(val name: String, val existed: Boolean, val added: Int, val removed: Int, val total: Int, val unparsedLines: Int) : SubscribeResult
    data class NeedsConfirm(val name: String, val existed: Boolean, val remove: Int) : SubscribeResult

    /** The subscription is saved but its servers were not loaded. [hwidHint]: the provider may limit devices and the switch is off. */
    data class NotFetched(val name: String, val existed: Boolean, val failure: SubFailure, val hwidHint: Boolean) : SubscribeResult

    /** The core refused the link (bad address, a happ link it cannot open) or could not be asked; nothing was kept. */
    data class Refused(val kind: ErrorKind, val line: Int?) : SubscribeResult

    /** Another update is running; nothing was done. */
    data object Busy : SubscribeResult
}

/** The group a subscription link belongs to, and whether it was there before. */
internal data class Registered(val group: Group, val existed: Boolean)

/** Failure kinds after which a provider's device limit is a plausible cause. */
internal val DEVICE_LIMIT_SUSPECTS: Set<SubErrorKind> = setOf(SubErrorKind.Auth, SubErrorKind.Zero, SubErrorKind.Format)

/**
 * Finds or makes the group for a subscription link and never makes a second one for an address that has a group.
 * The core does not check this itself (CreateSubscription always adds). For an address written out the local list answers;
 * for a link only the core can read (an encrypted one) the group the core made is compared with the others and dropped
 * when it is a twin.
 */
internal class SubscriptionRegistry(private val session: CoreSession) {
    /** [name] is what the person typed; empty lets the core name the group after the address. */
    suspend fun register(link: String, name: String = ""): CoreResult<Registered> {
        val known = when (val r = session.call { subscriptionListSubscriptions() }) {
            is CoreResult.Fail -> return r
            is CoreResult.Ok -> r.value.groups
        }
        known.firstOrNull { SubscriptionLogic.sameUrl(it.subscriptionUrl, link) }?.let { return CoreResult.Ok(Registered(it, existed = true)) }
        val made = when (val r = session.call { subscriptionCreateSubscription(CreateSubscriptionRequest(name.ifEmpty { SubscriptionLogic.hostOf(link) }, link)) }) {
            is CoreResult.Fail -> return r
            is CoreResult.Ok -> r.value
        }
        val twin = known.firstOrNull { SubscriptionLogic.sameUrl(it.subscriptionUrl, made.subscriptionUrl) }
            ?: return CoreResult.Ok(Registered(made, existed = false))
        return when (val r = session.call { subscriptionDeleteSubscription(DeleteSubscriptionRequest(made.id)) }) {
            is CoreResult.Fail -> r
            is CoreResult.Ok -> CoreResult.Ok(Registered(twin, existed = true))
        }
    }
}
