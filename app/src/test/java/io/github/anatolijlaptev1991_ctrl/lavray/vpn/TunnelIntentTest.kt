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
        store.value = PersistedTunnel(needsPermission = true, lastFailure = "old", failureKind = "x")
        intent.onConnectRequested()
        val s = store.value
        assertTrue(s.wantConnected)
        assertFalse(s.needsPermission)
        assertNull(s.lastFailure)
        assertNull(s.failureKind)
    }

    @Test
    fun staleWishInAFreshProcessIsAnUnexpectedStop() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onProcessStarted("low_memory")
        assertTrue(store.value.unexpectedStop)
        assertEquals("low_memory", store.value.stopReason)
        // A second look does not overwrite the first reason.
        intent.onProcessStarted("crash")
        assertEquals("low_memory", store.value.stopReason)
    }

    @Test
    fun freshProcessWithoutAWishIsNotAStop() {
        intent.onProcessStarted("low_memory")
        assertFalse(store.value.unexpectedStop)
        assertEquals(0, store.saves)
    }

    @Test
    fun unknownReasonIsNamed() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onProcessStarted(null)
        assertEquals(TunnelIntent.REASON_PROCESS_DIED, store.value.stopReason)
    }

    @Test
    fun connectedClearsTheStop() {
        store.value = PersistedTunnel(wantConnected = true, unexpectedStop = true, stopReason = "low_memory", lastFailure = "m", failureKind = "k")
        intent.onConnected()
        assertEquals(PersistedTunnel(wantConnected = true), store.value)
    }

    @Test
    fun userDisconnectClearsTheWishAndTheStop() {
        store.value = PersistedTunnel(wantConnected = true, unexpectedStop = true, stopReason = "low_memory")
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
    fun failedFirstAttemptIsAFailureNotAStop() {
        store.value = PersistedTunnel(wantConnected = true)
        intent.onGaveUp("no_server", "no server selected", wasEstablished = false)
        val s = store.value
        assertFalse(s.wantConnected)
        assertFalse(s.unexpectedStop)
        assertEquals("no_server", s.failureKind)
    }

    @Test
    fun bootWithoutAutoConnectDropsTheOldWish() {
        store.value = PersistedTunnel(wantConnected = true, unexpectedStop = true, stopReason = "r", lastFailure = "keep")
        intent.onBootWithoutAutoConnect()
        assertEquals(PersistedTunnel(lastFailure = "keep"), store.value)
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
}
