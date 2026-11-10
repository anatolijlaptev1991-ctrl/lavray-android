// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import android.content.Context
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json

/** Where the last failure of each subscription is kept; the store in the app, memory in tests. */
interface SubFailureStorage {
    fun load(): Map<Long, SubFailure>
    fun save(failures: Map<Long, SubFailure>)
}

/**
 * Remembers the last failed update of each subscription, because the core group has no field for it
 * (desktop keeps it in local storage). Only a short code is stored, never the provider's text: it can
 * contain the link with its token.
 */
class SubFailureStore(context: Context) : SubFailureStorage {
    private val prefs by lazy { context.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE) }

    override fun load(): Map<Long, SubFailure> = decode(prefs.getString(KEY, null))

    override fun save(failures: Map<Long, SubFailure>) {
        prefs.edit().putString(KEY, encode(failures)).apply()
    }

    companion object {
        private const val FILE = "sub_failures"
        private const val KEY = "failures"
        private const val MAX_CODE = 16
        private val serializer = MapSerializer(String.serializer(), String.serializer())

        fun encode(failures: Map<Long, SubFailure>): String =
            Json.encodeToString(serializer, failures.entries.associate { it.key.toString() to encodeOne(it.value) })

        /** The code, and after an at sign the time of the failure; the older format had the code alone. */
        private fun encodeOne(f: SubFailure): String = if (f.atMs > 0) f.code + "@" + f.atMs else f.code

        /** Damaged or foreign data gives an empty map, never a crash; entries with a bad id or code are dropped. */
        fun decode(text: String?): Map<Long, SubFailure> {
            if (text.isNullOrBlank()) return emptyMap()
            val stored = runCatching { Json.decodeFromString(serializer, text) }.getOrElse { return emptyMap() }
            return stored.mapNotNull { (id, text) ->
                val key = id.toLongOrNull() ?: return@mapNotNull null
                val code = text.substringBefore('@')
                val at = text.substringAfter('@', "").toLongOrNull() ?: 0L
                if (code.isBlank() || code.length > MAX_CODE || at < 0) null else key to SubFailure.fromCode(code, at)
            }.toMap()
        }
    }
}
