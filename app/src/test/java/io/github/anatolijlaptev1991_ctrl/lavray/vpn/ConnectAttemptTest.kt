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

internal class FakeCoreApi(
    var status: CoreStatus,
    var onConnect: suspend (FakeCoreApi) -> ConnectReply = { ConnectReply(null) },
    private val last: Long? = 7,
) : CoreApi {
    val bus = MutableSharedFlow<CoreStatus>(extraBufferCapacity = 64)
    var connects = 0
    var connectedWith: Long? = null
    var statusCalls = 0
    override suspend fun status(): CoreStatus {
        statusCalls++
        return status
    }
    override suspend fun connect(profileId: Long): ConnectReply {
        connects++
        connectedWith = profileId
        return onConnect(this)
    }
    override suspend fun lastProfileId() = last
    override fun events(): Flow<CoreStatus> = bus
}

class ConnectAttemptTest {
    private fun st(state: CoreState, error: String? = null, profile: Long? = null) =
        CoreStatus(state = state, lastError = error, activeProfileId = profile)

    private fun connected(profile: Long? = null) = ConnectReply(st(CoreState.CORE_STATE_CONNECTED, profile = profile))

    @Test
    fun connectsAndReturnsTheConnectedStatusAndWarnings() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_IDLE), onConnect = { ConnectReply(st(CoreState.CORE_STATE_CONNECTED), listOf("w")) })
        val result = ConnectAttempt.run(api, 5)
        assertEquals(CoreState.CORE_STATE_CONNECTED, result.status.state)
        assertEquals(listOf("w"), result.warnings)
        assertEquals(5L, api.connectedWith)
    }

    @Test
    fun usesTheLastServerWhenNoneIsGiven() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_IDLE), onConnect = { connected() })
        ConnectAttempt.run(api, null)
        assertEquals(7L, api.connectedWith)
    }

    @Test
    fun noServerIsReported() {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_IDLE), last = null)
        assertThrows(NoServerException::class.java) { runBlocking { ConnectAttempt.run(api, null) } }
    }

    @Test
    fun aCoreAlreadyConnectedToTheRequestedServerIsSuccessWithoutAnotherConnect() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_CONNECTED, profile = 5))
        assertEquals(CoreState.CORE_STATE_CONNECTED, ConnectAttempt.run(api, 5).status.state)
        assertEquals(0, api.connects)
    }

    // L2: connected to A, asked for B: success for A would leave the server the person chose unconnected.
    @Test
    fun aCoreConnectedToAnotherServerIsAskedToConnectTheRequestedOne() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_CONNECTED, profile = 1), onConnect = { connected(profile = 2) })
        val result = ConnectAttempt.run(api, 2)
        assertEquals(1, api.connects)
        assertEquals(2L, api.connectedWith)
        assertEquals(2L, result.status.activeProfileId)
    }

    @Test
    fun noServerGivenAndTheCoreAlreadyConnectedKeepsWhatItHas() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_CONNECTED, profile = 1))
        ConnectAttempt.run(api, null)
        assertEquals(0, api.connects)
    }

    @Test
    fun aCoreThatIsStillConnectingIsWaitedForNotTrusted() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_CONNECTING, profile = 5))
        val done = CompletableDeferred<ConnectResult>()
        val job = launch { done.complete(ConnectAttempt.run(api, 5)) }
        // Not finished while the core is merely connecting.
        assertNull(withTimeoutOrNull(150) { done.await() })
        assertFalse(done.isCompleted)
        api.bus.emit(st(CoreState.CORE_STATE_CONNECTED))
        assertEquals(CoreState.CORE_STATE_CONNECTED, done.await().status.state)
        assertEquals(0, api.connects)
        job.join()
    }

    @Test
    fun connectingResponseWaitsForTheStream() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_IDLE), onConnect = { ConnectReply(st(CoreState.CORE_STATE_CONNECTING)) })
        val done = CompletableDeferred<ConnectResult>()
        launch { done.complete(ConnectAttempt.run(api, 5)) }
        assertNull(withTimeoutOrNull(150) { done.await() })
        api.bus.emit(st(CoreState.CORE_STATE_CONNECTED))
        assertEquals(CoreState.CORE_STATE_CONNECTED, done.await().status.state)
    }

    @Test
    fun coreErrorBeforeTheFirstConnectedFailsTheAttempt() {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_CONNECTING))
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
        val api = FakeCoreApi(st(CoreState.CORE_STATE_IDLE), onConnect = { ConnectReply(st(CoreState.CORE_STATE_ERROR, "boom")) })
        assertThrows(CoreConnectException::class.java) { runBlocking { ConnectAttempt.run(api, 5) } }
    }

    @Test
    fun staleErrorFromThePreviousRunIsNotTheOutcome() = runBlocking {
        val api = FakeCoreApi(st(CoreState.CORE_STATE_ERROR, "old"))
        api.onConnect = { a ->
            a.bus.emit(st(CoreState.CORE_STATE_CONNECTING))
            ConnectReply(st(CoreState.CORE_STATE_CONNECTING))
        }
        val done = CompletableDeferred<ConnectResult>()
        launch { done.complete(ConnectAttempt.run(api, 5)) }
        kotlinx.coroutines.delay(50)
        api.bus.emit(st(CoreState.CORE_STATE_CONNECTED))
        assertEquals(CoreState.CORE_STATE_CONNECTED, done.await().status.state)
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

    @Test
    fun theCoresReasonTravelsWithTheFailure() {
        val f = ConnectAttempt.classify(ApiException(ConnectCode.FAILED_PRECONDITION, "no profile", reason = "no_server"))
        assertTrue(f.permanent)
        assertEquals("no_server", f.reason)
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
