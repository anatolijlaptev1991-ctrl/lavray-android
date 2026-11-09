// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.border
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.sizeIn
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LocalMotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Motion
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MinTouch

/** Hit area of at least 48 x 48 dp; the visual element inside may be smaller (chips are 32 dp tall). */
fun Modifier.minTouch(): Modifier = sizeIn(minWidth = MinTouch, minHeight = MinTouch)

/**
 * The one clickable of the design system: pressed overlay (`bg.hover`), a 2 dp accent focus ring
 * for keyboards and switch access (section 12), optional press scale .97 (motion.micro; off for reduced
 * motion), semantics role and an optional long press. No ripple, no composed {} modifier.
 */
@Composable
fun Modifier.lavClickable(
    onClick: () -> Unit,
    shape: Shape,
    enabled: Boolean = true,
    role: Role? = Role.Button,
    onClickLabel: String? = null,
    pressScale: Boolean = false,
    onLongClick: (() -> Unit)? = null,
): Modifier {
    val source = remember { MutableInteractionSource() }
    val focused by source.collectIsFocusedAsState()
    val pressed = source.collectIsPressedAsState()
    val hover = Lav.colors.bgHover
    val ring = Lav.colors.accent
    val reduced = LocalMotionPrefs.current.reduced
    val scale = if (pressScale) {
        val s by animateFloatAsState(if (pressed.value && !reduced) 0.97f else 1f, Motion.micro(), label = "press")
        s
    } else {
        1f
    }
    val base = this
        .graphicsLayer { scaleX = scale; scaleY = scale }
        .clip(shape)
        .drawBehind { if (pressed.value && enabled) drawRect(hover) }
        .then(if (focused) Modifier.border(2.dp, ring, shape) else Modifier)
    return base.combinedClickable(
        interactionSource = source,
        indication = null,
        enabled = enabled,
        onClickLabel = onClickLabel,
        role = role,
        onLongClick = onLongClick,
        onClick = onClick,
    )
}
