// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavErrorState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSkeletonRows
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat

/**
 * Subscriptions: a card per subscription with status, traffic and expiry, update one or all with live
 * progress, settings, rollback and delete with confirmation, and the "this update removes servers" question.
 * Updates run as streams only while the screen is on show: leaving it (or the app) cancels them.
 */
@Composable
fun SubscriptionsScreen() {
    val env = LocalEnv.current
    val model = env.subscriptions
    val state by model.state.collectAsStateWithLifecycle()
    var sheet by remember { mutableStateOf<SubSheet?>(null) }

    LifecycleEventEffect(Lifecycle.Event.ON_START) { model.refresh() }
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { model.cancelRunning() }
    EventToasts(model)

    val actions = remember(model) {
        CardActions(
            onSettings = { sheet = SubSheet.Settings(it) },
            onToggleAuto = { model.toggleAuto(it) },
            onUpdate = { model.update(it.id) },
            onMenu = { sheet = SubSheet.Menu(it) },
        )
    }
    val sheetEnv = remember(model) {
        SubSheetEnv(model, show = { sheet = it }, onCopied = { env.toast.show(env.appContext.getString(R.string.toast_copied)) })
    }

    Column(Modifier.fillMaxSize()) {
        // The empty state carries its own "Add subscription" button; a second one in the toolbar would only repeat it.
        if (!isEmptyList(state)) Toolbar(state, onAdd = { sheet = SubSheet.Add }, onUpdateAll = { model.updateAll() })
        Body(state, actions, onAdd = { sheet = SubSheet.Add }, onRetry = { model.refresh() })
    }
    SubscriptionSheets(sheet, state.confirm, sheetEnv)
}

/** Loaded, no error and nothing in the list: the screen shows its empty state. */
internal fun isEmptyList(state: SubscriptionsState): Boolean = !state.loading && state.error == null && state.rows.isEmpty()

@Composable
private fun Toolbar(state: SubscriptionsState, onAdd: () -> Unit, onUpdateAll: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            LavButton(
                stringResource(if (state.busy == Busy.All) R.string.subs_updateAll_busy else R.string.subs_updateAll),
                onUpdateAll, Modifier.weight(1f), icon = LavIcon.Refresh,
                loading = state.busy == Busy.All, enabled = state.rows.isNotEmpty() && !state.isBusy,
            )
            LavButton(stringResource(R.string.subs_add), onAdd, variant = ButtonVariant.Primary, icon = LavIcon.Plus)
        }
        if (state.busy == Busy.All) {
            val name = state.rows.firstOrNull { it.id == state.progress.groupId }?.name
            val phase = stringResource(state.progress.phase.labelRes())
            LavText(if (name != null) "$name · $phase" else phase, style = Lav.type.small, color = Lav.colors.textSecondary, maxLines = 1)
        }
    }
}

@Composable
private fun Body(state: SubscriptionsState, actions: CardActions, onAdd: () -> Unit, onRetry: () -> Unit) {
    when {
        state.loading -> LavSkeletonRows(3, stringResource(R.string.lazy_loading), Modifier.padding(16.dp))
        state.error != null && state.rows.isEmpty() -> LavErrorState(
            stringResource(R.string.core_err_t), stringResource(state.error.textRes()),
            actions = { LavButton(stringResource(R.string.err_retry), onRetry, variant = ButtonVariant.Primary) },
        )
        isEmptyList(state) -> LavEmptyState(
            stringResource(R.string.subs_empty_t), text = stringResource(R.string.subs_empty_d), icon = LavIcon.Rss,
            actions = { LavButton(stringResource(R.string.subs_add), onAdd, variant = ButtonVariant.Primary, icon = LavIcon.Plus) },
        )
        else -> Cards(state, actions)
    }
}

@Composable
private fun Cards(state: SubscriptionsState, actions: CardActions) {
    val fmt = rememberUnitFormat()
    val relative = rememberRelativeTime()
    val nowMs = remember(state.rows) { System.currentTimeMillis() }
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(state.rows, key = { it.id }, contentType = { CARD_TYPE }) { row ->
            val running = state.busy == Busy.One(row.id) || (state.busy == Busy.All && state.progress.groupId == row.id)
            SubscriptionCard(
                row,
                CardEnv(fmt, relative, nowMs, busyOn = running, progress = state.progress.takeIf { running }, otherBusy = state.isBusy),
                actions,
            )
        }
    }
}

@Composable
internal fun EventToasts(model: SubscriptionsModel) {
    val env = LocalEnv.current
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(model) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            model.events.collect { e -> eventText(env.appContext, e)?.let { env.toast.show(it) } }
        }
    }
}

private fun eventText(ctx: android.content.Context, e: SubscriptionsEvent): String? = when (e) {
    is SubscriptionsEvent.Added -> ctx.getString(R.string.toast_subAdded, e.name)
    is SubscriptionsEvent.Updated -> ctx.getString(R.string.toast_subUpdated, e.name)
    is SubscriptionsEvent.UpdateFailed -> ctx.getString(e.failure.kind.textRes())
    is SubscriptionsEvent.UpdateAllDone -> updateAllText(ctx, e.tally)
    // Placeholders {n} and {name} are numbered alphabetically: n first.
    is SubscriptionsEvent.RolledBack ->
        if (e.count != null) ctx.getString(R.string.subs_rollback_done, e.count.toString(), e.name) else ctx.getString(R.string.sub_rolledNoCount, e.name)
    SubscriptionsEvent.RollbackFailed -> ctx.getString(R.string.subs_rollback_fail)
    SubscriptionsEvent.RollbackUnavailable -> ctx.getString(R.string.sub_rollbackGone)
    SubscriptionsEvent.Busy -> ctx.getString(R.string.sub_busy)
    SubscriptionsEvent.Saved -> ctx.getString(R.string.toast_saved)
    SubscriptionsEvent.Deleted -> null
    SubscriptionsEvent.InUse -> ctx.getString(R.string.srv_inUse)
    is SubscriptionsEvent.Failed -> ctx.getString(e.kind.textRes())
}

/** "updated 2, skipped 0, failed 1", plus the hint about subscriptions that wait for a confirmation. */
private fun updateAllText(ctx: android.content.Context, t: UpdateTally): String {
    // Placeholders {fail}, {ok}, {skip} are numbered alphabetically.
    val head = ctx.getString(R.string.subs_updateAll_done, t.fail.toString(), t.ok.toString(), t.skip.toString())
    return if (t.confirm > 0) head + ". " + ctx.getString(R.string.subs_updateAll_confirm, t.confirm.toString()) else head
}

private const val CARD_TYPE = 1
