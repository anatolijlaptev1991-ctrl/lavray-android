// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CreateSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileUpdateGroup
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

    /** The core is already updating this subscription (its own scheduler holds it): this call did nothing and failed nothing. */
    data object AlreadyRunning : FetchResult
}

/** What else came of adding a link, besides how the update went. */
@Immutable
data class SubNotes(
    /** The core made a second group for an address that had one, and it could not be removed (it is parked: no automatic updates). */
    val duplicateLeft: Boolean = false,
    /** The stored address is plain http (a decrypted link can turn out to be), so the provider and the network see it. */
    val insecure: Boolean = false,
)

/** What "add this subscription link and load it" came to. The subscription is kept in every case except [Refused] and [Busy]. */
@Immutable
sealed interface SubscribeResult {
    data class Fetched(
        val name: String, val existed: Boolean, val added: Int, val removed: Int, val total: Int, val unparsedLines: Int,
        val notes: SubNotes = SubNotes(),
    ) : SubscribeResult

    data class NeedsConfirm(val name: String, val existed: Boolean, val remove: Int, val notes: SubNotes = SubNotes()) : SubscribeResult

    /**
     * The subscription is saved but its servers were not loaded. [hwidHint]: the provider may limit devices and the switch is
     * off. [coreProblem] is set when the core itself did not answer: [failure] then says nothing about the provider.
     */
    data class NotFetched(
        val name: String, val existed: Boolean, val failure: SubFailure, val hwidHint: Boolean,
        val coreProblem: ErrorKind? = null, val notes: SubNotes = SubNotes(),
    ) : SubscribeResult

    /** The subscription is saved and the core is updating it right now (its scheduler got there first); the servers arrive when that ends. */
    data class Updating(val name: String, val existed: Boolean, val notes: SubNotes = SubNotes()) : SubscribeResult

    /** The core refused the link (bad address, a happ link it cannot open) or could not be asked; nothing was kept. */
    data class Refused(val kind: ErrorKind, val line: Int?) : SubscribeResult

    /** Another update is running; nothing was done. */
    data object Busy : SubscribeResult
}

/** The group a subscription link belongs to, and whether it was there before. [duplicateLeft]: see [SubNotes]. */
internal data class Registered(val group: Group, val existed: Boolean, val duplicateLeft: Boolean = false)

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
        twinOf(known, link)?.let { return CoreResult.Ok(Registered(it, existed = true)) }
        val created = when (val r = session.call { subscriptionCreateSubscription(CreateSubscriptionRequest(name.ifEmpty { SubscriptionLogic.hostOf(link) }, link)) }) {
            is CoreResult.Fail -> return r
            is CoreResult.Ok -> r.value
        }
        val twin = twinOf(known, created.subscriptionUrl.orEmpty()) ?: return CoreResult.Ok(Registered(named(created, name), existed = false))
        return CoreResult.Ok(Registered(twin, existed = true, duplicateLeft = !dropDuplicate(created)))
    }

    /** A group with this address; a live one is preferred to an archived one (an archived group is not updated). */
    private fun twinOf(known: List<Group>, url: String): Group? {
        val same = known.filter { SubscriptionLogic.sameUrl(it.subscriptionUrl, url) }
        return same.firstOrNull { !it.archive } ?: same.firstOrNull()
    }

    /**
     * Removes the group the core made for an address that already had one. A failed delete is tried once more; if it still
     * fails the group is parked (no automatic updates), otherwise the core's scheduler would fill it with a second copy of
     * every server. False when it is still there.
     */
    private suspend fun dropDuplicate(made: Group): Boolean {
        repeat(DELETE_TRIES) {
            val r = session.call { subscriptionDeleteSubscription(DeleteSubscriptionRequest(made.id)) }
            // NOT_FOUND on the second try: the first delete got through and only its answer was lost.
            if (r is CoreResult.Ok || (r is CoreResult.Fail && r.kind == ErrorKind.NotFound)) return true
        }
        session.call { profileUpdateGroup(UpdateGroupRequest(SubscriptionEdit.withAutoUpdate(made, on = false))) }
        return false
    }

    /**
     * The core names the group of a link it had to decrypt after the whole address, user name and port included (that can be
     * a token). Unless the person typed a name, the host alone is the name, as for a link written out.
     */
    private suspend fun named(made: Group, typed: String): Group {
        val wanted = SubscriptionLogic.hostOf(made.subscriptionUrl.orEmpty())
        if (typed.isNotEmpty() || wanted.isEmpty() || wanted == made.name) return made
        val renamed = made.copy(name = wanted)
        // A rename that failed leaves the core's name in the core, but it is not carried on into what this app shows (it can
        // hold a token); lists show the host for such a name too ([SubscriptionLogic.displayName]).
        session.call { profileUpdateGroup(UpdateGroupRequest(renamed)) }
        return renamed
    }

    private companion object {
        const val DELETE_TRIES = 2
    }
}
