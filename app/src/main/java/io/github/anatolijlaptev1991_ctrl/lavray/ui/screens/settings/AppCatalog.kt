// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.content.pm.ResolveInfo
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Build
import android.util.LruCache
import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.withContext

/** One installed app the picker offers. */
@Immutable
data class AppEntry(val packageName: String, val label: String, val system: Boolean)

/** Filter and order of the picker list: pure, so it is tested on the JVM. */
object AppFilter {
    /**
     * Apps whose label or package contains [query], system apps only when [showSystem] is on (a chosen system app
     * always stays visible so it can be unchosen), the chosen ones first, then by label.
     */
    fun visible(apps: List<AppEntry>, query: String, showSystem: Boolean, chosen: Set<String>): List<AppEntry> {
        val needle = query.trim().lowercase()
        return apps.asSequence()
            .filter { showSystem || !it.system || it.packageName in chosen }
            .filter { needle.isEmpty() || it.label.lowercase().contains(needle) || it.packageName.lowercase().contains(needle) }
            .sortedWith(compareByDescending<AppEntry> { it.packageName in chosen }.thenBy(String.CASE_INSENSITIVE_ORDER) { it.label })
            .toList()
    }
}

/**
 * Installed apps that have a launcher entry. The query is declared in the manifest as a `<queries>` intent (MAIN and
 * LAUNCHER), which is the narrowest visibility Android offers for this: no QUERY_ALL_PACKAGES permission. Apps
 * without a launcher entry (background services) are not listed; they have no screen a person would pick them by.
 * Labels load in parallel on the default dispatcher, never on the main thread.
 */
object AppCatalog {
    suspend fun load(pm: PackageManager, ownPackage: String): List<AppEntry> = withContext(Dispatchers.Default) {
        val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val infos = query(pm, intent).distinctBy { it.activityInfo.packageName }.filter { it.activityInfo.packageName != ownPackage }
        coroutineScope {
            infos.chunked(CHUNK).map { part -> async { part.map { entry(pm, it) } } }.awaitAll().flatten()
        }
    }

    private fun entry(pm: PackageManager, info: ResolveInfo): AppEntry {
        val app = info.activityInfo.applicationInfo
        val label = info.loadLabel(pm)?.toString()?.takeIf { it.isNotBlank() } ?: info.activityInfo.packageName
        return AppEntry(info.activityInfo.packageName, label, app.flags and ApplicationInfo.FLAG_SYSTEM != 0)
    }

    @Suppress("DEPRECATION")
    private fun query(pm: PackageManager, intent: Intent): List<ResolveInfo> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            pm.queryIntentActivities(intent, PackageManager.ResolveInfoFlags.of(0L))
        } else {
            pm.queryIntentActivities(intent, 0)
        }

    private const val CHUNK = 32
}

/** App icons, drawn to a small bitmap off the main thread and kept in a small LRU cache (about 6 MB at most). */
object AppIcons {
    private const val SIZE_PX = 96
    private val cache = object : LruCache<String, ImageBitmap>(MAX_ENTRIES) {}

    fun cached(packageName: String): ImageBitmap? = cache.get(packageName)

    suspend fun load(pm: PackageManager, packageName: String): ImageBitmap? = withContext(Dispatchers.Default) {
        cache.get(packageName)?.let { return@withContext it }
        val bitmap = try {
            val drawable = pm.getApplicationIcon(packageName)
            val out = Bitmap.createBitmap(SIZE_PX, SIZE_PX, Bitmap.Config.ARGB_8888)
            drawable.setBounds(0, 0, SIZE_PX, SIZE_PX)
            drawable.draw(Canvas(out))
            out.asImageBitmap()
        } catch (_: PackageManager.NameNotFoundException) {
            null
        } ?: return@withContext null
        cache.put(packageName, bitmap)
        bitmap
    }

    private const val MAX_ENTRIES = 160
}
