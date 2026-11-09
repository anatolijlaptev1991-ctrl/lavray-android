// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import android.content.ContentResolver
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.nio.ByteBuffer
import java.nio.charset.CharacterCodingException
import java.nio.charset.CodingErrorAction

/**
 * Preset files through the system file picker (Storage Access Framework): the app never asks for a storage
 * permission and sees only the file the person chose. The 1 MiB limit is the core's own; a bigger file is
 * refused here, before it is read whole.
 */
object PresetFiles {
    const val MAX_BYTES = 1 shl 20
    private const val CHUNK = 8 * 1024
    private val BOM = Char(0xFEFF).toString()
    const val MIME_JSON = "application/json"

    /** Types offered when opening: many file managers label a .json file as plain text or as a binary. */
    val OPEN_TYPES: Array<String> = arrayOf(MIME_JSON, "text/plain", "application/octet-stream")

    /** The text of the stream as UTF-8, or null when it is larger than [max] bytes or not valid UTF-8. */
    fun readLimited(input: InputStream, max: Int = MAX_BYTES): String? {
        val out = ByteArrayOutputStream()
        val buffer = ByteArray(CHUNK)
        while (true) {
            val n = input.read(buffer)
            if (n < 0) break
            if (out.size() + n > max) return null
            out.write(buffer, 0, n)
        }
        return decodeUtf8(out.toByteArray())
    }

    private fun decodeUtf8(bytes: ByteArray): String? {
        val decoder = Charsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT)
        val text = try {
            decoder.decode(ByteBuffer.wrap(bytes)).toString()
        } catch (_: CharacterCodingException) {
            return null
        }
        return text.removePrefix(BOM)
    }

    /** Reads the chosen file off the main thread; null when it cannot be opened, is too large or is not text. */
    suspend fun read(resolver: ContentResolver, uri: Uri): String? = withContext(Dispatchers.IO) {
        try {
            resolver.openInputStream(uri)?.use { readLimited(it) }
        } catch (_: IOException) {
            null
        } catch (_: SecurityException) {
            null
        }
    }

    /** Writes the text to the chosen file, replacing what was there. False when it could not be written. */
    suspend fun write(resolver: ContentResolver, uri: Uri, text: String): Boolean = withContext(Dispatchers.IO) {
        try {
            val stream = resolver.openOutputStream(uri, "wt") ?: return@withContext false
            stream.use { it.write(text.toByteArray(Charsets.UTF_8)) }
            true
        } catch (_: IOException) {
            false
        } catch (_: SecurityException) {
            false
        }
    }

    /** File name offered in the save dialog: the core's suggestion made safe, with the .json extension. */
    fun exportFileName(suggested: String): String {
        val base = suggested.trim().removeSuffix(".json").replace(Regex("[^A-Za-z0-9._-]"), "-").trim('-', '.')
        return (base.ifEmpty { "preset" }) + ".json"
    }
}
