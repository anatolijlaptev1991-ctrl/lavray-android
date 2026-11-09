// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.runtime.Immutable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineBreak
import androidx.compose.ui.unit.sp

/**
 * Type scale of section 3 (rem x 16 -> sp): display 24/700, h1 20/650, h2 16.8/600,
 * body 14, small 13, caption 12. The system font is used (no font files in the APK).
 * Chinese gets +0.15 line height; digits in columns use tabular figures.
 */
@Immutable
class LavTypography(
    val display: TextStyle,
    val h1: TextStyle,
    val h2: TextStyle,
    val body: TextStyle,
    val bodyStrong: TextStyle,
    val small: TextStyle,
    val caption: TextStyle,
    val mono: TextStyle,
    val button: TextStyle,
)

private fun style(size: Float, weight: Int, cjk: Boolean, lineFactor: Float = 1.4f): TextStyle = TextStyle(
    fontFamily = FontFamily.Default,
    fontSize = size.sp,
    fontWeight = FontWeight(weight),
    lineHeight = (size * (lineFactor + if (cjk) 0.15f else 0f)).sp,
    lineBreak = LineBreak.Paragraph,
)

fun lavTypography(cjk: Boolean): LavTypography {
    val body = style(14f, 400, cjk)
    return LavTypography(
        display = style(24f, 700, cjk, 1.25f),
        h1 = style(20f, 650, cjk, 1.3f),
        h2 = style(16.8f, 600, cjk, 1.3f),
        body = body,
        bodyStrong = style(14f, 600, cjk),
        small = style(13f, 400, cjk),
        caption = style(12f, 400, cjk),
        mono = body.copy(fontFamily = FontFamily.Monospace, fontFeatureSettings = "tnum", fontSize = 13.sp),
        button = style(14f, 600, cjk, 1.2f),
    )
}

