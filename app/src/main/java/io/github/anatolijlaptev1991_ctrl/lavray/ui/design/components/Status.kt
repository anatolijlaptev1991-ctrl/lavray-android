// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavColors
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius

/** Statuses of section 2. A tone always comes with text or a shape, never colour alone. */
enum class StatusTone {
    Connected, Connecting, Error, Direct, Neutral;

    fun indicator(c: LavColors): Color = when (this) {
        Connected -> c.connected
        Connecting -> c.connecting
        Error -> c.error
        Direct -> c.direct
        Neutral -> c.textTertiary
    }

    fun text(c: LavColors): Color = when (this) {
        Connected -> c.connectedText
        Connecting -> c.connectingText
        Error -> c.errorText
        Direct -> c.directText
        Neutral -> c.textSecondary
    }
}

/** Dot with a label next to it; the label is the status for screen readers. */
@Composable
fun StatusDot(tone: StatusTone, label: String, modifier: Modifier = Modifier) {
    val c = Lav.colors
    Row(modifier, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Box(Modifier.size(8.dp).background(tone.indicator(c), CircleShape))
        LavText(label, style = Lav.type.small, color = tone.text(c), maxLines = 1)
    }
}

/**
 * Banner of section 6: icon, title, optional text and actions. Errors are announced as an alert
 * (live region), so TalkBack reads them when they appear.
 */
@Composable
fun LavBanner(
    tone: StatusTone,
    title: String,
    modifier: Modifier = Modifier,
    text: String? = null,
    actions: (@Composable () -> Unit)? = null,
) {
    val c = Lav.colors
    val shape = RoundedCornerShape(Radius.md)
    val fg = tone.text(c)
    val icon = if (tone == StatusTone.Error || tone == StatusTone.Connecting) LavIcon.Alert else LavIcon.Info
    Column(
        modifier
            .fillMaxWidth()
            .background(c.bgSurface, shape)
            .border(1.dp, tone.indicator(c), shape)
            .padding(12.dp)
            .semantics { liveRegion = if (tone == StatusTone.Error) LiveRegionMode.Assertive else LiveRegionMode.Polite },
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            LavIconView(icon, fg, size = 20.dp)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                LavText(title, style = Lav.type.bodyStrong, color = fg)
                if (text != null) LavText(text, style = Lav.type.small, color = c.textSecondary)
            }
        }
        if (actions != null) FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { actions() }
    }
}

/** Static colour swatch for tests and the contrast table. */
internal fun StatusTone.pair(c: LavColors): Pair<Color, Color> = indicator(c) to text(c)
