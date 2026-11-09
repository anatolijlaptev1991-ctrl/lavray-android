// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius
import kotlinx.coroutines.delay

/** One toast: [id] makes two identical messages distinct so the timer restarts. */
@Immutable
data class ToastMessage(val id: Long, val text: String, val actionLabel: String? = null, val onAction: (() -> Unit)? = null)

/** Holder of the single visible toast. Newest wins; the host hides it after [DURATION_MS]. */
@Stable
class ToastState {
    var current: ToastMessage? by mutableStateOf(null)
        private set
    private var counter = 0L

    fun show(text: String, actionLabel: String? = null, onAction: (() -> Unit)? = null) {
        counter += 1
        current = ToastMessage(counter, text, actionLabel, onAction)
    }

    fun dismiss(id: Long) {
        if (current?.id == id) current = null
    }

    companion object {
        const val DURATION_MS = 4500L
    }
}

/** Toast of section 6 (with an optional "Undo"-style action). A polite live region; hosts place it above the navigation. */
@Composable
fun ToastHost(state: ToastState, modifier: Modifier = Modifier) {
    val msg = state.current ?: return
    LaunchedEffect(msg.id) {
        delay(ToastState.DURATION_MS)
        state.dismiss(msg.id)
    }
    val c = Lav.colors
    val shape = RoundedCornerShape(Radius.md)
    Box(modifier.fillMaxWidth().padding(horizontal = 16.dp), contentAlignment = Alignment.BottomCenter) {
        Row(
            Modifier
                .background(c.bgSurface2, shape)
                .border(1.dp, c.borderStrong, shape)
                .padding(start = 16.dp, top = 4.dp, bottom = 4.dp, end = 4.dp)
                .semantics { liveRegion = LiveRegionMode.Polite },
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            LavText(msg.text, Modifier.weight(1f, fill = false), style = Lav.type.body)
            if (msg.actionLabel != null && msg.onAction != null) {
                LavButton(msg.actionLabel, { msg.onAction.invoke(); state.dismiss(msg.id) }, variant = ButtonVariant.Ghost)
            }
        }
    }
}
