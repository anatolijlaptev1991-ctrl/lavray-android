// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
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
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavErrorState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSkeletonRows
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable

/**
 * DNS: the resolution modes of the desktop (auto, through the tunnel, DoH, DoT, local), own servers, and the
 * advanced JSON editor with the core's check before saving. A change that needs a core restart shows the shared
 * restart banner. The settings belong to the active route preset.
 */
@Composable
fun DnsScreen() {
    val env = LocalEnv.current
    val scope = rememberCoroutineScope()
    val model = remember(env) { DnsModel(env.session, scope, env.restart) }
    val state by model.state.collectAsStateWithLifecycle()
    val error = state.error

    LifecycleEventEffect(Lifecycle.Event.ON_START) { model.refresh() }
    DnsToasts(model)

    when {
        state.loading -> LavSkeletonRows(4, stringResource(R.string.lazy_loading), Modifier.padding(16.dp))
        error != null && state.dns == null -> LavErrorState(
            stringResource(R.string.core_err_t), stringResource(error.textRes()),
            actions = { LavButton(stringResource(R.string.err_retry), { model.refresh() }, variant = ButtonVariant.Primary) },
        )
        else -> Content(state, model)
    }
}

@Composable
private fun Content(state: DnsState, model: DnsModel) {
    Column(Modifier.fillMaxSize()) {
        LavScreenColumn(Modifier.weight(1f)) {
            ModesCard(state, model)
            AdvancedCard(state, model)
        }
    }
}

@Composable
private fun ModesCard(state: DnsState, model: DnsModel) {
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            LavSectionLabel(stringResource(R.string.dns_modes))
            DnsCard.entries.forEach { card ->
                ModeOption(card, state.activeCard == card.id, enabled = !state.busy) { model.pick(card) }
            }
            LavBanner(StatusTone.Neutral, stringResource(R.string.dns_warn_t), Modifier.padding(top = 8.dp), text = stringResource(R.string.dns_warn_phone))
        }
    }
}

/** One mode: title and explanation, a check mark when it is the one in force (not by colour alone). */
@Composable
private fun ModeOption(card: DnsCard, selected: Boolean, enabled: Boolean, onPick: () -> Unit) {
    val c = Lav.colors
    val shape = RoundedCornerShape(Radius.md)
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp)
            .lavClickable(onPick, shape, enabled = enabled, role = Role.RadioButton)
            .semantics { this.selected = selected }
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            LavText(stringResource(card.title), style = if (selected) Lav.type.bodyStrong else Lav.type.body)
            LavText(stringResource(card.desc), style = Lav.type.small, color = c.textTertiary)
        }
        if (selected) LavIconView(LavIcon.Check, c.accent, size = 20.dp)
    }
}

@Composable
private fun AdvancedCard(state: DnsState, model: DnsModel) {
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                LavSectionLabel(stringResource(R.string.dns_adv), Modifier.weight(1f))
                LavButton(
                    if (state.advanced) stringResource(R.string.dns_custom) else "JSON",
                    { model.dispatch(DnsAction.Advanced(!state.advanced)) },
                )
            }
            if (state.advanced) JsonEditor(state, model) else CustomServers(state, model)
        }
    }
}

@Composable
private fun CustomServers(state: DnsState, model: DnsModel) {
    LavTextField(
        state.remote, { model.dispatch(DnsAction.Remote(it)) }, label = stringResource(R.string.dns_custom), mono = true,
        placeholder = "https://dns.google/dns-query",
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
    )
    LavButton(
        stringResource(R.string.common_save), { model.saveCustom() },
        variant = ButtonVariant.Primary, loading = state.busy, enabled = state.remote.isNotBlank(),
    )
}

@Composable
private fun JsonEditor(state: DnsState, model: DnsModel) {
    LavTextField(
        state.json, { model.dispatch(DnsAction.Json(it)) }, label = "DNS JSON", mono = true,
        singleLine = false, minLines = JSON_LINES,
    )
    if (state.fakeDns) LavText(stringResource(R.string.dns_fakeipAdv), style = Lav.type.small, color = Lav.colors.textSecondary)
    if (state.invalid) Problems(state.problems)
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        LavButton(stringResource(R.string.dns_validate), { model.validate() }, enabled = !state.busy && state.json.isNotBlank())
        LavButton(
            stringResource(R.string.common_save), { model.saveAdvanced() },
            variant = ButtonVariant.Primary, loading = state.busy, enabled = state.json.isNotBlank(),
        )
    }
}

/** What the core did not accept in the JSON, one "path: reason" per line, as the core worded it. */
@Composable
private fun Problems(messages: List<String>) {
    LavBanner(
        StatusTone.Error, stringResource(R.string.da_json_invalid),
        text = messages.joinToString("\n").ifEmpty { null },
    )
}

@Composable
private fun DnsToasts(model: DnsModel) {
    val env = LocalEnv.current
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(model) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            model.events.collect { e -> env.toast.show(dnsEventText(env.appContext, e)) }
        }
    }
}

/** The toast follows the core's answer to this save (needsRestart), not any global state. */
private fun dnsEventText(ctx: Context, e: DnsEvent): String = when (e) {
    is DnsEvent.Saved -> ctx.getString(savedOrPending(e.needsRestart, R.string.toast_saved))
    DnsEvent.JsonValid -> ctx.getString(R.string.dns_jsonOk)
    is DnsEvent.Failed -> ctx.getString(e.kind.textRes())
}

private const val JSON_LINES = 10
