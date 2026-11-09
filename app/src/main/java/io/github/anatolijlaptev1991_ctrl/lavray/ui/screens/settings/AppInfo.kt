// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.produceState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult

/** Where the project lives. Opened in the browser by the person's tap; the app never contacts these addresses itself. */
object ProjectLinks {
    const val RELEASES = "https://github.com/anatolijlaptev1991-ctrl/lavray-android/releases"
    const val APP_REPO = "https://github.com/anatolijlaptev1991-ctrl/lavray-android"
    const val APP_ISSUES = "https://github.com/anatolijlaptev1991-ctrl/lavray-android/issues"
    const val CORE_REPO = "https://github.com/anatolijlaptev1991-ctrl/lavray"
    const val GPL_TEXT = "https://www.gnu.org/licenses/gpl-3.0.html"
}

object AppInfo {
    /** The installed version name (the manifest's versionName), or a dash when the system does not report it. */
    fun versionName(context: Context): String = try {
        val pm = context.packageManager
        val info = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            pm.getPackageInfo(context.packageName, PackageManager.PackageInfoFlags.of(0L))
        } else {
            @Suppress("DEPRECATION")
            pm.getPackageInfo(context.packageName, 0)
        }
        info.versionName?.takeIf { it.isNotBlank() } ?: DASH
    } catch (_: PackageManager.NameNotFoundException) {
        DASH
    }

    private const val DASH = "—"
}

/** Opens [url] in the browser. False when the device has no browser, so the caller can say so. */
fun openUrl(context: Context, url: String): Boolean = try {
    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    true
} catch (_: ActivityNotFoundException) {
    false
}

/** The core's own version, read once from its status; null while unknown or when the core does not answer. */
@Composable
fun rememberCoreVersion(): State<String?> {
    val env = LocalEnv.current
    return produceState<String?>(null, env) {
        value = (env.session.call { coreGetStatus() } as? CoreResult.Ok)?.value?.coreVersion?.takeIf { it.isNotBlank() }
    }
}
