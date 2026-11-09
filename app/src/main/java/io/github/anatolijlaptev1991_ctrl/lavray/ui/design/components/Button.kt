// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LocalMotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius

enum class ButtonVariant { Primary, Secondary, Ghost, Danger }

private val ButtonShape = RoundedCornerShape(Radius.md)

/** Button of section 6: primary / secondary / ghost / danger, optional icon, loading state. Touch height 48 dp. */
@Composable
fun LavButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    variant: ButtonVariant = ButtonVariant.Secondary,
    icon: LavIcon? = null,
    loading: Boolean = false,
    enabled: Boolean = true,
) {
    val c = Lav.colors
    val (bg, fg) = when (variant) {
        ButtonVariant.Primary -> c.accent to c.onAccent
        ButtonVariant.Secondary -> c.bgSurface2 to c.textPrimary
        ButtonVariant.Ghost -> Color.Transparent to c.textPrimary
        ButtonVariant.Danger -> c.error to c.onError
    }
    val border = if (variant == ButtonVariant.Secondary) Modifier.border(1.dp, c.borderControl, ButtonShape) else Modifier
    val active = enabled && !loading
    Row(
        modifier
            .minTouch()
            .alpha(if (enabled) 1f else 0.45f)
            .background(bg, ButtonShape)
            .then(border)
            .lavClickable(onClick, ButtonShape, enabled = active, pressScale = true)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (loading) Spinner(fg) else if (icon != null) LavIconView(icon, fg, size = 18.dp)
        LavText(text, style = Lav.type.button, color = fg, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

/** Icon-only button: [contentDescription] is mandatory, it is the TalkBack name. 48 dp target. */
@Composable
fun LavIconButton(
    icon: LavIcon,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color = Lav.colors.textSecondary,
    enabled: Boolean = true,
) {
    Box(
        modifier
            .minTouch()
            .alpha(if (enabled) 1f else 0.45f)
            .lavClickable(onClick, RoundedCornerShape(Radius.md), enabled = enabled, role = Role.Button)
            .semantics { this.contentDescription = contentDescription },
        contentAlignment = Alignment.Center,
    ) { LavIconView(icon, tint, size = 20.dp) }
}

/** Indeterminate arc; it spins only while it is composed (loading). Static with reduced motion. */
@Composable
fun Spinner(color: Color, modifier: Modifier = Modifier, size: Dp = 18.dp) {
    val reduced = LocalMotionPrefs.current.reduced
    val angle = if (reduced) {
        0f
    } else {
        val t = rememberInfiniteTransition(label = "spin")
        val a by t.animateFloat(0f, 360f, infiniteRepeatable(tween(900, easing = LinearEasing), RepeatMode.Restart), label = "angle")
        a
    }
    Canvas(modifier.size(size)) {
        val w = 2.dp.toPx()
        rotate(angle) {
            drawArc(
                color, startAngle = 0f, sweepAngle = 270f, useCenter = false,
                topLeft = Offset(w / 2, w / 2), size = Size(this.size.width - w, this.size.height - w),
                style = Stroke(w, cap = StrokeCap.Round),
            )
        }
    }
}
