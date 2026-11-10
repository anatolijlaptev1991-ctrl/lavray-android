// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectAttemptTest {
    private fun st(state: CoreState, error: String? = null) = CoreStatus(state = state, lastError = error)

    private class FakeApi(
        var status: CoreStatus,
        var onConnect: suspend (FakeApi) -> CoreStatus? = { null },
        private val last: Long? = 7,
    ) : CoreApi {
        val bus = MutableSharedFlow<CoreStatus>(extraBufferCapacity = 64)
        var connects = 0
        var connectedWith: Long? = null
        override suspend fun status() = status
        override suspend fun connect(profileId: Long): CoreStatus? {
            connects++
            connectedWith = profileId
            return onConnect(this)
        }
        override suspend fun lastProfileId() = last
        override fun events(): Flow<CoreStatus> = bus
    }

    @Test
    fun connectsAndReturnsTheConnectedStatus() = runBlocking {
        val api = FakeApi(st(CoreState.CORE_STATE_IDLE), onConnect = { st(CoreState.CORE_STATE_CONNECTED) })
        val result = ConnectAttempt.run(api, 5)
        assertEquals(CoreState.CORE_STATE_CONNECTED, result.state)
        assertEquals(5L, api.connectedWith)
    }

    @Test
    fun usesTheLastServerWhenNoneIsGiven() = runBlocking {
        val api = FakeApi(st(CoreState.CORE_STATE_IDLE), onConnect = { st(CoreState.CORE_STATE_CONNECTED) })
        ConnectAttempt.run(api, null)
        assertEquals(7L, api.connectedWith)
    }

    @Test
    fun noServerIsReported() {
        val api = FakeApi(st(CoreState.CORE_STATE_IDLE), last = null)
        assertThrows(NoServerException::class.java) { runBlocking { ConnectAttempt.run(api, null) } }
    }

    @Test
    fun aCoreThatIsStillConnectingIsWaitedForNotTrusted() = runBlocking {
        val api = FakeApi(st(CoreState.CORE_STATE_CONNECTING))
        val done = CompletableDeferred<CoreStatus>()
        val job = launch { done.complete(ConnectAttempt.run(api, 5)) }
        // Not finished while the core is merely connecting.
        assertNull(withTimeoutOrNull(150) { done.await() })
        assertFalse(done.isCompleted)
        api.bus.emit(st(CoreState.CORE_STATE_CONNECTED))
        assertEquals(CoreState.CORE_STATE_CONNECTED, done.await().state)
        assertEquals(0, api.connects)
        job.join()
    }

    @Test
    fun connectingResponseWaitsForTheStream() = runBlocking {
        val api = FakeApi(st(CoreState.CORE_STATE_IDLE), onConnect = { st(CoreState.CORE_STATE_CONNECTING) })
        val done = CompletableDeferred<CoreStatus>()
        launch { done.complete(ConnectAttempt.run(api, 5)) }
        assertNull(withTimeoutOrNull(150) { done.await() })
        api.bus.emit(st(CoreState.CORE_STATE_CONNECTED))
        assertEquals(CoreState.CORE_STATE_CONNECTED, done.await().state)
    }

    @Test
    fun coreErrorBeforeTheFirstConnectedFailsTheAttempt() {
        val api = FakeApi(st(CoreState.CORE_STATE_CONNECTING))
        val e = assertThrows(CoreConnectException::class.java) {
            runBlocking {
                launch {
                    kotlinx.coroutines.delay(100)
                    api.bus.emit(st(CoreState.CORE_STATE_ERROR, "dial failed"))
                }
                ConnectAttempt.run(api, 5)
            }
        }
        assertEquals("dial failed", e.message)
        assertFalse(ConnectAttempt.classify(e).permanent)
    }

    @Test
    fun errorResponseFromConnectFails() {
        val api = FakeApi(st(CoreState.CORE_STATE_IDLE), onConnect = { st(CoreState.CORE_STATE_ERROR, "boom") })
        assertThrows(CoreConnectException::class.java) { runBlocking { ConnectAttempt.run(api, 5) } }
    }

    @Test
    fun staleErrorFromThePreviousRunIsNotTheOutcome() = runBlocking {
        val api = FakeApi(st(CoreState.CORE_STATE_ERROR, "old"))
        api.onConnect = { a ->
            a.bus.emit(st(CoreState.CORE_STATE_CONNECTING))
            st(CoreState.CORE_STATE_CONNECTING)
        }
        val done = CompletableDeferred<CoreStatus>()
        launch { done.complete(ConnectAttempt.run(api, 5)) }
        kotlinx.coroutines.delay(50)
        api.bus.emit(st(CoreState.CORE_STATE_CONNECTED))
        assertEquals(CoreState.CORE_STATE_CONNECTED, done.await().state)
    }

    @Test
    fun classification() {
        val tun = TunFailure(FailureKind.APPS_NONE, "no apps")
        val f = ConnectAttempt.classify(ApiException(ConnectCode.UNKNOWN, "x"), tun)
        assertTrue(f.permanent)
        assertEquals(FailureKind.APPS_NONE, f.kind)
        assertEquals(FailureKind.NO_SERVER, ConnectAttempt.classify(NoServerException()).kind)
        assertEquals(FailureKind.CORE_START, ConnectAttempt.classify(CoreStartException("x")).kind)
        assertFalse(ConnectAttempt.classify(ApiException(ConnectCode.UNAVAILABLE, "x")).permanent)
    }
}

class AttemptGateTest {
    @Test
    fun sameProfileInFlightIsKept() {
        assertEquals(AttemptDecision.KEEP, AttemptGate.decide(true, 5, 5))
        assertEquals(AttemptDecision.KEEP, AttemptGate.decide(true, null, null))
    }

    @Test
    fun otherProfileReplacesIt() {
        assertEquals(AttemptDecision.REPLACE, AttemptGate.decide(true, 5, 6))
        assertEquals(AttemptDecision.REPLACE, AttemptGate.decide(true, null, 6))
    }

    @Test
    fun nothingRunningStarts() {
        assertEquals(AttemptDecision.START, AttemptGate.decide(false, 5, 5))
    }
}
