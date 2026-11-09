// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ResetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingDescriptor
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsDescribeSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsGetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsResetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsSetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

@Immutable
data class SettingsState(
    val loaded: Boolean = false,
    val available: Set<String> = SettingKeys.SHOWN.toSet(),
    val values: Map<String, SettingValue> = emptyMap(),
    val descriptors: Map<String, SettingDescriptor> = emptyMap(),
    /** A saved setting that only takes effect when the tunnel is built again. */
    val reconnectNeeded: Boolean = false,
    val error: ErrorKind? = null,
) {
    fun bool(key: String, fallback: Boolean = false): Boolean =
        SettingsMapper.bool(values[key], SettingsMapper.defaultBool(descriptors[key]) ?: fallback)

    fun int(key: String, fallback: Long = 0L): Long =
        SettingsMapper.int(values[key], SettingsMapper.defaultInt(descriptors[key]) ?: fallback)

    fun string(key: String, fallback: String = ""): String =
        SettingsMapper.string(values[key], SettingsMapper.defaultString(descriptors[key]) ?: fallback)

    fun has(key: String): Boolean = key in available
}

/**
 * Core settings of the phone's Settings screen. The catalog comes from DescribeSettings (which keys exist, their
 * defaults and whether they need a reconnect); values are read once and written one key at a time, each write on
 * the IO dispatcher. The new value is shown at once and put back if the core refuses it.
 */
class SettingsModel(private val session: CoreSession, private val scope: CoroutineScope) {
    private val mutable = MutableStateFlow(SettingsState())
    val state: StateFlow<SettingsState> = mutable.asStateFlow()

    fun load() {
        scope.launch(Dispatchers.IO) {
            val described = session.call { settingsDescribeSettings() }
            val descriptors = (described as? CoreResult.Ok)?.value?.descriptors.orEmpty()
            val keys = SettingKeys.available(descriptors)
            val read = session.call { settingsGetSettings(GetSettingsRequest(SettingKeys.SHOWN.filter { it in keys })) }
            when (read) {
                is CoreResult.Ok -> mutable.update {
                    it.copy(
                        loaded = true, available = keys, values = read.value.values,
                        descriptors = descriptors.associateBy { d -> d.key }, error = null,
                    )
                }
                is CoreResult.Fail -> mutable.update { it.copy(loaded = true, error = read.kind) }
            }
        }
    }

    fun setBool(key: String, value: Boolean) = write(key, SettingValue(boolValue = value))

    fun setInt(key: String, value: Long) = write(key, SettingValue(intValue = value))

    fun setString(key: String, value: String) = write(key, SettingValue(stringValue = value))

    private fun write(key: String, value: SettingValue) {
        val before = mutable.value.values[key]
        mutable.update { it.copy(values = it.values + (key to value), error = null) }
        scope.launch(Dispatchers.IO) {
            when (val r = session.call { settingsSetSettings(SetSettingsRequest(mapOf(key to value))) }) {
                is CoreResult.Ok -> if (r.value.needsProfileRestart) mutable.update { it.copy(reconnectNeeded = true) }
                is CoreResult.Fail -> mutable.update {
                    val restored = if (before == null) it.values - key else it.values + (key to before)
                    it.copy(values = restored, error = r.kind)
                }
            }
        }
    }

    /** Puts every setting of this screen back to the core's default. */
    fun resetAll() {
        scope.launch(Dispatchers.IO) {
            val keys = mutable.value.available.toList()
            when (val r = session.call { settingsResetSettings(ResetSettingsRequest(keys)) }) {
                is CoreResult.Ok -> {
                    mutable.update { it.copy(values = emptyMap(), reconnectNeeded = it.reconnectNeeded || r.value.needsProfileRestart) }
                    load()
                }
                is CoreResult.Fail -> mutable.update { it.copy(error = r.kind) }
            }
        }
    }

    fun dismissReconnect() = mutable.update { it.copy(reconnectNeeded = false) }
    fun dismissError() = mutable.update { it.copy(error = null) }
}
