// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import android.content.Context
import android.os.PowerManager
import android.provider.Settings
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.Easing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.compositionLocalOf

/** Motion tokens of section 11. Only transform and opacity are animated; every transition is interruptible. */
object Motion {
    const val MICRO_MS = 120
    const val STANDARD_MS = 200
    const val EMPHASIZED_MS = 280
    const val CHOREO_MS = 700

    val Standard: Easing = CubicBezierEasing(0.2f, 0.8f, 0.3f, 1f)
    val Enter: Easing = CubicBezierEasing(0.05f, 0.7f, 0.1f, 1f)
    val Exit: Easing = CubicBezierEasing(0.3f, 0f, 0.8f, 0.15f)

    fun <T> micro(): FiniteAnimationSpec<T> = tween(MICRO_MS, easing = Standard)
    fun <T> standard(): FiniteAnimationSpec<T> = tween(STANDARD_MS, easing = Standard)
    fun <T> emphasized(): FiniteAnimationSpec<T> = tween(EMPHASIZED_MS, easing = Enter)
    fun <T> exit(): FiniteAnimationSpec<T> = tween(MICRO_MS, easing = Exit)

    /** Reduced motion keeps only opacity, 120 ms (section 11 table). */
    fun <T> fade(): FiniteAnimationSpec<T> = tween(MICRO_MS, easing = Standard)
}

/**
 * Motion preferences of the device: [reduced] when the user turned animations off in system
 * settings, [ambientAllowed] false as well on battery saver (the light field stays still then).
 */
@Immutable
data class MotionPrefs(val reduced: Boolean, val ambientAllowed: Boolean) {
    companion object {
        val Default = MotionPrefs(reduced = false, ambientAllowed = true)

        fun read(context: Context): MotionPrefs {
            val scale = runCatching {
                Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
            }.getOrDefault(1f)
            val saver = (context.getSystemService(Context.POWER_SERVICE) as? PowerManager)?.isPowerSaveMode == true
            val reduced = scale == 0f
            return MotionPrefs(reduced, ambientAllowed = !reduced && !saver)
        }
    }
}

val LocalMotionPrefs = compositionLocalOf { MotionPrefs.Default }
