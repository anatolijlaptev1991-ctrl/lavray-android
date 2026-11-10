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
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartHub
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
    val error: ErrorKind? = null,
    /** The settings could not be read: the screen shows the error and no values, never the built-in defaults as if they were saved. */
    val readFailed: Boolean = false,
) {
    /** The values on screen are the core's own. */
    val ready: Boolean get() = loaded && !readFailed

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
class SettingsModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val restart: RestartHub,
) {
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
                        descriptors = descriptors.associateBy { d -> d.key }, error = null, readFailed = false,
                    )
                }
                is CoreResult.Fail -> mutable.update { it.copy(loaded = true, error = read.kind, readFailed = true) }
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
                is CoreResult.Ok -> when (val outcome = SettingsWrite.outcome(key, r.value)) {
                    is SettingsWrite.Outcome.Accepted -> noteRestart(key, outcome.coreAskedForRestart)
                    // The core did not take the key (a version that does not know it): the screen must not keep showing it as saved.
                    SettingsWrite.Outcome.Rejected -> mutable.update { putBack(it, key, before).copy(error = ErrorKind.InvalidInput) }
                }
                is CoreResult.Fail -> mutable.update { putBack(it, key, before).copy(error = r.kind) }
            }
        }
    }

    private fun putBack(s: SettingsState, key: String, before: SettingValue?): SettingsState =
        s.copy(values = if (before == null) s.values - key else s.values + (key to before))

    /** A saved key that only the next tunnel reads is offered a reconnect, whatever the core flagged. */
    private fun noteRestart(key: String, coreAskedForRestart: Boolean) {
        when {
            coreAskedForRestart -> restart.need(PendingReason.Tunnel)
            SettingKeys.appliesOnNextTunnel(key) -> restart.needIfLive(PendingReason.Tunnel)
        }
    }

    /** Puts every setting of this screen back to the core's default. */
    fun resetAll() {
        scope.launch(Dispatchers.IO) {
            val keys = mutable.value.available.toList()
            when (val r = session.call { settingsResetSettings(ResetSettingsRequest(keys)) }) {
                is CoreResult.Ok -> {
                    mutable.update { it.copy(values = emptyMap()) }
                    // A reset changes every key of this screen, so what only the next tunnel reads is pending too.
                    if (r.value.needsProfileRestart || keys.any(SettingKeys::appliesOnNextTunnel)) restart.needIfLive(PendingReason.Tunnel)
                    load()
                }
                is CoreResult.Fail -> mutable.update { it.copy(error = r.kind) }
            }
        }
    }

    fun dismissError() = mutable.update { it.copy(error = null) }
}
