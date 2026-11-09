// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.absoluteOffset
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LocalMotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Motion

@Composable
private fun SwitchVisual(checked: Boolean, enabled: Boolean, modifier: Modifier = Modifier) {
    val c = Lav.colors
    val reduced = LocalMotionPrefs.current.reduced
    val pos by animateFloatAsState(if (checked) 1f else 0f, if (reduced) Motion.fade() else Motion.standard(), label = "thumb")
    val track = RoundedCornerShape(13.dp)
    Box(
        modifier
            .alpha(if (enabled) 1f else 0.45f)
            .size(44.dp, 26.dp)
            .background(if (checked) c.accent else c.bgSurface2, track)
            .border(1.dp, if (checked) c.accent else c.borderControl, track),
    ) {
        Box(
            Modifier
                .absoluteOffset(x = 4.dp, y = 4.dp)
                .size(18.dp)
                .graphicsLayer { translationX = pos * 18.dp.toPx() }
                .background(if (checked) c.onAccent else c.textSecondary, CircleShape),
        )
    }
}

/**
 * Switch 44 x 26 with a border.control frame (section 6), thumb eased by motion.standard.
 * Exposed as a Switch role so TalkBack announces on or off. Touch target 48 dp.
 */
@Composable
fun LavSwitch(
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    Box(
        modifier
            .minTouch()
            .toggleable(value = checked, enabled = enabled, role = Role.Switch, onValueChange = onCheckedChange),
        contentAlignment = Alignment.Center,
    ) { SwitchVisual(checked, enabled) }
}

/** Label (and optional hint) on the left, the switch on the right; the whole row is one toggle. */
@Composable
fun LavSwitchRow(
    label: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    hint: String? = null,
    enabled: Boolean = true,
) {
    Row(
        modifier
            .fillMaxWidth()
            .minTouch()
            .toggleable(value = checked, enabled = enabled, role = Role.Switch, onValueChange = onCheckedChange)
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            LavText(label, style = Lav.type.body)
            if (hint != null) LavText(hint, style = Lav.type.small, color = Lav.colors.textTertiary)
        }
        SwitchVisual(checked, enabled, Modifier.padding(start = 12.dp))
    }
}
