// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CheckRouteResponse
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.RouteMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import kotlinx.coroutines.launch

/** Outcome shown under the field: the answer of the core, or what went wrong in words. */
private sealed interface RouteAnswer {
    data class Found(val response: CheckRouteResponse) : RouteAnswer
    data class Failed(val kind: ErrorKind) : RouteAnswer
}

/**
 * "Check a route": a domain or an address, and where the active preset sends it and by which rule. The answer
 * is announced to screen readers when it appears. Process names are not asked for on a phone.
 */
@Composable
fun RouteTesterCard(model: PresetsModel, modifier: Modifier = Modifier) {
    var query by remember { mutableStateOf("") }
    var running by remember { mutableStateOf(false) }
    var answer by remember { mutableStateOf<RouteAnswer?>(null) }
    val scope = rememberCoroutineScope()
    val check = {
        if (query.isNotBlank() && !running) {
            running = true
            scope.launch {
                answer = when (val r = model.checkRoute(query)) {
                    is CoreResult.Ok -> RouteAnswer.Found(r.value)
                    is CoreResult.Fail -> RouteAnswer.Failed(r.kind)
                }
                running = false
            }
        }
    }
    LavCard(modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            LavSectionLabel(stringResource(R.string.rt_test_t))
            LavTextField(
                query, { query = it; answer = null }, placeholder = "chat.openai.com", mono = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { check() }),
            )
            LavButton(stringResource(R.string.rt_test_run), check, variant = ButtonVariant.Primary, loading = running, enabled = query.isNotBlank())
            Answer(answer)
        }
    }
}

@Composable
private fun Answer(answer: RouteAnswer?) {
    val text = when (answer) {
        null -> return
        is RouteAnswer.Failed -> stringResource(answer.kind.textRes())
        is RouteAnswer.Found -> foundText(answer.response)
    }
    val color = if (answer is RouteAnswer.Failed) Lav.colors.errorText else Lav.colors.textPrimary
    LavText(text, Modifier.semantics { liveRegion = LiveRegionMode.Polite }, style = Lav.type.mono, color = color)
}

@Composable
private fun foundText(r: CheckRouteResponse): String {
    // The core answers "unspecified" for a tag it does not name (a custom rule's own tag): that is not "direct".
    val outbound = RouteMapper.outboundName(r.outbound)?.let { stringResource(it) } ?: "—"
    val rule = r.matchedRule?.takeIf { it.isNotBlank() }
    return if (rule != null) "$outbound · $rule" else outbound
}
