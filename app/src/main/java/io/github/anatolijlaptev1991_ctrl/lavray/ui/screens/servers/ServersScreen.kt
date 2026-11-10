// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TestLatencyRequest
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavErrorState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSkeletonRows
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.ServerSort
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.rememberVpnConnect

/**
 * Servers: search, filters, groups, latency tests with live results and a Stop button, selection with bulk
 * actions, and a per-server menu (connect, favourite, copy / share / QR, duplicate, move, reorder, delete).
 * The list is a LazyColumn with stable keys and content types; filtering, sorting and grouping happen in the
 * reducer off the main thread, so 2000 servers cost the same to draw as 20.
 */
@Composable
fun ServersScreen() {
    val env = LocalEnv.current
    val model = env.servers
    val state by model.state.collectAsStateWithLifecycle()
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    val phase = phaseOf(vpn.state)
    val connect = rememberVpnConnect()
    var sheet by remember { mutableStateOf<ServerSheet?>(null) }
    val scope = rememberCoroutineScope()
    val current by rememberUpdatedState(state)

    LaunchedEffect(Unit) {
        model.configure(env.settings.sort, env.settings.groupBySubscription, env.settings.selectedProfileId)
        model.refresh()
    }
    LaunchedEffect(phase) { model.refreshActive(phase == Phase.On || phase == Phase.Connecting) }
    LaunchedEffect(env.settings.selectedProfileId) { model.dispatch(ServersAction.Selected(env.settings.selectedProfileId)) }
    EventToasts(model)
    BackHandler(enabled = state.selecting) { model.dispatch(ServersAction.ClearSelection) }

    val onClick = remember(model) {
        { row: ServerRow ->
            if (current.selecting) model.dispatch(ServersAction.Toggle(row.id)) else connect(row.id)
        }
    }
    val onLong = remember(model) { { row: ServerRow -> model.dispatch(ServersAction.Toggle(row.id)) } }
    val onMenu = remember { { row: ServerRow -> sheet = ServerSheet.RowMenu(row) } }
    val onFavorite = remember(model) { { row: ServerRow -> model.toggleFavorite(row) } }

    val sheetEnv = SheetEnv(
        state = state, model = model,
        canReorder = state.sort == ServerSort.Default && !state.filtering,
        compact = env.settings.compactList,
        onCompact = { on -> env.update { it.copy(compactList = on) } },
        onSort = { s -> env.update { it.copy(sort = s) }; model.dispatch(ServersAction.Sort(s)) },
        onGroupBy = { on -> env.update { it.copy(groupBySubscription = on) }; model.dispatch(ServersAction.GroupBy(on)) },
        onConnect = connect,
        onCopied = { env.toast.show(env.appContext.getString(R.string.toast_copied)) },
        show = { sheet = it },
        scope = scope,
    )

    Column(Modifier.fillMaxSize()) {
        Toolbar(state, onAdd = { env.importer.open() }, onView = { sheet = ServerSheet.View }, model = model)
        FilterRows(state, model)
        TestBar(state, model)
        Box(Modifier.weight(1f).fillMaxWidth()) {
            ListBody(state, env.settings.compactList, phase == Phase.Connecting, onClick, onLong, onMenu, onFavorite, model)
        }
        if (state.selecting) SelectionBar(state, model, onMove = { sheet = ServerSheet.Move(state.selection) }, onDelete = { sheet = ServerSheet.ConfirmDelete(state.selection) })
    }
    ServerSheets(sheet, sheetEnv)
}

@Composable
private fun EventToasts(model: ServersModel) {
    val env = LocalEnv.current
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(model) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            model.events.collect { e ->
                val ctx = env.appContext
                val text = when (e) {
                    is ServersEvent.Cloned -> ctx.getString(R.string.srv_cloned, e.count.toString())
                    is ServersEvent.Deleted -> ctx.getString(R.string.toast_deletedN, e.count.toString())
                    ServersEvent.Moved -> ctx.getString(R.string.mv_done)
                    ServersEvent.GroupSaved -> ctx.getString(R.string.toast_saved)
                    ServersEvent.InUse -> ctx.getString(R.string.srv_inUse)
                    is ServersEvent.Failed -> ctx.getString(e.kind.textRes())
                }
                env.toast.show(text)
            }
        }
    }
}

@Composable
private fun Toolbar(state: ServersState, onAdd: () -> Unit, onView: () -> Unit, model: ServersModel) {
    var text by remember { mutableStateOf(state.query) }
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        LavTextField(
            text, { text = it; model.dispatch(ServersAction.Query(it)) },
            modifier = Modifier.weight(1f), placeholder = stringResource(R.string.prof_searchPh), leading = LavIcon.Search,
        )
        LavIconButton(LavIcon.Plus, stringResource(R.string.quick_import), onAdd, tint = Lav.colors.accent)
        LavIconButton(LavIcon.Sliders, stringResource(R.string.srv_view), onView)
    }
}

@Composable
private fun FilterRows(state: ServersState, model: ServersModel) {
    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        FilterChip(R.string.filter_all, state.filter == ServerFilter.All) { model.dispatch(ServersAction.Filter(ServerFilter.All)) }
        FilterChip(R.string.filter_fav, state.filter == ServerFilter.Favorites) { model.dispatch(ServersAction.Filter(ServerFilter.Favorites)) }
        FilterChip(R.string.filter_fast, state.filter == ServerFilter.Fast) { model.dispatch(ServersAction.Filter(ServerFilter.Fast)) }
        FilterChip(R.string.filter_down, state.filter == ServerFilter.Down) { model.dispatch(ServersAction.Filter(ServerFilter.Down)) }
    }
    if (state.groups.size > 1) {
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            FilterChip(R.string.filter_subAll, state.scope == GroupScope.All) { model.dispatch(ServersAction.Scope(GroupScope.All)) }
            FilterChip(R.string.srv_manual, state.scope == GroupScope.Manual) { model.dispatch(ServersAction.Scope(GroupScope.Manual)) }
            state.groups.forEach { g ->
                LavChip(g.name, state.scope == GroupScope.One(g.id), { model.dispatch(ServersAction.Scope(GroupScope.One(g.id))) })
            }
        }
    }
}

