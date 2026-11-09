// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow

val LocalColors = compositionLocalOf { darkColors() }
val LocalType = compositionLocalOf { lavTypography(cjk = false) }

/** Entry point to the tokens: `Lav.colors.accent`, `Lav.type.body`. */
object Lav {
    val colors: LavColors
        @Composable @ReadOnlyComposable get() = LocalColors.current
    val type: LavTypography
        @Composable @ReadOnlyComposable get() = LocalType.current
}

/** Resolves the effective dark flag for a [ThemeMode]; the system choice reads the device setting. */
@Composable
fun resolveDark(mode: ThemeMode): Boolean = when (mode) {
    ThemeMode.SYSTEM -> isSystemInDarkTheme()
    ThemeMode.DARK -> true
    ThemeMode.LIGHT -> false
}

@Composable
fun LavTheme(
    mode: ThemeMode,
    accent: AccentChoice,
    cjk: Boolean,
    motion: MotionPrefs,
    content: @Composable () -> Unit,
) {
    val dark = resolveDark(mode)
    val colors = remember(dark, accent) { if (dark) darkColors(accent) else lightColors(accent) }
    val type = remember(cjk) { lavTypography(cjk) }
    CompositionLocalProvider(
        LocalColors provides colors,
        LocalType provides type,
        LocalMotionPrefs provides motion,
        content = content,
    )
}

/** Text with the design-system style and colour; the single text primitive of the app. */
@Composable
fun LavText(
    text: String,
    modifier: Modifier = Modifier,
    style: TextStyle = Lav.type.body,
    color: Color = Lav.colors.textPrimary,
    maxLines: Int = Int.MAX_VALUE,
    overflow: TextOverflow = TextOverflow.Clip,
    textAlign: TextAlign? = null,
) {
    BasicText(
        text = text,
        modifier = modifier,
        style = style.copy(color = color, textAlign = textAlign ?: TextAlign.Unspecified),
        maxLines = maxLines,
        overflow = overflow,
    )
}
