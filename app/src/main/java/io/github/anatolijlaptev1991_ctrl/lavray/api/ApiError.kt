// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import kotlinx.serialization.Serializable
import java.io.IOException

/** Connect error codes as they appear on the wire (lower snake case). */
enum class ConnectCode(val wire: String) {
    CANCELED("canceled"),
    UNKNOWN("unknown"),
    INVALID_ARGUMENT("invalid_argument"),
    DEADLINE_EXCEEDED("deadline_exceeded"),
    NOT_FOUND("not_found"),
    ALREADY_EXISTS("already_exists"),
    PERMISSION_DENIED("permission_denied"),
    RESOURCE_EXHAUSTED("resource_exhausted"),
    FAILED_PRECONDITION("failed_precondition"),
    ABORTED("aborted"),
    OUT_OF_RANGE("out_of_range"),
    UNIMPLEMENTED("unimplemented"),
    INTERNAL("internal"),
    UNAVAILABLE("unavailable"),
    DATA_LOSS("data_loss"),
    UNAUTHENTICATED("unauthenticated");

    companion object {
        fun fromWire(value: String?): ConnectCode = entries.firstOrNull { it.wire == value } ?: UNKNOWN

        /** Connect protocol mapping of an HTTP status when no error body is usable. */
        fun fromHttpStatus(status: Int): ConnectCode = when (status) {
            400 -> INTERNAL
            401 -> UNAUTHENTICATED
            403 -> PERMISSION_DENIED
            404 -> UNIMPLEMENTED
            429, 502, 503, 504 -> UNAVAILABLE
            else -> UNKNOWN
        }
    }
}

/** Failure of a call: a Connect error from the core, or a transport problem (code UNAVAILABLE). */
class ApiException(
    val code: ConnectCode,
    message: String,
    val httpStatus: Int = 0,
    cause: Throwable? = null,
    /** ErrorInfo.reason from the core (for example backup_wrong_password), when it sent one; screens choose their text by it. */
    val reason: String? = null,
) : IOException(message, cause) {
    /** The request never reached the core (the socket could not be opened or the token was refused), so repeating it is safe. */
    val neverReachedCore: Boolean
        get() = code == ConnectCode.UNAUTHENTICATED || (code == ConnectCode.UNAVAILABLE && message == NOT_REACHABLE)

    companion object {
        const val NOT_REACHABLE = "core socket is not reachable"
    }
}

@Serializable
internal data class ConnectErrorBody(val code: String = "", val message: String = "", val details: List<ErrorDetailBody> = emptyList())

/** One entry of the Connect error details: a type name and the base64 of the protobuf message. */
@Serializable
internal data class ErrorDetailBody(val type: String = "", val value: String = "")

@Serializable
internal data class EndStreamBody(val error: ConnectErrorBody? = null)

internal fun errorFromBody(bytes: ByteArray, httpStatus: Int): ApiException {
    val parsed = runCatching { ProtoJson.decodeFromString(ConnectErrorBody.serializer(), String(bytes, Charsets.UTF_8)) }.getOrNull()
    return if (parsed != null && parsed.code.isNotEmpty()) {
        ApiException(ConnectCode.fromWire(parsed.code), parsed.message, httpStatus, reason = ErrorInfoReader.reason(parsed.details))
    } else {
        ApiException(ConnectCode.fromHttpStatus(httpStatus), "HTTP $httpStatus", httpStatus)
    }
}

/** Returns the error carried by an end-of-stream frame, or null for a clean end. */
internal fun errorFromEndStream(payload: ByteArray): ApiException? {
    if (payload.isEmpty()) return null
    val body = runCatching { ProtoJson.decodeFromString(EndStreamBody.serializer(), String(payload, Charsets.UTF_8)) }
        .getOrElse { throw ApiException(ConnectCode.INTERNAL, "malformed end-of-stream frame", cause = it) }
    val err = body.error ?: return null
    return ApiException(ConnectCode.fromWire(err.code), err.message, reason = ErrorInfoReader.reason(err.details))
}
