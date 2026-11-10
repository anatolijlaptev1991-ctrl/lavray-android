// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MemoryTunnelStore(var value: PersistedTunnel = PersistedTunnel()) : TunnelStore {
    var saves = 0
    override fun load() = value
    override fun save(state: PersistedTunnel) {
        value = state
        saves++
    }
}

class TunnelIntentTest {
    private val store = MemoryTunnelStore()
    private val intent = TunnelIntent(store)

    @Test
    fun userConnectSetsTheWishAndClearsComplaints() {
        store.value = PersistedTunnel(needsPermission = true, lastFailure = "old", failureKind = "x", failureReason = "r", unexpectedStop = true, stopReason = "low_memory")
        intent.onConnectRequested()
        val s = store.value
        assertTrue(s.wantConnected)
        assertFalse("the tunnel is on its way back", s.unexpectedStop)
        assertNull(s.stopReason)
        assertFalse(s.needsPermission)
        assertNull(s.lastFailure)
        assertNull(s.failureKind)
        assertNull(s.failureReason)
    }

    @Test
    fun staleWishInAFreshProcessIsAnUnexpectedStop() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onProcessStarted { "low_memory" }
        assertTrue(store.value.unexpectedStop)
        assertEquals("low_memory", store.value.stopReason)
        // A second look does not overwrite the first reason.
        intent.onProcessStarted { "crash" }
        assertEquals("low_memory", store.value.stopReason)
    }

    @Test
    fun freshProcessWithoutAWishIsNotAStopAndNeverAsksForTheReason() {
        var asked = false
        intent.onProcessStarted { asked = true; "low_memory" }
        assertFalse(store.value.unexpectedStop)
        assertFalse("the Binder call for the exit reason is made only when it is needed", asked)
        assertEquals(0, store.saves)
    }

    @Test
    fun unknownReasonIsNamed() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onProcessStarted { null }
        assertEquals(TunnelIntent.REASON_PROCESS_DIED, store.value.stopReason)
    }

    @Test
    fun connectedClearsTheStopButNeverWritesTheWish() {
        store.value = PersistedTunnel(wantConnected = false, unexpectedStop = true, stopReason = "low_memory", lastFailure = "m", failureKind = "k", resumeAttempts = 2)
        intent.onConnected()
        assertEquals("the crash counter waits for onStable", PersistedTunnel(wantConnected = false, resumeAttempts = 2), store.value)
        intent.onStable()
        assertEquals(PersistedTunnel(wantConnected = false), store.value)
        store.value = PersistedTunnel(wantConnected = true, unexpectedStop = true)
        intent.onConnected()
        assertTrue("a connect result does not clear a wish either", store.value.wantConnected)
    }

    @Test
    fun userDisconnectClearsTheWishAndTheStop() {
        store.value = PersistedTunnel(wantConnected = true, unexpectedStop = true, stopReason = "low_memory", resumeAttempts = 1)
        intent.onUserDisconnect()
        assertEquals(PersistedTunnel(), store.value)
    }

    @Test
    fun revokeEndsTheWishAndAsksForPermission() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onRevoked()
        assertFalse(store.value.wantConnected)
        assertTrue(store.value.needsPermission)
        assertFalse(store.value.unexpectedStop)
    }

    @Test
    fun givingUpAfterAnEstablishedTunnelIsUnexpected() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onGaveUp("tunnel_lost", "dropped", wasEstablished = true)
        val s = store.value
        assertFalse(s.wantConnected)
        assertTrue(s.unexpectedStop)
        assertEquals("tunnel_lost", s.stopReason)
        assertEquals("dropped", s.lastFailure)
        assertEquals("tunnel_lost", s.failureKind)
    }

    @Test
    fun failedFirstAttemptIsAFailureNotAStopAndKeepsTheCoresReason() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onGaveUp("no_server", "no server selected", wasEstablished = false, reason = "no_server")
        val s = store.value
        assertFalse(s.wantConnected)
        assertFalse(s.unexpectedStop)
        assertEquals("no_server", s.failureKind)
        assertEquals("no_server", s.failureReason)
    }

    @Test
    fun serviceStoppingByItselfOnlyClearsTheWish() {
        store.value = PersistedTunnel(wantConnected = true, resumeAttempts = 2)
        intent.onServiceStopped()
        assertEquals(PersistedTunnel(), store.value)
    }

    @Test
    fun missingPermissionIsPersisted() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onPermissionMissing()
        assertTrue(store.value.needsPermission)
        assertFalse(store.value.wantConnected)
        intent.onPermissionGranted()
        assertFalse(store.value.needsPermission)
    }

    @Test
    fun aNotedFailureKeepsTheSession() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onFailureNoted("protection_off", "apps go direct")
        assertTrue(store.value.wantConnected)
        assertEquals("protection_off", store.value.failureKind)
        intent.onConnected()
        assertNull(store.value.failureKind)
    }

    @Test
    fun noWriteWhenNothingChanges() {
        intent.onUserDisconnect()
        assertEquals(0, store.saves)
    }

    // ---- H1: the wish belongs to one boot

    @Test
    fun wishWrittenBeforeARebootReadsAsClearedAfterIt() {
        var boot = 5
        val i = TunnelIntent(store, { boot })
        i.onConnectRequested()
        assertEquals(5, store.value.bootCount)
        assertTrue(i.snapshot.wantConnected)
        boot = 6
        assertFalse("auto-connect is the only thing that carries intent across a reboot", i.snapshot.wantConnected)
        var asked = false
        i.onProcessStarted { asked = true; "stale exit info from before the reboot" }
        assertFalse(i.snapshot.unexpectedStop)
        assertFalse(asked)
    }

    @Test
    fun theRebootClearsTheStopFlagsToo() {
        var boot = 1
        val i = TunnelIntent(store, { boot })
        i.onConnectRequested()
        i.onProcessStarted { "low_memory" }
        assertTrue(i.snapshot.unexpectedStop)
        boot = 2
        val s = i.snapshot
        assertFalse(s.unexpectedStop)
        assertNull(s.stopReason)
        assertEquals(0, s.resumeAttempts)
    }

    @Test
    fun aBootStartWritesTheWishUnderTheNewBoot() {
        var boot = 1
        val i = TunnelIntent(store, { boot })
        i.onConnectRequested()
        boot = 2
        i.onConnectRequested() // auto-connect at boot
        assertTrue(i.snapshot.wantConnected)
        assertEquals(2, store.value.bootCount)
    }

    @Test
    fun informationalFieldsSurviveTheReboot() {
        var boot = 1
        val i = TunnelIntent(store, { boot })
        i.onGaveUp("no_server", "no server selected", wasEstablished = false)
        i.onPermissionMissing()
        boot = 2
        val s = i.snapshot
        assertEquals("no_server", s.failureKind)
        assertTrue(s.needsPermission)
    }

    // L-1: a ROM whose boot count is missing, constant, or not bumped by a fast-boot is told apart by the kernel's boot id.
    @Test
    fun aChangedBootIdIsARebootEvenWhenTheCountDoesNotSayIt() {
        var id: String? = "boot-a"
        val i = TunnelIntent(store, { 3 }, { id })
        i.onConnectRequested()
        assertTrue(i.snapshot.wantConnected)
        id = "boot-b"
        assertFalse(i.snapshot.wantConnected)
        assertEquals("boot-b", i.snapshot.bootId)
    }

    @Test
    fun aMissingCountIsCoveredByTheBootId() {
        var id: String? = "boot-a"
        val i = TunnelIntent(store, { -1 }, { id })
        i.onConnectRequested()
        id = "boot-b"
        assertFalse(i.snapshot.wantConnected)
    }

    @Test
    fun anIdSeenForTheFirstTimeIsAdoptedWithoutClearingAnything() {
        val old = TunnelIntent(store, { 3 })
        old.onConnectRequested() // written before ids were recorded
        val i = TunnelIntent(store, { 3 }, { "boot-a" })
        assertTrue(i.snapshot.wantConnected)
        assertEquals("boot-a", i.snapshot.bootId)
    }

    @Test
    fun anUnreadableIdKeepsWhatWasStored() {
        var id: String? = "boot-a"
        val i = TunnelIntent(store, { 3 }, { id })
        i.onConnectRequested()
        id = null
        assertTrue(i.snapshot.wantConnected)
        assertEquals("boot-a", i.snapshot.bootId)
    }

    // N2: an exit record only labels the stop. user_requested is a force-stop, a swipe, an OEM cleaner or (before API 34) an
    // update: none of them may be read as "the person wants the tunnel off".
    @Test
    fun noExitReasonClearsTheWish() {
        for (reason in listOf("user_stopped", "user_requested", "low_memory", "crash_native", "signaled", null)) {
            store.value = PersistedTunnel(wantConnected = true)
            intent.onProcessStarted { reason }
            assertTrue(reason.toString(), store.value.wantConnected)
            assertTrue(reason.toString(), store.value.unexpectedStop)
            assertEquals(reason ?: TunnelIntent.REASON_PROCESS_DIED, store.value.stopReason)
        }
    }

    // N11: uptime only grows within a boot, so a smaller one now is a reboot even when no boot id was recorded before.
    @Test
    fun aSmallerUptimeThanAtTheLastWriteIsARebootEvenWithoutAnId() {
        var up = 5_000_000L
        val i = TunnelIntent(store, { -1 }, { null }, { up })
        i.onConnectRequested()
        up += 1_000
        assertTrue(i.snapshot.wantConnected)
        up = 60_000L // the phone restarted: uptime started over
        assertFalse(i.snapshot.wantConnected)
    }

    @Test
    fun theUptimeStampAloneNeverCausesAWrite() {
        var up = 1_000L
        val i = TunnelIntent(store, { -1 }, { null }, { up })
        i.onConnectRequested()
        val saves = store.saves
        up += 10_000
        i.onUserDisconnect()
        i.onUserDisconnect()
        assertEquals(saves + 1, store.saves)
    }

    @Test
    fun anUnknownUptimeDisablesTheClockCheck() {
        val i = TunnelIntent(store, { -1 }, { null }, { -1 })
        i.onConnectRequested()
        assertTrue(i.snapshot.wantConnected)
    }

    @Test
    fun unknownBootCountDisablesTheCheck() {
        val i = TunnelIntent(store, { -1 })
        i.onConnectRequested()
        assertTrue(i.snapshot.wantConnected)
    }

    // ---- two instances over one file (the receiver's and the service's)

    @Test
    fun instancesOverTheSameStoreSeeEachOthersWrites() {
        val service = TunnelIntent(store)
        val receiver = TunnelIntent(store)
        receiver.onConnectRequested()
        assertTrue(service.snapshot.wantConnected)
        service.onFailureNoted("k", "m")
        receiver.onPermissionGranted()
        assertEquals("k", receiver.snapshot.failureKind)
        assertTrue("a write by the other instance is not overwritten by a stale copy", receiver.snapshot.wantConnected)
    }

    @Test
    fun concurrentWritersDoNotLoseEachOthersFields() {
        val a = TunnelIntent(store)
        val b = TunnelIntent(store)
        val rounds = 500
        val t1 = Thread { repeat(rounds) { a.onResumeAttempt() } }
        val t2 = Thread { repeat(rounds) { b.onResumeAttempt() } }
        t1.start(); t2.start(); t1.join(); t2.join()
        assertEquals("every read-modify-write is atomic across instances", 2 * rounds, store.value.resumeAttempts)
    }

    // ---- the restart policy

    @Test
    fun resumePolicy() {
        val wanted = PersistedTunnel(wantConnected = true)
        assertEquals(ResumeAction.STOP_QUIET, ResumePolicy.decide(PersistedTunnel(), true))
        assertEquals(ResumeAction.NEEDS_PERMISSION, ResumePolicy.decide(wanted, false))
        assertEquals(ResumeAction.CONNECT, ResumePolicy.decide(wanted, true))
        assertEquals(ResumeAction.CONNECT, ResumePolicy.decide(wanted.copy(resumeAttempts = ResumePolicy.MAX_ATTEMPTS - 1), true))
        assertEquals(ResumeAction.GIVE_UP, ResumePolicy.decide(wanted.copy(resumeAttempts = ResumePolicy.MAX_ATTEMPTS), true))
    }
}
