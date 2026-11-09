// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import android.net.LocalSocket
import android.net.LocalSocketAddress
import java.io.BufferedInputStream
import java.io.Closeable
import java.io.InputStream
import java.io.OutputStream

/** One open connection to the core. [abort] must unblock a thread parked in a read. */
interface Connection : Closeable {
    val input: InputStream
    val output: OutputStream
    fun abort()
}

/** Opens connections; a seam so framing and client logic run on the JVM without sockets. */
fun interface Transport {
    @Throws(java.io.IOException::class)
    fun open(): Connection
}

/** Unix-domain socket in the app's private directory (filesystem namespace). */
class LocalSocketTransport(private val socketPath: String) : Transport {
    override fun open(): Connection {
        val socket = LocalSocket()
        try {
            socket.connect(LocalSocketAddress(socketPath, LocalSocketAddress.Namespace.FILESYSTEM))
        } catch (e: java.io.IOException) {
            runCatching { socket.close() }
            throw e
        }
        return LocalSocketConnection(socket)
    }
}

private class LocalSocketConnection(private val socket: LocalSocket) : Connection {
    override val input: InputStream = BufferedInputStream(socket.inputStream, 8192)
    override val output: OutputStream = socket.outputStream

    // close() alone does not wake a reader blocked in the kernel; shutdown does.
    override fun abort() {
        runCatching { socket.shutdownInput() }
        runCatching { socket.shutdownOutput() }
        runCatching { socket.close() }
    }

    override fun close() {
        runCatching { socket.close() }
    }
}
