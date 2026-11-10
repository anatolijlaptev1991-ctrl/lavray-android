// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText

/** One bottom-navigation or rail entry. The [label] is also the TalkBack name. */
@Immutable
class NavItem(val id: String, val label: String, val icon: LavIcon)

/**
 * Bottom navigation of section 4: icon and label, at least 56 dp tall, the selected entry is marked by an
 * accent pill, a heavier label and the selected semantics (not by colour alone).
 */
@Composable
fun LavBottomNav(items: List<NavItem>, selectedId: String, onSelect: (String) -> Unit, modifier: Modifier = Modifier) {
    val c = Lav.colors
    Row(
        modifier.fillMaxWidth().background(c.bgSurface).heightIn(min = IntrinsicHeight56),
        horizontalArrangement = Arrangement.SpaceEvenly,
    ) {
        items.forEach { item -> NavEntry(item, item.id == selectedId, { onSelect(item.id) }, Modifier.weight(1f)) }
    }
}

/** Vertical variant for wide screens and landscape (the desktop collapses its sidebar to icons the same way). */
@Composable
fun LavNavRail(items: List<NavItem>, selectedId: String, onSelect: (String) -> Unit, modifier: Modifier = Modifier) {
    val c = Lav.colors
    Column(
        modifier.width(80.dp).background(c.bgSurface).padding(vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        items.forEach { item -> NavEntry(item, item.id == selectedId, { onSelect(item.id) }, Modifier.fillMaxWidth()) }
    }
}

private val IntrinsicHeight56 = 64.dp
private const val NAV_MAX_FONT_SCALE = 1.3f

@Composable
private fun NavEntry(item: NavItem, selected: Boolean, onClick: () -> Unit, modifier: Modifier) {
    val c = Lav.colors
    val shape = RoundedCornerShape(16.dp)
    Column(
        modifier
            .heightIn(min = 56.dp)
            .selectable(selected = selected, role = Role.Tab, onClick = onClick)
            .semantics { contentDescription = item.label }
            .padding(vertical = 6.dp, horizontal = 2.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Box(
            Modifier.widthIn(max = 56.dp).fillMaxWidth().height(28.dp).background(if (selected) c.accentSoft else androidx.compose.ui.graphics.Color.Transparent, shape),
            contentAlignment = Alignment.Center,
        ) { LavIconView(item.icon, if (selected) c.accent else c.textTertiary, size = 20.dp) }
        // Five labels share the width of the phone: at 200 % text size they would all end in an ellipsis, so the
        // label grows to 130 % at most (the TalkBack name is unaffected) like the system navigation bar.
        val density = LocalDensity.current
        CompositionLocalProvider(LocalDensity provides Density(density.density, minOf(density.fontScale, NAV_MAX_FONT_SCALE))) {
            LavText(
                item.label,
                Modifier.fillMaxWidth(),
                style = if (selected) Lav.type.caption.copy(fontWeight = androidx.compose.ui.text.font.FontWeight.SemiBold) else Lav.type.caption,
                color = if (selected) c.textPrimary else c.textTertiary,
                maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center,
            )
        }
    }
}

/** Tabs inside a section (WAI-ARIA tabs pattern): equal width, 48 dp tall, accent underline on the selected one. */
@Composable
fun LavTabs(labels: List<String>, selectedIndex: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val c = Lav.colors
    Row(modifier.fillMaxWidth()) {
        labels.forEachIndexed { index, label ->
            val selected = index == selectedIndex
            Column(
                Modifier
                    .weight(1f)
                    .selectable(selected = selected, role = Role.Tab, onClick = { onSelect(index) }),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                // No weight here: inside a Column that gets the screen height as its maximum, a weighted child
                // would swallow all of it and push the bottom navigation off the screen.
                Box(Modifier.fillMaxWidth().heightIn(min = 46.dp).padding(horizontal = 8.dp), contentAlignment = Alignment.Center) {
                    LavText(
                        label, style = if (selected) Lav.type.bodyStrong else Lav.type.body,
                        color = if (selected) c.textPrimary else c.textTertiary,
                        maxLines = 1, overflow = TextOverflow.Ellipsis,
                    )
                }
                Box(Modifier.fillMaxWidth().height(2.dp).background(if (selected) c.accent else c.borderSubtle))
            }
        }
    }
}
