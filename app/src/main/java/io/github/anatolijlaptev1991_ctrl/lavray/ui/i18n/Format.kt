// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import io.github.anatolijlaptev1991_ctrl.lavray.R

/** Unit formatter for the language in force; rebuilt only when the configuration (locale) changes. */
@Composable
fun rememberUnitFormat(): UnitFormat {
    val context = LocalContext.current
    val config = LocalConfiguration.current
    return remember(config) {
        val r = context.resources
        UnitFormat(
            UnitLabels(
                byte = r.getString(R.string.ub_b),
                kb = r.getString(R.string.ub_kb),
                mb = r.getString(R.string.ub_mb),
                gb = r.getString(R.string.ub_gb),
                bytePerSec = r.getString(R.string.u_b),
                kbPerSec = r.getString(R.string.u_kb),
                mbPerSec = r.getString(R.string.u_mb),
                gbPerSec = r.getString(R.string.u_gb),
                ms = r.getString(R.string.unit_ms),
            ),
            config.locales[0],
        )
    }
}
