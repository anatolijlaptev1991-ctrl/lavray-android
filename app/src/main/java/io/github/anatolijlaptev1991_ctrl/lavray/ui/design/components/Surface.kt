// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius

private val CardShape = RoundedCornerShape(Radius.lg)
private val ChipShape = RoundedCornerShape(50)

/** Card: bg.surface, subtle border, radius 14. [strong] draws the stronger frame used by active cards. */
@Composable
fun LavCard(
    modifier: Modifier = Modifier,
    strong: Boolean = false,
    content: @Composable () -> Unit,
) {
    val c = Lav.colors
    Box(
        modifier
            .background(c.bgSurface, CardShape)
            .border(1.dp, if (strong) c.borderStrong else c.borderSubtle, CardShape),
    ) { content() }
}

/**
 * Chip: 32 dp visual in a 48 dp touch target. The selected state is shown by frame (1.5 dp), soft accent
 * fill and a check mark, never by colour alone. The border is border.control and keeps 3:1.
 */
@Composable
fun LavChip(
    text: String,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: LavIcon? = null,
) {
    val c = Lav.colors
    Box(
        modifier
            .minTouch()
            .lavClickable(onClick, ChipShape, role = Role.RadioButton)
            .semantics { this.selected = selected },
        contentAlignment = Alignment.Center,
    ) {
        Row(
            Modifier
                .heightIn(min = 32.dp)
                .background(if (selected) c.accentSoft else Color.Transparent, ChipShape)
                .border(if (selected) 1.5.dp else 1.dp, if (selected) c.accent else c.borderControl, ChipShape)
                .padding(horizontal = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (selected) {
                LavIconView(LavIcon.Check, c.accent, size = 16.dp)
            } else if (icon != null) {
                LavIconView(icon, c.textSecondary, size = 16.dp)
            }
            LavText(
                text, style = if (selected) Lav.type.bodyStrong else Lav.type.small,
                color = c.textPrimary, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** Neutral badge (protocol names and the like): colour is reserved for statuses (section 2). */
@Composable
fun LavBadge(text: String, modifier: Modifier = Modifier, tone: StatusTone? = null) {
    val c = Lav.colors
    val fg = tone?.text(c) ?: c.textSecondary
    LavText(
        text,
        modifier.background(c.bgSurface2, RoundedCornerShape(Radius.sm)).padding(horizontal = 6.dp, vertical = 2.dp),
        style = Lav.type.caption, color = fg, maxLines = 1,
    )
}

/**
 * List row: [title], optional [subtitle] (tertiary) and leading / trailing slots. A row with [onClick] is a
 * 56 dp minimum touch target. [description] overrides the merged TalkBack text when the content is visual.
 */
@Composable
fun LavListRow(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
    onClick: (() -> Unit)? = null,
    onLongClick: (() -> Unit)? = null,
    selected: Boolean = false,
    description: String? = null,
) {
    val c = Lav.colors
    val shape = RoundedCornerShape(Radius.md)
    val clickable = if (onClick != null) {
        Modifier.lavClickable(onClick, shape, role = Role.Button, onLongClick = onLongClick)
    } else {
        Modifier
    }
    Row(
        modifier
            .heightIn(min = 56.dp)
            .background(if (selected) c.accentSoft else Color.Transparent, shape)
            .then(clickable)
            .then(if (description != null) Modifier.semantics { contentDescription = description } else Modifier)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (leading != null) {
            leading()
            Spacer(Modifier.width(12.dp))
        }
        Column(Modifier.weight(1f)) {
            LavText(title, style = Lav.type.body, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (subtitle != null) {
                LavText(subtitle, style = Lav.type.small, color = c.textTertiary, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        if (trailing != null) {
            Spacer(Modifier.width(8.dp))
            trailing()
        }
    }
}

/** Group title above a block of rows (caption, tertiary). */
@Composable
fun LavSectionLabel(text: String, modifier: Modifier = Modifier) {
    LavText(text, modifier.padding(horizontal = 4.dp, vertical = 8.dp), style = Lav.type.caption, color = Lav.colors.textTertiary)
}
