// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection

/**
 * How many of the chosen apps the last built tunnel skipped because they are not installed. The service reports what it
 * skipped when it built the tunnel; counting only those still in the current choice keeps the number true after the
 * choice changed since. "All apps" has no choice to skip from.
 */
internal fun skippedInSelection(skipped: List<String>, selection: AppSelection): Int =
    if (selection.mode == AppMode.All) 0 else skipped.count { it in selection.packages }

/** One line under the per-app choice: "N chosen apps are not installed and were skipped"; nothing when none were. */
@Composable
internal fun SkippedAppsNote(selection: AppSelection, modifier: Modifier = Modifier.padding(horizontal = 16.dp)) {
    val status by LocalEnv.current.vpn.status.collectAsStateWithLifecycle()
    val count = skippedInSelection(status.skippedPackages, selection)
    if (count > 0) LavText(stringResource(R.string.nt_skipped, count.toString()), modifier, style = Lav.type.caption, color = Lav.colors.connectingText)
}
