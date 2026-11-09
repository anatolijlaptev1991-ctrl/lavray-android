// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LocalMotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ReflectionMark
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius

/** Empty state: title, one line of explanation, one or two actions (section 6). */
@Composable
fun LavEmptyState(
    title: String,
    modifier: Modifier = Modifier,
    text: String? = null,
    icon: LavIcon? = null,
    actions: (@Composable () -> Unit)? = null,
) {
    Column(
        modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (icon != null) {
            LavIconView(icon, Lav.colors.textTertiary, size = 32.dp)
        } else {
            ReflectionMark(MarkState.Off, size = 48.dp, small = true)
        }
        LavText(title, style = Lav.type.h2, textAlign = TextAlign.Center)
        if (text != null) LavText(text, style = Lav.type.body, color = Lav.colors.textSecondary, textAlign = TextAlign.Center)
        if (actions != null) {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) { actions() }
        }
    }
}

/** Error state: what happened and what to do, with a retry action supplied by the caller. */
@Composable
fun LavErrorState(
    title: String,
    text: String,
    modifier: Modifier = Modifier,
    actions: (@Composable () -> Unit)? = null,
) {
    Column(
        modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        ReflectionMark(MarkState.Error, size = 56.dp, small = true)
        LavText(title, style = Lav.type.h2, textAlign = TextAlign.Center)
        LavText(text, style = Lav.type.body, color = Lav.colors.textSecondary, textAlign = TextAlign.Center)
        if (actions != null) {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) { actions() }
        }
    }
}

/** Skeleton pulse of section 6: opacity only, still with reduced motion; exists only while loading. */
@Composable
fun LavSkeletonRows(count: Int, label: String, modifier: Modifier = Modifier) {
    val c = Lav.colors
    val reduced = LocalMotionPrefs.current.reduced
    val pulse = if (reduced) {
        null
    } else {
        rememberInfiniteTransition(label = "skeleton").animateFloat(
            0.5f, 1f, infiniteRepeatable(tween(900), RepeatMode.Reverse), label = "alpha",
        )
    }
    Column(
        modifier.fillMaxWidth().semantics { contentDescription = label },
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        repeat(count) {
            Box(
                Modifier
                    .fillMaxWidth()
                    .height(56.dp)
                    .graphicsLayer { alpha = pulse?.value ?: 0.7f }
                    .background(c.bgSurface2, RoundedCornerShape(Radius.md))
                    .clearAndSetSemantics { },
            )
        }
    }
}

/** Placeholder alpha helper for disabled content. */
fun Modifier.dimmed(on: Boolean): Modifier = if (on) alpha(0.45f) else this
