// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.core

import androidx.annotation.StringRes
import io.github.anatolijlaptev1991_ctrl.lavray.R

/** Dictionary text for an [ErrorKind]: what happened, and for the generic case what to do. */
@StringRes
fun ErrorKind.textRes(): Int = when (this) {
    ErrorKind.CoreUnavailable -> R.string.core_unavailable
    ErrorKind.NotImplemented -> R.string.err_unimplemented
    ErrorKind.Precondition -> R.string.err_precondition
    ErrorKind.InvalidInput -> R.string.err_invalid
    ErrorKind.NotFound -> R.string.err_notfound
    ErrorKind.Generic -> R.string.err_generic
}
