// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

/** Field intensity by connection state (section 11): off .45, connecting .95, on .75, error .30. */
fun fieldIntensity(state: MarkState): Float = when (state) {
    MarkState.Off -> 0.45f
    MarkState.Connecting -> 0.95f
    MarkState.On -> 0.75f
    MarkState.Error -> 0.30f
}

/**
 * The "light field": two soft radial rays behind the content, drawn as two gradients, no bitmap, no blur,
 * no grain. It is static. Intensity eases between states (200 ms) and only while [MarkState.Connecting]
 * the rays drift slowly, which is the only animation here; it never runs when idle, when
 * reduced motion or battery saver is on, or when this composable is not on screen (it is only composed
 * by the connection screen, so leaving the screen disposes it and its animation with it).
 */
@Composable
fun LightField(state: MarkState, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val c = Lav.colors
    val prefs = LocalMotionPrefs.current
    val intensity = remember { Animatable(fieldIntensity(state)) }
    val drift = remember { Animatable(0f) }

    LaunchedEffect(state, prefs.reduced) {
        val target = fieldIntensity(state)
        if (prefs.reduced) intensity.snapTo(target) else intensity.animateTo(target, Motion.standard())
    }
    LaunchedEffect(state, prefs.ambientAllowed) {
        drift.snapTo(0f)
        if (state == MarkState.Connecting && prefs.ambientAllowed) {
            drift.animateTo(1f, infiniteRepeatable(tween(DRIFT_MS, easing = LinearEasing)))
        }
    }

    val strength = if (c.dark) DARK_ALPHA else LIGHT_ALPHA
    Box(
        modifier.drawBehind {
            val a = intensity.value * strength
            val t = drift.value * TWO_PI
            val w = size.width
            val h = size.height
            val r = maxOf(w, h) * 0.75f
            val c1 = Offset(w * (0.2f + 0.08f * kotlin.math.sin(t)), h * 0.12f)
            val c2 = Offset(w * (0.85f - 0.08f * kotlin.math.cos(t)), h * 0.3f)
            drawRect(Brush.radialGradient(listOf(c.rayA.copy(alpha = a), Color.Transparent), c1, r))
            drawRect(Brush.radialGradient(listOf(c.rayB.copy(alpha = a * 0.7f), Color.Transparent), c2, r * 0.8f))
        },
        content = { content() },
    )
}

private const val DRIFT_MS = 9000
private const val DARK_ALPHA = 0.16f
private const val LIGHT_ALPHA = 0.14f
private const val TWO_PI = 6.2831855f
