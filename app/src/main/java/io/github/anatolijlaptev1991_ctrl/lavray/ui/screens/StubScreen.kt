// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState

/**
 * Placeholder for a destination that is registered but not built yet. The owner of that destination replaces
 * the one-line body of its own screen file; nothing else changes (see ui/nav/Registry.kt).
 */
@Composable
fun StubScreen(@StringRes title: Int) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        LavEmptyState(title = stringResource(title), text = stringResource(R.string.stub_soon))
    }
}
