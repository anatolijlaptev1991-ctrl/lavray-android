// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.util.concurrent.CopyOnWriteArrayList

// ---- BootLogic: both receivers, with fake actions

class BootLogicTest {
    private val store = MemoryTunnelStore()
    private var boot = 1

    private class Actions(var consent: Boolean = true, var startFails: RuntimeException? = null, var exit: String? = null) : BootActions {
        var starts = 0
        var permissionNotices = 0
        val failures = ArrayList<Pair<String, String?>>()
        override fun consentGranted() = consent
        override fun lastExitReason() = exit
        override fun startService() {
            startFails?.let { throw it }
            starts++
        }
        override fun notifyPermissionNeeded() { permissionNotices++ }
        override fun notifyFailure(kind: String, message: String?) { failures += kind to message }
    }

    private fun intent() = TunnelIntent(store, { boot })

    @Test
    fun bootWithAutoConnectStartsAndWritesTheWish() {
        val a = Actions()
        BootLogic.run(BootTrigger.BOOT, autoConnect = true, intent = intent(), actions = a)
        assertEquals(1, a.starts)
        assertTrue(store.value.wantConnected)
    }

    @Test
    fun bootWithoutAutoConnectDoesNothing() {
        val a = Actions()
        BootLogic.run(BootTrigger.BOOT, autoConnect = false, intent = intent(), actions = a)
        assertEquals(0, a.starts)
        assertFalse(store.value.wantConnected)
    }

    // H1 scenario 2: tunnel on, auto-connect off, phone rebooted, then the app is updated.
    @Test
    fun updateAfterARebootDoesNotRestoreAWishFromBeforeIt() {
        intent().onConnectRequested()
        boot = 2
        val a = Actions()
        BootLogic.run(BootTrigger.UPDATE, autoConnect = false, intent = intent(), actions = a)
        assertEquals(0, a.starts)
    }

    @Test
    fun updateWithoutARebootRestoresAWantedTunnel() {
        intent().onConnectRequested()
        val a = Actions()
        BootLogic.run(BootTrigger.UPDATE, autoConnect = false, intent = intent(), actions = a)
        assertEquals(1, a.starts)
    }

    // L-10: the person force-stopped the app, then an update arrived.
    @Test
    fun updateAfterAForceStopDoesNotRestoreTheTunnel() {
        intent().onConnectRequested()
        val a = Actions(exit = "user_stopped")
        BootLogic.run(BootTrigger.UPDATE, autoConnect = false, intent = intent(), actions = a)
        assertEquals(0, a.starts)
        assertFalse(store.value.wantConnected)
    }

    @Test
    fun updateThatKilledARunningTunnelStillRestoresIt() {
        intent().onConnectRequested()
        val a = Actions(exit = "package_updated")
        BootLogic.run(BootTrigger.UPDATE, autoConnect = false, intent = intent(), actions = a)
        assertEquals(1, a.starts)
    }

    @Test
    fun lostConsentBecomesAPersistedNoticeAndAnAlert() {
        val a = Actions(consent = false)
        BootLogic.run(BootTrigger.BOOT, autoConnect = true, intent = intent(), actions = a)
        assertEquals(1, a.permissionNotices)
        assertTrue(store.value.needsPermission)
        assertFalse(store.value.wantConnected)
        assertEquals(0, a.starts)
    }

    @Test
    fun aRefusedStartIsRecordedAndShownNotACrash() {
        val a = Actions(startFails = IllegalStateException("not allowed"))
        BootLogic.run(BootTrigger.BOOT, autoConnect = true, intent = intent(), actions = a)
        assertEquals(listOf(FailureKind.RESTRICTED_START to "not allowed"), a.failures)
        assertEquals(FailureKind.RESTRICTED_START, store.value.failureKind)
        assertFalse(store.value.wantConnected)
    }

