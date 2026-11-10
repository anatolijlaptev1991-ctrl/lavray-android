// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import io.github.anatolijlaptev1991_ctrl.lavray.api.ProtoJson
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteProfileRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Empty
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListGroupsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListSubscriptionsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RollbackSubscriptionResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.EditForm
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.RelUnit
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.RelativeTime
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.RelativeWording
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.wordingOf
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailureStorage
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UaPreset
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

private class NoFailures : SubFailureStorage {
    override fun load(): Map<Long, SubFailure> = emptyMap()
    override fun save(failures: Map<Long, SubFailure>) = Unit
}

private fun sub(id: Long, name: String = "Mine", skipAuto: Boolean = false) =
    Group(id = id, name = name, kind = Group.Kind.KIND_SUBSCRIPTION, subscriptionUrl = "https://p.example/s/$id", skipAutoUpdate = skipAuto)

/**
 * Contract defects found by the audit against the core, each pinned by what the core really answers:
 * the empty RollbackSubscription answer that means "done", refused batches that changed some items, whole-group updates
 * from a stale copy, and the "servers changed" signal after updates.
 */
class ContractFixesTests {
    private val core = FakeCore()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val changed = AtomicInteger()
    private val subscriptions = SubscriptionsModel(core.session, scope, NoFailures()) { changed.incrementAndGet() }
    private val servers = ServersModel(core.session, scope) { }

    @After
    fun stop() = scope.cancel()

    private fun row(id: Long = 3) = SubRow.from(sub(id), count = 5)

    private fun subs(vararg g: Group) =
        core.on(SubscriptionService.LIST_SUBSCRIPTIONS, reply(ListSubscriptionsResponse.serializer(), ListSubscriptionsResponse(g.toList())))

    private fun total(n: Long) = core.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse(total = n)))

    private fun <T> firstEvent(flow: kotlinx.coroutines.flow.Flow<T>, action: () -> Unit): T = runBlocking {
        val e = async(Dispatchers.Default) { withTimeout(5_000) { flow.first() } }
        delay(50)
        action()
        e.await()
    }

    // ---- rollback

    @Test
    fun aRollbackTheCoreAnswersWithAnEmptyMessageIsASuccessWithTheRealCount() {
        // The core returns RollbackSubscriptionResponse{}: success=false, no count. That is what "done" looks like.
        core.on(SubscriptionService.ROLLBACK_SUBSCRIPTION, reply(RollbackSubscriptionResponse.serializer(), RollbackSubscriptionResponse()))
        subs(sub(3))
        total(7)

        val e = firstEvent(subscriptions.events) { subscriptions.rollback(row()) }

        assertEquals(SubscriptionsEvent.RolledBack("Mine", 7), e)
        runBlocking { withTimeout(5_000) { while (changed.get() == 0) delay(20) } }
    }

    @Test
    fun aRollbackWithAnErrorTextIsAFailure() {
        core.on(SubscriptionService.ROLLBACK_SUBSCRIPTION, reply(RollbackSubscriptionResponse.serializer(), RollbackSubscriptionResponse(error = "nope")))

        assertEquals(SubscriptionsEvent.RollbackFailed, firstEvent(subscriptions.events) { subscriptions.rollback(row()) })
        assertEquals(0, changed.get())
    }

    @Test
    fun aRollbackThatReportsItsOwnCountKeepsIt() {
        core.on(SubscriptionService.ROLLBACK_SUBSCRIPTION, reply(RollbackSubscriptionResponse.serializer(), RollbackSubscriptionResponse(success = true, restoredCount = 4)))
        subs(sub(3))
        total(9)

        assertEquals(SubscriptionsEvent.RolledBack("Mine", 4), firstEvent(subscriptions.events) { subscriptions.rollback(row()) })
    }

    // ---- servers changed after an update

    @Test
    fun anUpdateOfASubscriptionTellsTheScreensThatListServers() {
        subs(sub(3))
        total(2)
        core.on(
            SubscriptionService.UPDATE_SUBSCRIPTION,
            Reply.Frames(
                listOf(
                    SubscriptionProgress(groupId = 3, phase = Phase.PHASE_DIFFING, addedCount = 2, totalCount = 2),
                    SubscriptionProgress(groupId = 3, phase = Phase.PHASE_DONE),
                ),
            ),
        )

        firstEvent(subscriptions.events) { subscriptions.update(3) }

        runBlocking { withTimeout(5_000) { while (changed.get() == 0) delay(20) } }
    }

    // ---- whole-group updates are sent what the core holds now

    @Test
    fun switchingAutoUpdateSendsTheCurrentGroupNotTheLoadedCopy() {
        // The list on screen was loaded when the group was called "Old"; it was renamed since.
        subs(sub(3, name = "Renamed"))
        core.on(ProfileService.UPDATE_GROUP, reply(Group.serializer(), sub(3, name = "Renamed", skipAuto = true)))
        total(1)

        subscriptions.toggleAuto(SubRow.from(sub(3, name = "Old"), count = 1))
        runBlocking { withTimeout(5_000) { while (core.callsTo(ProfileService.UPDATE_GROUP).isEmpty()) delay(20) } }

        val sent = ProtoJson.decodeFromString(UpdateGroupRequest.serializer(), core.callsTo(ProfileService.UPDATE_GROUP).single().body).group!!
        assertEquals("Renamed", sent.name)
        assertTrue(sent.skipAutoUpdate)
    }

    // ---- servers: a refused delete

    @Test
    fun aRefusedDeleteOfTheConnectedServerSaysWhyAndReadsTheListAgain() {
        core.on(ProfileService.DELETE_PROFILE, Reply.Fail("failed_precondition", "профиль 4 активен"))
        core.on(ProfileService.LIST_GROUPS, reply(ListGroupsResponse.serializer(), ListGroupsResponse()))
        total(1)

        val e = firstEvent(servers.events) { servers.delete(setOf(4, 5)) }

        assertEquals(ServersEvent.InUse, e)
        runBlocking { withTimeout(5_000) { while (core.callsTo(ProfileService.LIST_GROUPS).isEmpty()) delay(20) } }
        assertEquals(setOf(4L, 5L), ProtoJson.decodeFromString(DeleteProfileRequest.serializer(), core.callsTo(ProfileService.DELETE_PROFILE).single().body).ids.toSet())
    }

    @Test
    fun otherRefusalsKeepTheirKind() {
        core.on(ProfileService.DELETE_PROFILE, Reply.Fail("not_found", "x"))
        core.on(ProfileService.LIST_GROUPS, reply(ListGroupsResponse.serializer(), ListGroupsResponse()))
        total(0)

        assertEquals(ServersEvent.Failed(ErrorKind.NotFound), firstEvent(servers.events) { servers.delete(setOf(4)) })
    }

    @Test
    fun aDeleteTheCoreAcceptsIsReportedWithItsCount() {
        core.on(ProfileService.DELETE_PROFILE, reply(Empty.serializer(), Empty))

        assertEquals(ServersEvent.Deleted(2), firstEvent(servers.events) { servers.delete(setOf(4, 5)) })
    }

    // ---- the subscription edit form

    @Test
    fun anEditedAddressMustBeHttpBecauseTheCoreStoresItAsTypedAndCannotOpenAnEncryptedLinkThere() {
        val form = EditForm(name = "n", url = "https://p.example/s/1", hours = 6, ua = UaPreset.Default, uaCustom = "")
        assertTrue(form.valid)
        assertFalse(form.copy(url = "happ://crypt3/AAAA").valid)
        assertFalse(form.copy(url = "").valid)
        assertFalse(form.copy(url = "ftp://p.example/x").valid)
    }
}

