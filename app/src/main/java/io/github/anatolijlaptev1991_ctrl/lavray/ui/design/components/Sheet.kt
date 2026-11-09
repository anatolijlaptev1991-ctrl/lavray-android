// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.paneTitle
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LocalMotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Motion
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius
import kotlinx.coroutines.launch

private val SheetShape = RoundedCornerShape(topStart = Radius.dialog, topEnd = Radius.dialog)
private val DialogShape = RoundedCornerShape(Radius.dialog)

/**
 * Bottom sheet (dialogs are sheets on a phone, section 4): scrim, 16 dp top radius, drag handle that
 * dismisses on a downward swipe, content scrolls, the keyboard and navigation bar never cover it.
 * The system back gesture and a tap on the scrim call [onDismiss]. Enter is motion.emphasized from
 * the bottom edge (opacity only with reduced motion).
 */
@Composable
fun LavSheet(
    title: String,
    onDismiss: () -> Unit,
    content: @Composable () -> Unit,
) {
    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false),
    ) {
        val c = Lav.colors
        val reduced = LocalMotionPrefs.current.reduced
        val density = LocalDensity.current
        val enter = remember { Animatable(0f) }
        val drag = remember { Animatable(0f) }
        val scope = rememberCoroutineScope()
        LaunchedEffect(Unit) { enter.animateTo(1f, if (reduced) Motion.fade() else Motion.emphasized()) }

        BoxWithConstraints(
            Modifier
                .fillMaxSize()
                .background(c.scrim.copy(alpha = c.scrim.alpha * enter.value))
                .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onDismiss),
            contentAlignment = Alignment.BottomCenter,
        ) {
            val maxH = maxHeight * 0.92f
            Column(
                Modifier
                    .widthIn(max = 580.dp)
                    .fillMaxWidth()
                    .heightIn(max = maxH)
                    .graphicsLayer {
                        val slide = if (reduced) 0f else (1f - enter.value) * 48.dp.toPx()
                        translationY = slide + drag.value
                        alpha = enter.value
                    }
                    .background(c.bgSurface, SheetShape)
                    .border(1.dp, c.borderStrong, SheetShape)
                    .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = {})
                    .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars))
                    .semantics { paneTitle = title },
            ) {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .height(24.dp)
                        .draggable(
                            orientation = Orientation.Vertical,
                            state = rememberDraggableState { delta -> scope.launch { drag.snapTo((drag.value + delta).coerceAtLeast(0f)) } },
                            onDragStopped = {
                                if (drag.value > with(density) { 96.dp.toPx() }) {
                                    onDismiss()
                                } else {
                                    drag.animateTo(0f, Motion.standard())
                                }
                            },
                        ),
                    contentAlignment = Alignment.Center,
                ) {
                    Box(Modifier.size(36.dp, 4.dp).background(c.borderControl, RoundedCornerShape(2.dp)))
                }
                LavText(
                    title,
                    Modifier.padding(horizontal = 20.dp).semantics { heading() },
                    style = Lav.type.h1,
                )
                Column(
                    Modifier.verticalScroll(rememberScrollState()).padding(start = 20.dp, end = 20.dp, top = 12.dp, bottom = 20.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) { content() }
            }
        }
    }
}

/** Centred confirmation dialog for short yes/no decisions (delete, stop). Buttons are 48 dp. */
@Composable
fun LavConfirmDialog(
    title: String,
    text: String,
    confirmLabel: String,
    dismissLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    danger: Boolean = false,
) {
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        val c = Lav.colors
        Column(
            Modifier
                .padding(horizontal = 24.dp)
                .widthIn(max = 400.dp)
                .fillMaxWidth()
                .background(c.bgSurface, DialogShape)
                .border(1.dp, c.borderStrong, DialogShape)
                .padding(20.dp)
                .semantics { paneTitle = title },
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            LavText(title, Modifier.semantics { heading() }, style = Lav.type.h1)
            LavText(text, style = Lav.type.body, color = c.textSecondary)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                LavButton(dismissLabel, onDismiss, variant = ButtonVariant.Ghost)
                LavButton(confirmLabel, onConfirm, variant = if (danger) ButtonVariant.Danger else ButtonVariant.Primary)
            }
        }
    }
}