    @Test
    fun theServicesInstanceSeesWhatTheReceiversInstanceWrote() {
        val receiver = intent()
        val service = intent()
        BootLogic.run(BootTrigger.BOOT, true, receiver, Actions())
        assertTrue(service.snapshot.wantConnected)
    }
}

// ---- CoreWatcher: the event stream is reopened and re-seeded

class CoreWatcherTest {
    private fun st(state: CoreState) = CoreStatus(state = state)

    private class Api(var status: CoreStatus, val streams: List<Flow<CoreStatus>>) : CoreApi {
        var opened = 0
        override suspend fun status() = status
        override suspend fun connect(profileId: Long) = ConnectReply(null)
        override suspend fun lastProfileId(): Long? = null
        override fun events(): Flow<CoreStatus> = streams[minOf(opened++, streams.size - 1)]
    }

    @Test
    fun aStreamThatEndsIsReopenedAndTheStateIsSeededAgain() = runBlocking {
        val seen = CopyOnWriteArrayList<CoreState>()
        val second = MutableSharedFlow<CoreStatus>(extraBufferCapacity = 8)
        val api = Api(st(CoreState.CORE_STATE_CONNECTED), listOf(flow { /* ends at once */ }, second))
        val gotSeeds = CompletableDeferred<Unit>()
        val job = launch {
            CoreWatcher.keep(api, { seen += it.state; if (seen.size >= 2) gotSeeds.complete(Unit) }, { true }, retryMs = 10)
        }
        withTimeout(2_000) { gotSeeds.await() }
        assertEquals(listOf(CoreState.CORE_STATE_CONNECTED, CoreState.CORE_STATE_CONNECTED), seen.take(2))
        assertTrue("reopened", api.opened >= 2)
        // The reopened stream delivers.
        api.status = st(CoreState.CORE_STATE_IDLE)
        second.emit(st(CoreState.CORE_STATE_IDLE))
        withTimeout(2_000) { while (seen.last() != CoreState.CORE_STATE_IDLE) delay(5) }
        job.cancelAndJoin()
    }

    @Test
    fun aFailingStreamIsRetried() = runBlocking {
        val seen = CopyOnWriteArrayList<CoreState>()
        val boom = flow<CoreStatus> { throw IOException("socket closed") }
        val live = MutableSharedFlow<CoreStatus>(extraBufferCapacity = 8)
        val api = Api(st(CoreState.CORE_STATE_IDLE), listOf(boom, live))
        val job = launch { CoreWatcher.keep(api, { seen += it.state }, { true }, retryMs = 10) }
        withTimeout(2_000) { while (api.opened < 2) delay(5) }
        live.emit(st(CoreState.CORE_STATE_CONNECTED))
        withTimeout(2_000) { while (CoreState.CORE_STATE_CONNECTED !in seen) delay(5) }
        job.cancelAndJoin()
    }

    // L-9: a stream that ends at once every time must not make the service re-seed and publish every couple of seconds.
    @Test
    fun aStreamThatEndsAtOnceIsRetriedWithAGrowingPause() = runBlocking {
        val pauses = CopyOnWriteArrayList<Long>()
        var current = true
        val api = Api(st(CoreState.CORE_STATE_IDLE), listOf(flow { /* ends at once */ }))
        CoreWatcher.keep(api, { }, { current }, retryMs = 10, maxRetryMs = 50, sleep = { pauses += it; if (pauses.size == 6) current = false })
        assertEquals(listOf(10L, 20L, 40L, 50L, 50L, 50L), pauses.toList())
    }

    @Test
    fun anyEventResetsThePause() = runBlocking {
        val pauses = CopyOnWriteArrayList<Long>()
        var current = true
        val one = flow { emit(st(CoreState.CORE_STATE_CONNECTED)) }
        val idle = flow<CoreStatus> { }
        val api = Api(st(CoreState.CORE_STATE_IDLE), listOf(idle, idle, one, idle))
        CoreWatcher.keep(api, { }, { current }, retryMs = 10, maxRetryMs = 100, sleep = { pauses += it; if (pauses.size == 4) current = false })
        assertEquals(listOf(10L, 20L, 10L, 10L), pauses.toList())
    }

