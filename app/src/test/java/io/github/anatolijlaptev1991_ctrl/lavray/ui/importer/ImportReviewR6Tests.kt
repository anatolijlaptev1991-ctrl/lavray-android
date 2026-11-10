// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Empty
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListSubscriptionsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionService
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailureStorage
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscribeResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionLogic
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

private class Store : SubFailureStorage {
    override fun load(): Map<Long, SubFailure> = emptyMap()
    override fun save(failures: Map<Long, SubFailure>) = Unit
}

/** Review R6, items L7, L8, L9 (classifier edge cases, sameUrl, hostOf, the name after a failed rename). */
class ImportReviewR6Tests {
    private val core = FakeCore()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val subscriptions = SubscriptionsModel(core.session, scope, Store())

    @After
    fun stop() = scope.cancel()

    private fun group(id: Long, url: String, name: String = "provider.example") =
        Group(id = id, name = name, kind = Group.Kind.KIND_SUBSCRIPTION, subscriptionUrl = url)

    private fun existing(vararg groups: Group) =
        core.on(SubscriptionService.LIST_SUBSCRIPTIONS, reply(ListSubscriptionsResponse.serializer(), ListSubscriptionsResponse(groups.toList())))

    private fun fetchOk(id: Long) = Reply.Frames(
        listOf(SubscriptionProgress(groupId = id, phase = Phase.PHASE_DIFFING, addedCount = 1, totalCount = 1), SubscriptionProgress(groupId = id, phase = Phase.PHASE_DONE)),
    )

    // ---- L7: a job cancelled before its first line leaves no stuck claim