/**
 * Found on the emulator: a subscription updated a few seconds ago made the Subscriptions screen crash, because "now" was
 * formatted with Direction.THIS and ICU accepts only PLAIN for it. The wording is decided apart from ICU so this is testable.
 */
class RelativeWordingTests {
    private val now = 1_000_000_000L

    private fun wording(deltaMs: Long) = wordingOf(RelativeTime.span(now + deltaMs, now))

    @Test
    fun aMomentWithinHalfAMinuteIsNowInEitherDirection() {
        assertEquals(RelativeWording.Now, wording(0))
        assertEquals(RelativeWording.Now, wording(-12_000))
        assertEquals(RelativeWording.Now, wording(20_000))
    }

    @Test
    fun otherSpansKeepTheirDirectionAndUnit() {
        assertEquals(RelativeWording.Amount(5, RelUnit.Minute, future = false), wording(-5 * 60_000L))
        assertEquals(RelativeWording.Amount(2, RelUnit.Hour, future = true), wording(2 * 3_600_000L))
        assertEquals(RelativeWording.AdjacentDay(future = true), wording(86_400_000L))
        assertEquals(RelativeWording.AdjacentDay(future = false), wording(-86_400_000L))
        assertEquals(RelativeWording.Amount(3, RelUnit.Day, future = true), wording(3 * 86_400_000L))
    }
}

/** A backup larger than the client will hold is "too large", not "the core is unavailable", and the client is kept. */
class OversizedAnswerTests {
    private class Conn(override val input: java.io.InputStream) : io.github.anatolijlaptev1991_ctrl.lavray.api.Connection {
        override val output: java.io.OutputStream = java.io.ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    @Test
    fun anAnswerBeyondTheLimitIsResourceExhaustedAndWordedAsAFailureNotAMissingCore() = runBlocking {
        val body = ByteArray(2_000) { 'a'.code.toByte() }
        val http = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body
        val drops = AtomicInteger()
        val session = io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession(
            { io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint("/x", "t") },
            { drops.incrementAndGet(); io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient("t", { Conn(java.io.ByteArrayInputStream(http)) }, Dispatchers.IO, maxUnaryBytes = 1_000) },
        )
        val r = session.call { coreGetStatus() }
        assertTrue(r is io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult.Fail)
        assertEquals(ErrorKind.Generic, (r as io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult.Fail).kind)
        session.call { coreGetStatus() }
        assertEquals("the client was not dropped, so it was built once", 1, drops.get())
    }
}