@Composable
private fun FilterChip(label: Int, selected: Boolean, onClick: () -> Unit) = LavChip(stringResource(label), selected, onClick)

@Composable
private fun TestBar(state: ServersState, model: ServersModel) {
    val c = Lav.colors
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (state.test.running) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                LavText(
                    stringResource(R.string.test_progress, state.test.done.toString(), state.test.total.toString()),
                    style = Lav.type.small, color = c.textSecondary,
                )
                val fraction = if (state.test.total > 0) (state.test.done.toFloat() / state.test.total).coerceIn(0f, 1f) else 0f
                Box(Modifier.fillMaxWidth().height(4.dp).background(c.bgSurface2, RoundedCornerShape(2.dp))) {
                    Box(Modifier.fillMaxWidth(fraction).height(4.dp).background(c.accent, RoundedCornerShape(2.dp)))
                }
            }
            LavButton(stringResource(R.string.test_stop), { model.stopTest() }, icon = LavIcon.Stop)
        } else {
            LavText(
                if (state.loading) "" else pluralStringResource(R.plurals.pl_srv, state.visibleCount, state.visibleCount), Modifier.weight(1f),
                style = Lav.type.small, color = c.textTertiary,
            )
            LavButton(
                stringResource(R.string.test_all),
                { model.startTest(state.items.mapNotNull { (it as? ListItem.Row)?.row?.id }, TestLatencyRequest.Mode.MODE_TCP_PING) },
                icon = LavIcon.Gauge, enabled = state.visibleCount > 0,
            )
        }
    }
}

@Composable
private fun ListBody(
    state: ServersState,
    compact: Boolean,
    connecting: Boolean,
    onClick: (ServerRow) -> Unit,
    onLong: (ServerRow) -> Unit,
    onMenu: (ServerRow) -> Unit,
    onFavorite: (ServerRow) -> Unit,
    model: ServersModel,
) {
    val env = LocalEnv.current
    val fmt = rememberUnitFormat()
    val listState = rememberLazyListState()
    when {
        state.loading -> LavSkeletonRows(6, stringResource(R.string.lazy_loading), Modifier.padding(16.dp))
        state.error != null && state.rows.isEmpty() -> LavErrorState(
            stringResource(R.string.core_err_t), stringResource(state.error.textRes()),
            actions = { LavButton(stringResource(R.string.err_retry), { model.refresh() }, variant = ButtonVariant.Primary) },
        )
        state.rows.isEmpty() -> LavEmptyState(
            stringResource(R.string.srv_empty_t), text = stringResource(R.string.srv_empty_d), icon = LavIcon.Layers,
            actions = { LavButton(stringResource(R.string.cx_add), { env.importer.open() }, variant = ButtonVariant.Primary, icon = LavIcon.Plus) },
        )
        state.items.isEmpty() -> LavEmptyState(
            stringResource(R.string.prof_empty_t), text = stringResource(R.string.prof_empty_d), icon = LavIcon.Search,
            actions = {
                LavButton(stringResource(R.string.prof_reset), {
                    model.dispatch(ServersAction.Query(""))
                    model.dispatch(ServersAction.Filter(ServerFilter.All))
                    model.dispatch(ServersAction.Scope(GroupScope.All))
                })
            },
        )
        else -> {
            LazyColumn(
                Modifier.fillMaxSize(),
                state = listState,
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 8.dp, vertical = 4.dp),
            ) {
                items(state.items, key = { it.key }, contentType = { if (it is ListItem.Header) 0 else 1 }) { item ->
                    when (item) {
                        is ListItem.Header -> GroupHeaderItem(item.group, item.count)
                        is ListItem.Row -> {
                            val row = item.row
                            ServerRowItem(
                                row = row,
                                flags = RowFlags(
                                    active = row.id == state.activeId, connecting = connecting,
                                    current = row.id == state.selectedId, selecting = state.selecting,
                                    checked = row.id in state.selection, compact = compact,
                                ),
                                fmt = fmt, onClick = onClick, onLongClick = onLong, onMenu = onMenu, onFavorite = onFavorite,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SelectionBar(state: ServersState, model: ServersModel, onMove: () -> Unit, onDelete: () -> Unit) {
    val c = Lav.colors
    Row(
        Modifier.fillMaxWidth().background(c.bgSurface2).padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        LavIconButton(LavIcon.Close, stringResource(R.string.srv_selClear), { model.dispatch(ServersAction.ClearSelection) })
        LavText(stringResource(R.string.srv_selected, state.selection.size.toString()), Modifier.weight(1f), style = Lav.type.bodyStrong)
        LavIconButton(LavIcon.List, stringResource(R.string.srv_select_all), { model.dispatch(ServersAction.SelectAllVisible) })
        LavIconButton(LavIcon.Duplicate, stringResource(R.string.srv_menu_clone), { model.clone(state.selection); model.dispatch(ServersAction.ClearSelection) })
        LavIconButton(LavIcon.Folder, stringResource(R.string.srv_move), onMove)
        LavIconButton(LavIcon.Trash, stringResource(R.string.srv_delSel), onDelete, tint = c.errorText)
    }
}
