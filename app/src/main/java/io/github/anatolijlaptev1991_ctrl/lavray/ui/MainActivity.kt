// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.remember
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavTheme
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.resolveDark
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.AppLocale
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.IncomingIntent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.AppShell
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.rememberNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.UiPrefs

/**
 * The only activity. onCreate does what the first frame needs and nothing more: read the small UI preferences
 * (theme and language), create the environment (models are lazy) and set the content. The :vpn process is
 * bound in onStart, asynchronously, and unbound in onStop so an idle :vpn can exit.
 */
class MainActivity : ComponentActivity() {
    private lateinit var env: AppEnv

    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase, UiPrefs(newBase).load().language))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        env = AppEnv(applicationContext, VpnController(this), UiPrefs(this))
        setContent { Root() }
        if (savedInstanceState == null) offerIncoming(intent)
    }

    override fun onStart() {
        super.onStart()
        env.controller.bind()
    }

    override fun onStop() {
        env.controller.unbind()
        super.onStop()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        offerIncoming(intent)
    }

    override fun onDestroy() {
        env.close()
        env.controller.close()
        super.onDestroy()
    }

    private fun offerIncoming(intent: Intent?) {
        val text = IncomingIntent.extract(intent?.action, intent?.dataString, intent?.getCharSequenceExtra(Intent.EXTRA_TEXT))
        if (text != null) env.importer.open(text)
    }

    @androidx.compose.runtime.Composable
    private fun Root() {
        val settings = env.settings
        val motion = remember { MotionPrefs.read(this) }
        val language = AppLocale.effective(this, settings.language)
        LavTheme(settings.theme, settings.accent, language.isCjk, motion) {
            val dark = resolveDark(settings.theme)
            SideEffect {
                val style = if (dark) SystemBarStyle.dark(Color.TRANSPARENT) else SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
            }
            CompositionLocalProvider(LocalEnv provides env, LocalNavigator provides rememberNavigator()) { AppShell() }
        }
    }
}
