// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.content.Context
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import lavray.core.mobile.Mobile
import lavray.core.mobile.Options
import lavray.core.mobile.Platform
import java.io.File
import java.security.SecureRandom

class CoreStartException(message: String, cause: Throwable? = null) : IllegalStateException(message, cause)

/**
 * Who owns the one Go core of this process. A service that is destroyed stops its core on a thread of its own;
 * a service created meanwhile starts a new one. Without an owner the late stop would shut down the NEW core, and
 * the new start could meet the old one still running ("already started"). The owner is the only one whose stop
 * counts, and a new owner stops the previous core first.
 */
class CoreLifecycle {
    private val lock = Any()
    private var owner: Any? = null
    private var ownerStop: (() -> Unit)? = null

    fun start(who: Any, stopNative: () -> Unit, startNative: () -> Unit) = synchronized(lock) {
        val previous = ownerStop
        if (owner != null && owner !== who && previous != null) {
            runCatching(previous)
            owner = null
            ownerStop = null
        }
        startNative()
        owner = who
        ownerStop = stopNative
    }

    /** Stops the core only if [who] still owns it. */
    fun stop(who: Any, stopNative: () -> Unit) = synchronized(lock) {
        if (owner !== who) return@synchronized
        owner = null
        ownerStop = null
        stopNative()
    }
}

/**
 * Owns the Go core inside the :vpn process. Lazy: nothing is loaded until
 * [ensureStarted], so the process that only shows the UI never pays for it.
 * The bearer token is generated per start, lives in memory only, and reaches the
 * UI through the binder; it is never logged or written to disk.
 */
internal class CoreHost(
    private val context: Context,
    private val platform: Platform,
    private val lifecycle: CoreLifecycle = PROCESS_LIFECYCLE,
) {
    private val lock = Any()
    private var endpoint: CoreEndpoint? = null

    /** Starts the core if needed and returns where it listens. Blocks until its API answers. */
    fun ensureStarted(): CoreEndpoint = synchronized(lock) {
        endpoint ?: start().also { endpoint = it }
    }

    fun isRunning(): Boolean = synchronized(lock) { endpoint != null }

    /** Graceful, idempotent shutdown. */
    fun stop() {
        synchronized(lock) {
            if (endpoint == null) return
            lifecycle.stop(this) { runCatching { Mobile.stop() } }
            endpoint = null
        }
    }

    private fun start(): CoreEndpoint {
        // no_backup is excluded from device backup and private to the app (0700 owner-only).
        val root = context.noBackupFilesDir
        val dataDir = File(root, "data").apply { mkdirs() }
        val socket = File(root, SOCKET_NAME)
        socket.delete()
        val token = newToken()
        val options = Options().apply {
            setDataDir(dataDir.absolutePath)
            setTempDir(context.cacheDir.absolutePath)
            setSocketPath(socket.absolutePath)
            setAuthToken(token)
            setMemoryLimitMB(0)
            // Go has no zoneinfo database on Android; the platform zone id is the only source.
            setTimeZone(java.util.TimeZone.getDefault().id)
        }
        try {
            lifecycle.start(this, { Mobile.stop() }) { Mobile.start(options, platform) }
        } catch (e: Exception) {
            throw CoreStartException("core failed to start: ${e.message}", e)
        } catch (e: LinkageError) {
            // libgojni.so missing or wrong for this ABI: the same "core failed to start" path, not a process crash.
            throw CoreStartException("core failed to start: ${e.message}", e)
        }
        return CoreEndpoint(socket.absolutePath, token)
    }

    private fun newToken(): String {
        val bytes = ByteArray(TOKEN_BYTES)
        SecureRandom().nextBytes(bytes)
        return bytes.joinToString("") { "%02x".format(it) }
    }

    private companion object {
        val PROCESS_LIFECYCLE = CoreLifecycle()
        const val SOCKET_NAME = "lavray.sock"
        const val TOKEN_BYTES = 32
    }
}
