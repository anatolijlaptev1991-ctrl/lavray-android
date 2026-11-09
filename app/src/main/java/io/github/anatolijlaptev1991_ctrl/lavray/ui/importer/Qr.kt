// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import androidx.compose.runtime.Immutable
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.EncodeHintType
import com.google.zxing.InvertedLuminanceSource
import com.google.zxing.LuminanceSource
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.WriterException
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import com.google.zxing.qrcode.encoder.Encoder

/** A QR code as a square grid of modules; drawn as vector rectangles, never as a bitmap. */
@Immutable
class QrMatrix(val size: Int, private val bits: BooleanArray) {
    fun isDark(x: Int, y: Int): Boolean = bits[y * size + x]
}

/** QR generation with zxing's encoder only (error correction M). Returns null when the text does not fit. */
object QrEncoder {
    private val hints = mapOf<EncodeHintType, Any>(EncodeHintType.CHARACTER_SET to "UTF-8")

    fun encode(text: String): QrMatrix? {
        if (text.isEmpty()) return null
        val code = try {
            Encoder.encode(text, ErrorCorrectionLevel.M, hints)
        } catch (_: WriterException) {
            return null
        }
        val m = code.matrix
        val n = m.width
        val bits = BooleanArray(n * n)
        for (y in 0 until n) for (x in 0 until n) bits[y * n + x] = m.get(x, y).toInt() == 1
        return QrMatrix(n, bits)
    }
}

/**
 * QR reading with zxing's QR reader only. Tries the normal image, then the inverted one (light code on
 * a dark background), and for stills a global-histogram binarizer too; returns null when nothing is found.
 */
object QrDecoder {
    private val hints = mapOf<DecodeHintType, Any>(
        DecodeHintType.TRY_HARDER to true,
        DecodeHintType.CHARACTER_SET to "UTF-8",
    )

    /** One camera frame: the Y (luminance) plane with its row stride. */
    fun decodeYPlane(y: ByteArray, rowStride: Int, width: Int, height: Int): String? =
        decode(PlanarYUVLuminanceSource(y, rowStride, height, 0, 0, width, height, false), still = false)

    /** A decoded still image in ARGB pixels. */
    fun decodePixels(pixels: IntArray, width: Int, height: Int): String? =
        decode(RGBLuminanceSource(width, height, pixels), still = true)

    private fun decode(source: LuminanceSource, still: Boolean): String? {
        val attempts = buildList {
            add(BinaryBitmap(HybridBinarizer(source)))
            add(BinaryBitmap(HybridBinarizer(InvertedLuminanceSource(source))))
            if (still) add(BinaryBitmap(GlobalHistogramBinarizer(source)))
        }
        for (bitmap in attempts) {
            val text = runCatching { QRCodeReader().decode(bitmap, hints).text }.getOrNull()
            if (!text.isNullOrEmpty()) return text
        }
        return null
    }
}