    @Test
    fun itStopsWhenThisCoreIsNoLongerTheOneInUse() = runBlocking {
        var current = true
        val api = Api(st(CoreState.CORE_STATE_IDLE), listOf(flow { }))
        val job = launch { CoreWatcher.keep(api, { }, { current }, retryMs = 10) }
        withTimeout(2_000) { while (api.opened < 1) delay(5) }
        current = false
        withTimeout(2_000) { job.join() }
        assertTrue(job.isCompleted)
    }
}

// ---- TunHandoff: the descriptor handed to the core

class TunHandoffTest {
    private class Fd(val number: Int, val dupFails: Boolean = false) : DupableFd {
        var closed = 0
        var detached = false
        var dup: Fd? = null
        override fun dupFd(): DupableFd {
            if (dupFails) throw IOException("dup failed")
            return Fd(number + 100).also { dup = it }
        }
        override fun detach(): Int {
            detached = true
            return number
        }
        override fun close() { closed++ }
    }

    private val holder = TunHolder<Fd>()
    private val handoff = TunHandoff(holder)

    @Test
    fun theCoreGetsAnIndependentDescriptorAndKotlinKeepsTheOriginal() {
        val built = Fd(7)
        var generation = -1
        val number = handoff.hand(built) { generation = it }
        assertEquals(107, number)
        assertTrue(built.dup!!.detached)
        assertEquals("the original stays open: it keeps the interface alive", 0, built.closed)
        assertTrue(holder.isHeld())
        assertEquals(holder.currentGeneration(), generation)
    }

    @Test
    fun aFailedDupClosesTheBuiltInterface() {
        val built = Fd(7, dupFails = true)
        assertThrows(IOException::class.java) { handoff.hand(built) { } }
        assertEquals(1, built.closed)
        assertFalse(holder.isHeld())
    }

    // L8: the detached descriptor leaked when a step after the dup threw.
    @Test
    fun aFailureAfterTheReplaceClosesTheDuplicateAndNeverDetachesIt() {
        val built = Fd(7)
        assertThrows(IllegalStateException::class.java) { handoff.hand(built) { throw IllegalStateException("underlying") } }
        assertEquals(1, built.dup!!.closed)
        assertFalse(built.dup!!.detached)
        assertTrue("the interface stays held; the caller's release decision applies", holder.isHeld())
    }

    @Test
    fun aNewerInterfaceReplacesAndClosesTheOlderOriginal() {
        val first = Fd(1)
        val second = Fd(2)
        handoff.hand(first) { }
        handoff.hand(second) { }
        assertEquals(1, first.closed)
        assertEquals(0, second.closed)
    }
}

// ---- BusyHold, AutoConnectSetting, SwitchRetry

class BusyHoldTest {
    private val hold = BusyHold()

    @Test
    fun busyUntilTheDeadlineOrTheEnd() {
        hold.begin(0, "restore", 5_000)
        assertTrue(hold.isBusy(4_999))
        assertFalse("a hold the UI forgot ends by itself", hold.isBusy(5_000))
        hold.begin(0, "restore", 5_000)
        hold.end("restore")
        assertFalse(hold.isBusy(1))
    }

    @Test
    fun theHardCapClampsAnyTimeout() {
        val deadline = hold.begin(0, "x", Long.MAX_VALUE / 2)
        assertEquals(BusyHold.MAX_HOLD_MS, deadline)
        assertEquals(1L, hold.begin(0, "y", -5))
    }

