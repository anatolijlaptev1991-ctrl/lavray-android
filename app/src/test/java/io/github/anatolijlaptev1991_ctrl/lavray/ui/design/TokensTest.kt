// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.ui.graphics.Color
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.pow

/** The design system promises WCAG 2.2 AA (docs/DESIGN-SYSTEM.md section 2 and 12); this proves the tokens keep it. */
class TokensTest {
    private fun lin(v: Float): Double = v.toDouble().let { if (it <= 0.03928) it / 12.92 else ((it + 0.055) / 1.055).pow(2.4) }
    private fun luminance(c: Color): Double = 0.2126 * lin(c.red) + 0.7152 * lin(c.green) + 0.0722 * lin(c.blue)

    private fun contrast(a: Color, b: Color): Double {
        val la = luminance(a)
        val lb = luminance(b)
        return (maxOf(la, lb) + 0.05) / (minOf(la, lb) + 0.05)
    }

    private fun over(fg: Color, bg: Color): Color = Color(
        red = fg.red * fg.alpha + bg.red * (1 - fg.alpha),
        green = fg.green * fg.alpha + bg.green * (1 - fg.alpha),
        blue = fg.blue * fg.alpha + bg.blue * (1 - fg.alpha),
    )

    private fun themes() = listOf("dark" to darkColors(), "light" to lightColors())

    @Test
    fun textTokensReachFourPointFiveOnEverySurface() {
        for ((name, c) in themes()) {
            val surfaces = listOf(c.bgBase, c.bgSurface, c.bgSurface2, c.bgHover)
            val texts = listOf(c.textPrimary, c.textSecondary, c.textTertiary)
            for (bg in surfaces) for (fg in texts) {
                assertTrue("$name text on surface: ${contrast(fg, bg)}", contrast(fg, bg) >= 4.5)
            }
        }
    }

    @Test
    fun controlBordersAndFocusReachThreeToOne() {
        for ((name, c) in themes()) {
            for (bg in listOf(c.bgBase, c.bgSurface, c.bgSurface2)) {
                assertTrue("$name control border: ${contrast(c.borderControl, bg)}", contrast(c.borderControl, bg) >= 3.0)
                assertTrue("$name accent (focus ring): ${contrast(c.accent, bg)}", contrast(c.accent, bg) >= 3.0)
            }
        }
    }

    @Test
    fun textOnAccentAndOnErrorFillIsReadable() {
        for ((name, c) in themes()) {
            assertTrue("$name on accent", contrast(c.onAccent, c.accent) >= 4.5)
            assertTrue("$name on error", contrast(c.onError, c.error) >= 4.5)
        }
    }

    @Test
    fun statusTextAndIndicatorsAreLegibleOnCards() {
        for ((name, c) in themes()) {
            for (tone in listOf(StatusTone.Connected, StatusTone.Connecting, StatusTone.Error, StatusTone.Direct)) {
                assertTrue("$name $tone text", contrast(tone.text(c), c.bgSurface) >= 4.5)
                assertTrue("$name $tone indicator", contrast(tone.indicator(c), c.bgSurface) >= 3.0)
            }
        }
    }

    @Test
    fun everyAccentKeepsContrastInBothThemes() {
        for (accent in AccentChoice.entries) {
            for (c in listOf(darkColors(accent), lightColors(accent))) {
                assertTrue("$accent on surface", contrast(c.accent, c.bgSurface) >= 3.0)
                assertTrue("$accent label", contrast(c.onAccent, c.accent) >= 4.5)
            }
        }
    }

    @Test
    fun softAccentBackgroundKeepsPrimaryTextReadable() {
        for ((name, c) in themes()) {
            val tinted = over(c.accentSoft, c.bgSurface)
            assertTrue("$name selected row", contrast(c.textPrimary, tinted) >= 4.5)
            assertTrue("$name selected row secondary", contrast(c.textTertiary, tinted) >= 4.5)
        }
    }

    @Test
    fun valuesMatchTheSpecification() {
        val d = darkColors()
        assertEquals(Color(0xFF131316), d.bgBase)
        assertEquals(Color(0xFFE0A03A), d.accent)
        assertEquals(Color(0xFF70707A), d.borderControl)
        val l = lightColors()
        assertEquals(Color(0xFFF5F2EC), l.bgBase)
        assertEquals(Color(0xFF466F59), l.accent)
        assertEquals(Color(0xFF0E6B48), l.connectedText)
        assertEquals(Color(0xFF14855A), l.connected)
        assertEquals(Color(0xFF86590F), lightColors(AccentChoice.AMBER).accent)
    }

    @Test
    fun fieldIntensityFollowsTheMotionTable() {
        assertEquals(0.45f, fieldIntensity(MarkState.Off), 0f)
        assertEquals(0.95f, fieldIntensity(MarkState.Connecting), 0f)
        assertEquals(0.75f, fieldIntensity(MarkState.On), 0f)
        assertEquals(0.30f, fieldIntensity(MarkState.Error), 0f)
    }
}
