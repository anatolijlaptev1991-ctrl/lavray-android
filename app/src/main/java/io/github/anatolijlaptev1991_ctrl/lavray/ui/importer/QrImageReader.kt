// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException

/** Result of looking for a QR code in a picked image. */
sealed interface QrImageResult {
    data class Found(val text: String) : QrImageResult
    data object NotFound : QrImageResult
    data object Unreadable : QrImageResult
}

/** Reads a QR code from an image the user picked. Runs on the IO dispatcher; the bitmap never leaves this function. */
object QrImageReader {
    private const val MAX_SIDE = 2048
    private val scales = intArrayOf(1, 2, 4)

    suspend fun read(resolver: ContentResolver, uri: Uri): QrImageResult = withContext(Dispatchers.IO) {
        try {
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
            if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return@withContext QrImageResult.Unreadable
            val sample = sampleSize(bounds.outWidth, bounds.outHeight)
            for (extra in scales) {
                val text = decodeAt(resolver, uri, sample * extra) ?: continue
                return@withContext QrImageResult.Found(text)
            }
            QrImageResult.NotFound
        } catch (_: IOException) {
            QrImageResult.Unreadable
        } catch (_: SecurityException) {
            QrImageResult.Unreadable
        } catch (_: OutOfMemoryError) {
            QrImageResult.Unreadable
        }
    }

    internal fun sampleSize(width: Int, height: Int): Int {
        var sample = 1
        while (maxOf(width, height) / sample > MAX_SIDE) sample *= 2
        return sample
    }

    private fun decodeAt(resolver: ContentResolver, uri: Uri, sample: Int): String? {
        val opts = BitmapFactory.Options().apply {
            inSampleSize = sample
            inPreferredConfig = Bitmap.Config.ARGB_8888
        }
        val bitmap = resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, opts) } ?: return null
        return try {
            val pixels = IntArray(bitmap.width * bitmap.height)
            bitmap.getPixels(pixels, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
            QrDecoder.decodePixels(pixels, bitmap.width, bitmap.height)
        } finally {
            bitmap.recycle()
        }
    }
}