    // L-5: the wake-up must follow the earliest deadline of ANY hold and re-arm for the next one.
    @Test
    fun theWakeUpFollowsTheEarliestHoldAndRearmsForTheNext() {
        hold.begin(0, "a", 10_000)
        hold.begin(0, "b", 3_000)
        assertEquals(3_001L, hold.delayUntilNext(0))
        assertEquals("b is over, a is next", 5_001L, hold.delayUntilNext(5_000))
        assertEquals(null, hold.delayUntilNext(10_000))
    }

    @Test
    fun clearDropsEveryHold() {
        hold.begin(0, "a", 10_000)
        hold.begin(0, "b", 10_000)
        hold.clear()
        assertFalse(hold.isBusy(1))
        assertEquals(null, hold.delayUntilNext(1))
    }

    @Test
    fun severalHoldsAreIndependent() {
        hold.begin(0, "a", 1_000)
        hold.begin(0, "b", 3_000)
        assertEquals(1_000L, hold.nextDeadline(0))
        assertTrue(hold.isBusy(2_000))
        assertEquals(3_000L, hold.nextDeadline(2_000))
        hold.end("b")
        assertFalse(hold.isBusy(2_000))
    }
}

class AutoConnectSettingTest {
    private class Port(var state: BootSwitch?, var failOn: BootSwitch? = null) : BootSwitchPort {
        override fun current() = state
        override fun set(target: BootSwitch) {
            if (target == failOn) throw SecurityException("refused")
            state = target
        }
    }

    private var stored = false
    private var writeOk = true
    private fun setting(port: Port) = AutoConnectSetting({ stored }, { v -> if (writeOk) stored = v; writeOk }, BootReceiverSwitch(port))

    @Test
    fun enablingWritesAndAlignsTheReceiver() {
        val port = Port(BootSwitch.DISABLED)
        assertEquals(WriteResult.SAVED, setting(port).apply(true))
        assertTrue(stored)
        assertEquals(BootSwitch.DEFAULT, port.state)
    }

    // E-21: the switch must not show "on" when the component state was not written.
    @Test
    fun aFailedEnableRollsTheSettingBackAndSaysSo() {
        val port = Port(BootSwitch.DISABLED, failOn = BootSwitch.DEFAULT)
        assertEquals(WriteResult.RECEIVER_FAILED, setting(port).apply(true))
        assertFalse("the stored value is the old one", stored)
    }

    @Test
    fun aPreferenceThatDidNotReachStorageIsReported() {
        writeOk = false
        assertEquals(WriteResult.NOT_SAVED, setting(Port(BootSwitch.DEFAULT)).apply(false))
    }

    @Test
    fun aFailedDisableIsStillOffAndTheReceiverStaysOn() {
        stored = true
        val port = Port(BootSwitch.DEFAULT, failOn = BootSwitch.DISABLED)
        assertEquals(WriteResult.SAVED, setting(port).apply(false))
        assertFalse(stored)
        assertEquals(BootSwitch.DEFAULT, port.state)
    }

    @Test
    fun reconcileAlignsTheReceiverWithTheStoredPreference() {
        stored = true
        val port = Port(BootSwitch.DISABLED)
        assertEquals(SwitchOutcome.CHANGED, setting(port).reconcile())
        assertEquals(BootSwitch.DEFAULT, port.state)
    }

    @Test
    fun onlyAFailedEnableIsRetriedAndTheRetriesRunOut() {
        assertEquals(2_000L, SwitchRetry.delayMs(SwitchOutcome.FAILED_STILL_DISABLED, 0))
        assertEquals(60_000L, SwitchRetry.delayMs(SwitchOutcome.FAILED_STILL_DISABLED, 2))
        assertEquals(null, SwitchRetry.delayMs(SwitchOutcome.FAILED_STILL_DISABLED, 3))
        assertEquals(null, SwitchRetry.delayMs(SwitchOutcome.FAILED_STAYS_ENABLED, 0))
        assertEquals(null, SwitchRetry.delayMs(SwitchOutcome.UNCHANGED, 0))
    }
}