    @Test fun anUpdateCancelledAtOnceNeverLeavesTheNextOneRefusedAsBusy() {
        existing(group(1, "https://p.example/s"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(1))
        core.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse(total = 1)))
        repeat(300) {
            subscriptions.update(1)
            subscriptions.cancelRunning()
        }
        runBlocking { delay(300) }
        val busy = scope.let { s ->
            val seen = AtomicInteger()
            val job = s.launch(Dispatchers.Default) { subscriptions.events.collect { if (it == SubscriptionsEvent.Busy) seen.incrementAndGet() } }
            runBlocking { delay(50) }
            subscriptions.update(1)
            runBlocking { delay(300) }
            job.cancel()
            seen.get()
        }
        // The loop above refused some updates while one still ran (that is its job); the last one, with nothing running, must not be.
        assertEquals("the claim was released by every cancelled job", 0, busy)
    }

    // ---- L8: the first delete got through, its answer was lost

    @Test fun aSecondDeleteThatFindsNothingMeansTheFirstOneWorked() {
        existing(group(1, "https://panel.example/api/sub/ABC", name = "panel.example"))
        core.on(SubscriptionService.CREATE_SUBSCRIPTION, reply(Group.serializer(), group(2, "https://panel.example/api/sub/ABC", name = "panel.example")))
        val tries = AtomicInteger()
        core.on(SubscriptionService.DELETE_SUBSCRIPTION) { if (tries.incrementAndGet() == 1) Reply.Fail("unavailable", "answer lost") else Reply.Fail("not_found", "no such group") }
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(1))
        core.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse(total = 1)))
        core.on(ProfileService.UPDATE_GROUP, reply(Empty.serializer(), Empty))

        val r = runBlocking { subscriptions.addAndFetch("happ://crypt3/AAAA") } as SubscribeResult.Fetched

        assertFalse("nothing is left over, so nothing is said", r.notes.duplicateLeft)
        assertTrue("and nothing was parked", core.callsTo(ProfileService.UPDATE_GROUP).isEmpty())
    }

    // ---- L9: what the preview may call a subscription

    private fun sub(url: String) = LinkClassifier.isSubscriptionPage(url)

    @Test fun thePreviewReadsAddressesAsTheCoreDoes() {
        assertFalse("a path of encoded slashes is no path", sub("https://h.example/%2F%2f"))
        assertTrue("an encoded path is a path", sub("https://h.example/%73ub"))
        assertTrue("an empty key counts", sub("https://h.example:8443?=x"))
        assertFalse("a pair with a semicolon is dropped", sub("https://h.example:8443?token;x=1"))
        assertFalse("a pair with a broken escape is dropped", sub("https://h.example:8443?%zz=1"))
        assertFalse("an encoded security key is still security", sub("https://h.example:8443?%73ecurity=tls"))
        assertTrue("security plus another key", sub("https://h.example:8443?security=tls&token=1"))
        assertFalse("no host, no address", sub("http:///sub/x"))
        assertFalse("no host with a port only", sub("http://:80/x"))
        assertFalse("a broken escape in the path makes the address unreadable", sub("https://h.example/%zz"))
        assertTrue(sub("https://[2001:db8::1]:8443/sub"))
        assertFalse(sub("https://[2001:db8::1]:8443"))
    }

    // ---- L9: two different subscriptions are never one

    @Test fun userInfoIsCaseSensitiveAndDefaultPortsAreNone() {
        assertFalse("a token in the user info differs by case", SubscriptionLogic.sameUrl("https://TokenA@p.example/s", "https://tokena@p.example/s"))
        assertTrue(SubscriptionLogic.sameUrl("https://tokenA@P.EXAMPLE/s", "HTTPS://tokenA@p.example/s"))
        assertTrue("the default port is no port", SubscriptionLogic.sameUrl("https://p.example:443/s", "https://p.example/s"))
        assertTrue(SubscriptionLogic.sameUrl("http://p.example:80/s?t=1", "http://p.example/s?t=1"))
        assertFalse("another scheme's default port is a port", SubscriptionLogic.sameUrl("http://p.example:443/s", "http://p.example/s"))
        assertFalse(SubscriptionLogic.sameUrl("https://p.example:8443/s", "https://p.example/s"))
        assertFalse("the path stays case sensitive", SubscriptionLogic.sameUrl("https://p.example/Abc", "https://p.example/abc"))
        assertTrue(SubscriptionLogic.sameUrl("https://[2001:DB8::1]:443/s", "https://[2001:db8::1]/s"))
        assertTrue(SubscriptionLogic.sameUrl("https://p.example/", "https://p.example"))
    }

    @Test fun theHostOfAnIpv6AddressIsTheWholeBracketedHost() {
        assertEquals("[2001:db8::1]", SubscriptionLogic.hostOf("https://[2001:db8::1]:8443/sub?t=1"))
        assertEquals("[::1]", SubscriptionLogic.hostOf("http://[::1]/s"))
        assertEquals("panel.example", SubscriptionLogic.hostOf("https://user:secret@panel.example:8443/api?token=x"))
        assertEquals("", SubscriptionLogic.hostOf("happ://crypt3/AAAA"))
    }

    // ---- L9: a name that is the whole address is never shown

    @Test fun aGroupStillNamedAfterItsWholeAddressIsShownByItsHost() {
        val url = "https://user:secret@panel.example:8443/api/sub/ABC?token=x"
        assertEquals("panel.example", SubscriptionLogic.displayName("user:secret@panel.example:8443", url))
        assertEquals("panel.example", SubscriptionLogic.displayName("user:secret@panel.example:8443/api/sub/ABC?token=x", url))
        assertEquals("a name the person chose is theirs", "me@work", SubscriptionLogic.displayName("me@work", url))
        assertEquals("Work", SubscriptionLogic.displayName("Work", url))
        assertEquals("panel.example", SubRow.from(group(3, url, name = "user:secret@panel.example:8443"), 0).name)
    }

    @Test fun aRenameThatFailedLeavesNoTokenInTheResultOrTheResultingList() {
        val url = "https://user:secret@panel.example:8443/api/sub/ABC?token=x"
        val made = group(5, url, name = "user:secret@panel.example:8443")
        val created = java.util.concurrent.atomic.AtomicBoolean(false)
        core.on(SubscriptionService.LIST_SUBSCRIPTIONS) {
            reply(ListSubscriptionsResponse.serializer(), ListSubscriptionsResponse(if (created.get()) listOf(made) else emptyList()))
        }
        core.on(SubscriptionService.CREATE_SUBSCRIPTION) { created.set(true); reply(Group.serializer(), made) }
        core.on(ProfileService.UPDATE_GROUP, Reply.Fail("unavailable", "db locked"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(5))
        core.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse(total = 1)))

        val r = runBlocking { subscriptions.addAndFetch("happ://crypt3/AAAA") } as SubscribeResult.Fetched

        assertEquals("panel.example", r.name)
        runBlocking { withTimeout(5_000) { subscriptions.state.first { it.rows.isNotEmpty() } } }
        assertEquals("panel.example", subscriptions.state.value.rows.single().name)
    }
}
