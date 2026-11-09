// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** The four states of the "Reflection" mark (section 1). Shape carries the meaning, not colour alone. */
enum class MarkState { Off, Connecting, On, Error }

/** Geometry of the mark on its 64 grid (from the desktop BrandMark and the prototype). */
private object MarkGeometry {
    const val GRID = 64f
    const val BEAM_LENGTH = 52f // 30 down + 22 right
    val beam = Path().apply {
        moveTo(23f, 13f)
        lineTo(23f, 43f)
        lineTo(45f, 43f)
    }
    val mirror = Path().apply {
        moveTo(14.5f, 43f)
        lineTo(23f, 51.5f)
    }
}

/**
 * Full mark (beam 8/64, mirror, photon r 3.6) or [small] (beam 10, no mirror, photon r 5) for 16-24 dp.
 * Animations exist only while they mean something: the dash runs only in [MarkState.Connecting]
 * and the beam draws in once on the way to [MarkState.On]; nothing runs when idle, and reduced motion
 * shows the final state at once.
 */
@Composable
fun ReflectionMark(
    state: MarkState,
    modifier: Modifier = Modifier,
    size: Dp = 96.dp,
    small: Boolean = false,
) {
    val c = Lav.colors
    val reduced = LocalMotionPrefs.current.reduced
    val reveal = remember { Animatable(if (state == MarkState.On) 1f else 0f) }
    val flow = remember { Animatable(0f) }

    LaunchedEffect(state, reduced) {
        if (state == MarkState.On) {
            if (reduced) reveal.snapTo(1f) else reveal.animateTo(1f, tween(Motion.CHOREO_MS, easing = Motion.Enter))
        } else {
            reveal.snapTo(0f)
        }
    }
    LaunchedEffect(state, reduced) {
        flow.snapTo(0f)
        if (state == MarkState.Connecting && !reduced) {
            flow.animateTo(1f, infiniteRepeatable(tween(800, easing = LinearEasing)))
        }
    }

    val brush = remember(c, small) {
        val top = if (small) c.rayA else c.rayA.copy(alpha = 0.3f)
        val stops = if (small) arrayOf(0f to c.rayA, 1f to c.rayB) else arrayOf(0f to top, 0.5f to c.rayA, 1f to c.rayB)
        Brush.linearGradient(colorStops = stops, start = Offset(23f, 9f), end = Offset(49f, 43f))
    }
    val mirrorColor = c.textPrimary.copy(alpha = 0.92f)

    Canvas(modifier.size(size)) {
        val k = this.size.minDimension / MarkGeometry.GRID
        scale(k, k, pivot = Offset.Zero) {
            translate(-4.5f, 1.5f) {
                drawBeam(state, small, brush, reveal.value, flow.value)
                if (!small) drawPath(MarkGeometry.mirror, mirrorColor, style = Stroke(2.6f, cap = StrokeCap.Round))
                drawPhoton(state, small, c)
            }
        }
    }
}

private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawBeam(
    state: MarkState,
    small: Boolean,
    brush: Brush,
    reveal: Float,
    flow: Float,
) {
    val len = MarkGeometry.BEAM_LENGTH
    val width = if (small) 10f else 8f
    var alpha = 1f
    var effect: PathEffect? = null
    when (state) {
        MarkState.Off -> alpha = 0.38f
        MarkState.Connecting -> {
            alpha = 0.8f
            val on = len * 0.12f
            val off = len * 0.09f
            effect = PathEffect.dashPathEffect(floatArrayOf(on, off), -flow * (on + off))
        }
        MarkState.On -> {
            if (reveal <= 0f) return
            if (reveal < 1f) effect = PathEffect.dashPathEffect(floatArrayOf(len, len), len * (1f - reveal))
        }
        MarkState.Error -> {
            alpha = 0.75f
            effect = PathEffect.dashPathEffect(floatArrayOf(len * 0.58f, len * 0.09f, len * 1.0f, len * 0.09f), 0f)
        }
    }
    drawPath(
        MarkGeometry.beam,
        brush,
        alpha = alpha,
        style = Stroke(width, cap = StrokeCap.Round, join = StrokeJoin.Bevel, pathEffect = effect),
    )
}

private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawPhoton(state: MarkState, small: Boolean, c: LavColors) {
    val r = if (small) 5f else 3.6f
    val center = Offset(if (small) 56.5f else 55.5f, 43f)
    when (state) {
        MarkState.Off -> Unit
        MarkState.Connecting -> drawCircle(c.rayB, r, center, alpha = 0.85f, style = Stroke(2.4f))
        MarkState.On -> drawCircle(c.rayB, r, center)
        MarkState.Error -> drawCircle(c.error, r, center)
    }
}
