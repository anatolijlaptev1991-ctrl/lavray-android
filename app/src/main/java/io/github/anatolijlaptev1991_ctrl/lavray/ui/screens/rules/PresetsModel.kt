// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ActivatePresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CheckRouteRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CheckRouteResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CreatePresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeletePresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ExportPresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ExportPresetResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportPresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ResetBuiltinPresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SniffingMode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdatePresetRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.diagnosticsCheckRoute
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingActivatePreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingCreatePreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingDeletePreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingExportPreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingImportPreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingListPresets
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingResetBuiltinPreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingUpdatePreset
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

/** One-shot results the screen turns into a toast. */
sealed interface PresetsEvent {
    data object Activated : PresetsEvent
    data object Saved : PresetsEvent
    data object Copied : PresetsEvent
    data object Deleted : PresetsEvent
    data object Reset : PresetsEvent
    data object Exported : PresetsEvent
    data class Imported(val warnings: Int) : PresetsEvent
    data object Unreadable : PresetsEvent
    /** The preset was changed elsewhere (revision mismatch): the list was reloaded, the edit must be repeated. */
    data object Conflict : PresetsEvent
    data object RuleExists : PresetsEvent

    /** The core refused an entry or an edit as invalid (not a link or a file: the wording of "check the file" does not fit). */
    data object EditRejected : PresetsEvent
    data class Failed(val kind: ErrorKind) : PresetsEvent
}

/** Result of one core step: done, refused because the preset changed meanwhile, or failed. */
private sealed interface Step<out T> {
    data class Ok<T>(val value: T) : Step<T>
    data object Conflict : Step<Nothing>
    data class Failed(val kind: ErrorKind) : Step<Nothing>
}

/** A copy made by the core and whether it was activated in the same step. */
private class Made(val preset: RoutingPreset, val needsRestart: Boolean)

/**
 * Route presets: loading, activation, edits, copies, deletion, reset and file exchange. Every change goes
 * through one lock, so a second edit never starts with the revision of the first (the core answers ABORTED to a
 * stale revision, which becomes a "changed elsewhere" notice and a reload). State is immutable and reduced by
 * [PresetsReducer] off the main thread.
 */
class PresetsModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val restart: RestartHub,
) {
    private val actions = Channel<PresetsAction>(Channel.UNLIMITED)
    private val mutable = MutableStateFlow(PresetsState())
    private val mutableEvents = MutableSharedFlow<PresetsEvent>(extraBufferCapacity = 8)
    private val lock = Mutex()

    /** Presets as last read or written, updated at once so a queued edit starts from the newest revision. */
    @Volatile private var known: Map<String, RoutingPreset> = emptyMap()
    @Volatile private var activeId: String = ""

    val state: StateFlow<PresetsState> = mutable.asStateFlow()
    val events: SharedFlow<PresetsEvent> = mutableEvents.asSharedFlow()

    init {
        scope.launch(Dispatchers.Default) {
            for (action in actions) mutable.value = PresetsReducer.reduce(mutable.value, action)
        }
    }

    fun dispatch(action: PresetsAction) {
        actions.trySend(action)
    }

    fun refresh() {
        scope.launch(Dispatchers.IO) { load() }
    }

    private suspend fun load() {
        when (val r = session.call { routingListPresets() }) {
            is CoreResult.Fail -> dispatch(PresetsAction.Failed(r.kind))
            is CoreResult.Ok -> {
                known = r.value.presets.associateBy { it.id }
                activeId = PresetCatalog.effectiveActiveId(PresetCatalog.sorted(r.value.presets), r.value.activePresetId)
                dispatch(PresetsAction.Loaded(r.value.presets, r.value.activePresetId))
            }
        }
    }

    // ---- operations (serialized)

    fun activate(id: String) = op {
        exec({ routingActivatePreset(ActivatePresetRequest(id)) }) { r ->
            activeId = id
            dispatch(PresetsAction.Active(id))
            if (r.needsProfileRestart) restart.need()
            load()
            PresetsEvent.Activated
        }
    }

    /** One edit of a custom preset. A built-in one is read only, and an edit that changes nothing is not sent. */
    fun update(id: String, patch: PresetPatch) = op {
        val base = known[id]
        val next = base?.takeIf { !it.builtin }?.let { PresetDraft.apply(it, patch) }
        if (base == null || next == null || !PresetDraft.isChanged(base, next)) return@op null
        val outcome = exec({ routingUpdatePreset(UpdatePresetRequest(next, base.revision)) }) { r ->
            val saved = r.preset ?: next
            known = known + (id to saved)
            dispatch(PresetsAction.Replace(saved))
            if (r.needsProfileRestart) restart.need()
            PresetsEvent.Saved
        }
        if (outcome is PresetsEvent.Failed && outcome.kind == ErrorKind.InvalidInput) PresetsEvent.EditRejected else outcome
    }

    /** A copy of [sourceId] under [name]; [activateIt] makes it the active preset at once. */
    fun copy(sourceId: String, name: String?, activateIt: Boolean) = op {
        exec({ makeCopy(sourceId, name, activateIt) }) { made ->
            if (made.needsRestart) restart.need()
            if (activateIt) { activeId = made.preset.id; dispatch(PresetsAction.Active(made.preset.id)) }
            load()
            PresetsEvent.Copied
        }
    }

    private suspend fun LavrayClient.makeCopy(sourceId: String, name: String?, activateIt: Boolean): Made {
        val created = routingCreatePreset(CreatePresetRequest(builtinSourceId = sourceId))
        // Name and activation are separate calls: if one fails the copy is removed again, so "failed" is what the list shows too.
        return afterCreate(created, undo = { routingDeletePreset(DeletePresetRequest(it.id)) }) { c ->
            var made = c
            val wanted = name?.trim().orEmpty()
            if (wanted.isNotEmpty() && wanted != made.displayName) {
                val res = routingUpdatePreset(UpdatePresetRequest(PresetDraft.apply(made, PresetPatch(displayName = wanted)), made.revision))
                made = res.preset ?: made
            }
            val restartNeeded = if (activateIt) routingActivatePreset(ActivatePresetRequest(made.id)).needsProfileRestart else false
            Made(made, restartNeeded)
        }
    }

    fun createEmpty(name: String) = op {
        val values = RoutingPreset(
            displayName = name.trim(),
            defaultOutbound = Outbound.OUTBOUND_PROXY,
            sniffingMode = SniffingMode.SNIFFING_FOR_ROUTING,
        )
        exec({
            val made = routingCreatePreset(CreatePresetRequest(preset = values))
            afterCreate(made, undo = { routingDeletePreset(DeletePresetRequest(it.id)) }) {
                Made(made, routingActivatePreset(ActivatePresetRequest(made.id)).needsProfileRestart)
            }
        }) { made ->
            if (made.needsRestart) restart.need()
            activeId = made.preset.id
            dispatch(PresetsAction.Active(made.preset.id))
            load()
            PresetsEvent.Saved
        }
    }

    fun remove(id: String) = op {
        exec({ routingDeletePreset(DeletePresetRequest(id)) }) { r ->
            if (r.needsProfileRestart) restart.need()
            load()
            PresetsEvent.Deleted
        }
    }

    /** Puts a copy of a built-in preset back to the original's rules; the restart question follows the live tunnel. */
    fun reset(id: String) = op {
        val wasActive = id == activeId
        exec({
            routingResetBuiltinPreset(ResetBuiltinPresetRequest(id))
            wasActive && tunnelIsLive()
        }) { needsRestart ->
            if (needsRestart) restart.need()
            load()
            PresetsEvent.Reset
        }
    }

    private suspend fun LavrayClient.tunnelIsLive(): Boolean {
        val st = runCatching { coreGetStatus() }.getOrNull() ?: return false
        return st.state == CoreState.CORE_STATE_CONNECTED || st.state == CoreState.CORE_STATE_CONNECTING
    }

    fun importText(json: String) = op {
        exec({ routingImportPreset(ImportPresetRequest(json)) }) { r ->
            load()
            PresetsEvent.Imported(r.warnings.size)
        }
    }

    /** The preset as a file's text and a suggested file name; null (and a notice) when the core refused. */
    suspend fun exportOf(id: String): ExportPresetResponse? = when (val r = session.call { routingExportPreset(ExportPresetRequest(id)) }) {
        is CoreResult.Ok -> r.value
        is CoreResult.Fail -> { mutableEvents.tryEmit(PresetsEvent.Failed(r.kind)); null }
    }

    fun notify(event: PresetsEvent) {
        mutableEvents.tryEmit(event)
    }

    /** Where the core would send traffic to [address] under the preset in force, and by which rule. */
    suspend fun checkRoute(address: String): CoreResult<CheckRouteResponse> =
        session.call { diagnosticsCheckRoute(CheckRouteRequest(address = address.trim())) }

    // ---- plumbing

    private fun op(block: suspend () -> PresetsEvent?) {
        scope.launch(Dispatchers.IO) {
            lock.withLock {
                dispatch(PresetsAction.Busy(true))
                try {
                    block()?.let { mutableEvents.tryEmit(it) }
                } finally {
                    dispatch(PresetsAction.Busy(false))
                }
            }
        }
    }

    /** Runs one core call and turns its outcome into an event; a refused or failed edit reloads the list. */
    private suspend fun <T> exec(block: suspend LavrayClient.() -> T, onOk: suspend (T) -> PresetsEvent?): PresetsEvent? =
        when (val s = step(block)) {
            is Step.Ok -> onOk(s.value)
            Step.Conflict -> { load(); PresetsEvent.Conflict }
            is Step.Failed -> { load(); PresetsEvent.Failed(s.kind) }
        }

    private suspend fun <T> step(block: suspend LavrayClient.() -> T): Step<T> {
        val r = session.call {
            try {
                Step.Ok(block())
            } catch (e: ApiException) {
                if (e.code == ConnectCode.ABORTED) Step.Conflict else throw e
            }
        }
        return when (r) {
            is CoreResult.Ok -> r.value
            is CoreResult.Fail -> Step.Failed(r.kind)
        }
    }
}
