// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.core

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.IOException

/** What went wrong, in terms the UI can word for a person. The wording lives in [errorText]. */
enum class ErrorKind { CoreUnavailable, NotImplemented, Precondition, InvalidInput, NotFound, Generic }

/** Outcome of one core call. Never carries a raw exception text to the screen. */
sealed interface CoreResult<out T> {
    data class Ok<T>(val value: T) : CoreResult<T>
    data class Fail(val kind: ErrorKind) : CoreResult<Nothing>
}

fun Throwable.errorKind(): ErrorKind = when {
    this is ApiException -> when (code) {
        ConnectCode.UNAVAILABLE, ConnectCode.UNAUTHENTICATED, ConnectCode.DEADLINE_EXCEEDED -> ErrorKind.CoreUnavailable
        ConnectCode.UNIMPLEMENTED -> ErrorKind.NotImplemented
        ConnectCode.FAILED_PRECONDITION -> ErrorKind.Precondition
        ConnectCode.INVALID_ARGUMENT, ConnectCode.OUT_OF_RANGE -> ErrorKind.InvalidInput
        ConnectCode.NOT_FOUND -> ErrorKind.NotFound
        else -> ErrorKind.Generic
    }
    this is IOException -> ErrorKind.CoreUnavailable
    else -> ErrorKind.Generic
}

/** Source of the endpoint; the VPN controller in the app, a fake in tests. */
fun interface EndpointSource {
    suspend fun endpoint(): CoreEndpoint
}

/**
 * The UI process's handle on the core API. The client is built once from the endpoint the :vpn
 * process reports and dropped when the core stops answering, so the next call asks the service
 * again (the core may have restarted with a new token). All calls run on the client's IO dispatcher.
 */
class CoreSession(private val source: EndpointSource, private val factory: (CoreEndpoint) -> LavrayClient = ::LavrayClient) {
    private val lock = Mutex()
    @Volatile private var cached: LavrayClient? = null

    suspend fun client(): LavrayClient = cached ?: lock.withLock {
        cached ?: factory(endpointOrUnavailable()).also { cached = it }
    }

    // The service reports a core that failed to start as an exception; to the UI that is simply "core unavailable".
    private suspend fun endpointOrUnavailable(): CoreEndpoint = try {
        source.endpoint()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        throw ApiException(ConnectCode.UNAVAILABLE, "core endpoint is not available", cause = e)
    }

    private fun drop(error: Throwable) {
        val unreachable = error is ApiException &&
            (error.code == ConnectCode.UNAVAILABLE || error.code == ConnectCode.UNAUTHENTICATED)
        if (unreachable) cached = null
    }

    /** One call; failures become a [CoreResult.Fail], cancellation passes through. */
    suspend fun <T> call(block: suspend LavrayClient.() -> T): CoreResult<T> = try {
        CoreResult.Ok(client().block())
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        drop(e)
        CoreResult.Fail(e.errorKind())
    }

    /** A server stream. It lives exactly as long as it is collected: cancelling the collector aborts the socket. */
    fun <T> stream(block: LavrayClient.() -> Flow<T>): Flow<T> = flow {
        val c = try {
            client()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            throw StreamFailure(e.errorKind(), e)
        }
        try {
            emitAll(c.block())
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            drop(e)
            throw StreamFailure(e.errorKind(), e)
        }
    }
}

/** Thrown by [CoreSession.stream]; the kind is what the UI shows. */
class StreamFailure(val kind: ErrorKind, cause: Throwable) : IOException("core stream failed", cause)
