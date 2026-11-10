// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.content.pm.ResolveInfo
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Build
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
data class AppEntry(val packageName: String, val label: String, val system: Boolean, val missing: Boolean = false)

/** Filter and order of the picker list: pure, so it is tested on the JVM. */
object AppFilter {
    /**
     * The chosen packages the list does not know (uninstalled, or without a launcher entry) are added as [AppEntry.missing]
     * rows. A person can then unchoose them: the choice would otherwise keep names nothing on screen can remove, and
     * "only the chosen apps" with only such names builds a tunnel for no app at all.
     */
    fun withMissing(apps: List<AppEntry>, chosen: Set<String>): List<AppEntry> {
        val known = apps.mapTo(HashSet()) { it.packageName }
        val gone = chosen.filter { it !in known }.sorted().map { AppEntry(it, it, system = false, missing = true) }
        return if (gone.isEmpty()) apps else apps + gone
    }

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
        val label = info.loadLabel(pm).toString().takeIf { it.isNotBlank() } ?: info.activityInfo.packageName
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

/**
 * App icons, drawn to a small bitmap off the main thread and kept in a small LRU cache (about 3.5 MB at most). The
 * cache is emptied when the picker leaves the screen ([clear]), so the UI process does not keep the bitmaps while the
 * person is on another screen. 96 px stays: the row icon is 40 dp, which is 96 px at xhdpi+ and a smaller size would blur.
 */
object AppIcons {
    private const val SIZE_PX = 96
    private val cache = BoundedCache<String, ImageBitmap>(MAX_ENTRIES)

    fun cached(packageName: String): ImageBitmap? = cache.get(packageName)

    /** Drops every cached icon; called when the picker is left. */
    fun clear() = cache.evictAll()

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

    private const val MAX_ENTRIES = 100
}
