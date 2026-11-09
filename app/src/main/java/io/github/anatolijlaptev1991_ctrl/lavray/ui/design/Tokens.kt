// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/**
 * Colour tokens of LAVRay Design System v1.2 (docs/DESIGN-SYSTEM.md section 2), exact values.
 * Status colours come as an indicator (shapes, dots) and a text variant: they differ in the
 * light theme only. Every status is always paired with text or shape, never colour alone.
 */
@Immutable
class LavColors(
    val dark: Boolean,
    val bgBase: Color,
    val bgSurface: Color,
    val bgSurface2: Color,
    val bgHover: Color,
    val borderSubtle: Color,
    val borderStrong: Color,
    val borderControl: Color,
    val textPrimary: Color,
    val textSecondary: Color,
    val textTertiary: Color,
    val accent: Color,
    val accentHover: Color,
    val accentSoft: Color,
    val onAccent: Color,
    val rayA: Color,
    val rayB: Color,
    val connected: Color,
    val connectedText: Color,
    val connecting: Color,
    val connectingText: Color,
    val error: Color,
    val errorText: Color,
    val onError: Color,
    val direct: Color,
    val directText: Color,
    val scrim: Color,
)

/** The three accents of section 2; AUTO follows the theme (amber on dark, sage on light). */
enum class AccentChoice { AUTO, AMBER, SAGE, GRAPHITE }

/** Theme choice: follow the system, or force one. */
enum class ThemeMode { SYSTEM, DARK, LIGHT }

private class AccentSet(val base: Color, val hover: Color, val soft: Color, val on: Color)

private val darkAmber = AccentSet(Color(0xFFE0A03A), Color(0xFFEDB454), Color(0x21E0A03A), Color(0xFF1A1408))
private val darkSage = AccentSet(Color(0xFF7FA08C), Color(0xFF8FB29C), Color(0x247FA08C), Color(0xFF16221B))
private val darkGraphite = AccentSet(Color(0xFFEDEDEF), Color(0xFFFFFFFF), Color(0x1AEDEDEF), Color(0xFF131316))
private val lightAmber = AccentSet(Color(0xFF86590F), Color(0xFF744C0C), Color(0x1A86590F), Color(0xFFFFF8EC))
private val lightSage = AccentSet(Color(0xFF466F59), Color(0xFF3B604C), Color(0x1F466F59), Color(0xFFF2F7F4))
private val lightGraphite = AccentSet(Color(0xFF3B3B45), Color(0xFF262320), Color(0x143B3B45), Color(0xFFFBF9F5))

private fun accentFor(dark: Boolean, choice: AccentChoice): AccentSet = when (choice) {
    AccentChoice.AUTO -> if (dark) darkAmber else lightSage
    AccentChoice.AMBER -> if (dark) darkAmber else lightAmber
    AccentChoice.SAGE -> if (dark) darkSage else lightSage
    AccentChoice.GRAPHITE -> if (dark) darkGraphite else lightGraphite
}

fun darkColors(accent: AccentChoice = AccentChoice.AUTO): LavColors {
    val a = accentFor(true, accent)
    return LavColors(
        dark = true,
        bgBase = Color(0xFF131316),
        bgSurface = Color(0xFF1B1B1F),
        bgSurface2 = Color(0xFF232329),
        bgHover = Color(0xFF2A2A31),
        borderSubtle = Color(0xFF2C2C33),
        borderStrong = Color(0xFF3B3B45),
        borderControl = Color(0xFF70707A),
        textPrimary = Color(0xFFEDEDEF),
        textSecondary = Color(0xFFA3A3AB),
        textTertiary = Color(0xFF94949C),
        accent = a.base,
        accentHover = a.hover,
        accentSoft = a.soft,
        onAccent = a.on,
        rayA = Color(0xFFF2B94B),
        rayB = Color(0xFFE0813A),
        connected = Color(0xFF4CC38A),
        connectedText = Color(0xFF4CC38A),
        connecting = Color(0xFFE2B93B),
        connectingText = Color(0xFFE2B93B),
        error = Color(0xFFE5697A),
        errorText = Color(0xFFE5697A),
        onError = Color(0xFF2B0F15),
        direct = Color(0xFF5CA8D8),
        directText = Color(0xFF5CA8D8),
        scrim = Color(0x9E060608),
    )
}

fun lightColors(accent: AccentChoice = AccentChoice.AUTO): LavColors {
    val a = accentFor(false, accent)
    return LavColors(
        dark = false,
        bgBase = Color(0xFFF5F2EC),
        bgSurface = Color(0xFFFBF9F5),
        bgSurface2 = Color(0xFFEFEBE3),
        bgHover = Color(0xFFE8E3D9),
        borderSubtle = Color(0xFFE0DAD0),
        borderStrong = Color(0xFFBDB8AE),
        borderControl = Color(0xFF857F73),
        textPrimary = Color(0xFF262320),
        textSecondary = Color(0xFF57534B),
        textTertiary = Color(0xFF69645A),
        accent = a.base,
        accentHover = a.hover,
        accentSoft = a.soft,
        onAccent = a.on,
        rayA = Color(0xFFD99A2B),
        rayB = Color(0xFFB8661F),
        connected = Color(0xFF14855A),
        connectedText = Color(0xFF0E6B48),
        connecting = Color(0xFFA96A0D),
        connectingText = Color(0xFF8F5A0A),
        error = Color(0xFFC23A4E),
        errorText = Color(0xFFA92839),
        onError = Color(0xFFFFF6F7),
        direct = Color(0xFF1D7FB8),
        directText = Color(0xFF176A99),
        scrim = Color(0x5C262320),
    )
}

/** Spacing scale 4/8/12/16/20/24/32/48 (section 4). */
object Sp {
    val s4 = 4.dp
    val s8 = 8.dp
    val s12 = 12.dp
    val s16 = 16.dp
    val s20 = 20.dp
    val s24 = 24.dp
    val s32 = 32.dp
    val s48 = 48.dp
}

/** Radii: sm 6, md 10, lg 14, dialogs 16 (section 4). */
object Radius {
    val sm = 6.dp
    val md = 10.dp
    val lg = 14.dp
    val dialog = 16.dp
}

/** Minimum touch target on a phone (the desktop minimum of 32 px is not enough for a finger). */
val MinTouch = 48.dp
