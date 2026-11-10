// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.runtime.Immutable
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * What the "connect when the phone starts" switch shows. [value] is what the service says is stored (null until read, or
 * when it cannot be read): the switch never shows a value nobody confirmed. [saving] holds the switch while a write is
 * under way; [writeFailed] says the last write did not reach the storage, so [value] is the old one.
 */
@Immutable
data class AutoConnectUi(
    val value: Boolean? = null,
    val readFailed: Boolean = false,
    val saving: Boolean = false,
    val writeFailed: Boolean = false,
)

/**
 * The switch of the :vpn process's setting. A tap writes through [write], which returns whether the value is really in
 * force afterwards (the service rolls a write back when the boot receiver cannot follow it). The switch moves to the new
 * value only on that answer; on a refusal it stays where it was and says so; when the answer never came (the service
 * died, a binder error) the stored value is read again instead of guessed.
 */
class AutoConnectSwitch(
    private val read: suspend () -> Boolean,
    private val write: suspend (Boolean) -> Boolean,
) {
    private val mutable = MutableStateFlow(AutoConnectUi())
    val state: StateFlow<AutoConnectUi> = mutable.asStateFlow()
    private val lock = Mutex()

    suspend fun load() = lock.withLock {
        val stored = readOrNull()
        mutable.update { it.copy(value = stored, readFailed = stored == null, writeFailed = false) }
    }

    /** Ignored while a write is under way (the switch is disabled then; a second call can only be a double delivery). */
    suspend fun set(wanted: Boolean) {
        if (!lock.tryLock()) return
        try {
            mutable.update { it.copy(saving = true, writeFailed = false) }
            val result = writeOrNull(wanted)
            when (result) {
                true -> mutable.update { AutoConnectUi(value = wanted) }
                false -> mutable.update { it.copy(saving = false, writeFailed = true) }
                null -> {
                    val stored = readOrNull()
                    mutable.update { AutoConnectUi(value = stored, readFailed = stored == null, writeFailed = stored != wanted) }
                }
            }
        } finally {
            lock.unlock()
        }
    }

    private suspend fun writeOrNull(wanted: Boolean): Boolean? = try {
        write(wanted)
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        null
    }

    private suspend fun readOrNull(): Boolean? = try {
        read()
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        null
    }
}
