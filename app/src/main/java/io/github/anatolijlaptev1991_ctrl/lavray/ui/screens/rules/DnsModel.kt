// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetDnsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ValidateDnsJsonRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.dnsGetDns
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.dnsSetDns
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.dnsValidateDnsJson
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

sealed interface DnsEvent {
    data object Saved : DnsEvent
    data object JsonValid : DnsEvent
    data class Failed(val kind: ErrorKind) : DnsEvent
}

/**
 * DNS settings of the active route preset: load, choose a mode, set own servers, edit the JSON with the core's
 * check before it is saved. The core says when the change needs a core restart; that goes to the shared
 * [RestartHub] and its banner. All calls run off the main thread, one at a time.
 */
class DnsModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val restart: RestartHub,
) {
    private val actions = Channel<DnsAction>(Channel.UNLIMITED)
    private val mutable = MutableStateFlow(DnsState())
    private val mutableEvents = MutableSharedFlow<DnsEvent>(extraBufferCapacity = 8)
    private val lock = Mutex()

    val state: StateFlow<DnsState> = mutable.asStateFlow()
    val events: SharedFlow<DnsEvent> = mutableEvents.asSharedFlow()

    init {
        scope.launch(Dispatchers.Default) {
            for (action in actions) mutable.value = DnsReducer.reduce(mutable.value, action)
        }
    }

    fun dispatch(action: DnsAction) {
        actions.trySend(action)
    }

    fun refresh() {
        scope.launch(Dispatchers.IO) { load() }
    }

    private suspend fun load() {
        when (val r = session.call { dnsGetDns() }) {
            is CoreResult.Fail -> dispatch(DnsAction.Failed(r.kind))
            is CoreResult.Ok -> dispatch(DnsAction.Loaded(r.value))
        }
    }

    /** Picks one of the resolution modes; the JSON editor closes, as on the desktop. */
    fun pick(card: DnsCard) {
        dispatch(DnsAction.Advanced(false))
        save(DnsRequests.card(card))
    }

    fun saveCustom() {
        val s = mutable.value
        if (s.remote.isBlank()) return
        save(DnsRequests.custom(s.dns, s.remote))
    }

    /** Checks the JSON with the core; a valid text is replaced by its formatted form. */
    fun validate() {
        val text = mutable.value.json
        scope.launch(Dispatchers.IO) { lock.withLock { check(text, announce = true) } }
    }

    /** Saves the JSON, but only after the core accepted it: the problems are shown and nothing is sent otherwise. */
    fun saveAdvanced() {
        val s = mutable.value
        if (s.json.isBlank()) return
        scope.launch(Dispatchers.IO) {
            lock.withLock {
                val ok = check(s.json, announce = false)
                if (ok != null) send(DnsRequests.advanced(s.dns, ok))
            }
        }
    }

    /** The text to save when the core accepts it (its formatted form), null when it found problems or failed. */
    private suspend fun check(text: String, announce: Boolean): String? {
        dispatch(DnsAction.Busy(true))
        try {
            return when (val r = session.call { dnsValidateDnsJson(ValidateDnsJsonRequest(text)) }) {
                is CoreResult.Fail -> { mutableEvents.tryEmit(DnsEvent.Failed(r.kind)); null }
                is CoreResult.Ok -> if (r.value.valid) accepted(r.value.formatted, text, announce) else rejected(r.value.errors)
            }
        } finally {
            dispatch(DnsAction.Busy(false))
        }
    }

    private fun accepted(formatted: String?, original: String, announce: Boolean): String {
        val text = formatted ?: original
        if (text != original) dispatch(DnsAction.Json(text))
        if (announce) mutableEvents.tryEmit(DnsEvent.JsonValid)
        return text
    }

    private fun rejected(errors: List<String>): String? {
        dispatch(DnsAction.Problems(errors))
        return null
    }

    private fun save(dns: DnsSettings) {
        scope.launch(Dispatchers.IO) { lock.withLock { send(dns) } }
    }

    private suspend fun send(dns: DnsSettings) {
        dispatch(DnsAction.Busy(true))
        try {
            when (val r = session.call { dnsSetDns(SetDnsRequest(dns)) }) {
                is CoreResult.Fail -> mutableEvents.tryEmit(DnsEvent.Failed(r.kind))
                is CoreResult.Ok -> {
                    r.value.applied?.let { dispatch(DnsAction.Applied(it)) }
                    if (r.value.needsProfileRestart) restart.need()
                    mutableEvents.tryEmit(DnsEvent.Saved)
                }
            }
        } finally {
            dispatch(DnsAction.Busy(false))
        }
    }
}
