// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.content.Context

/** Settings owned by the :vpn process (the boot receiver reads them before any UI exists). */
class VpnPrefs(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    var autoConnect: Boolean
        get() = prefs.getBoolean(KEY_AUTO_CONNECT, false)
        set(value) = prefs.edit().putBoolean(KEY_AUTO_CONNECT, value).apply()

    /** Per-app choice as JSON (see [AppSelection]); empty means all apps. */
    var appSelection: AppSelection
        get() = AppSelection.parse(prefs.getString(KEY_APPS, null))
        set(value) = prefs.edit().putString(KEY_APPS, value.toJson()).apply()

    private companion object {
        const val KEY_APPS = "app_selection"
        const val FILE = "vpn"
        const val KEY_AUTO_CONNECT = "auto_connect"
    }
}
