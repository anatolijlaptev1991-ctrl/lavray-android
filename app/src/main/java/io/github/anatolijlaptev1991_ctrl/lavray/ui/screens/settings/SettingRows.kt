// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField

/** A titled block with a hint and chips below it (one choice among a few). */
@Composable
internal fun ChoiceBlock(title: String, hint: String? = null, chips: @Composable () -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        LavText(title, style = Lav.type.small, color = Lav.colors.textSecondary)
        if (hint != null) LavText(hint, style = Lav.type.caption, color = Lav.colors.textTertiary)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { chips() }
    }
}

/**
 * A whole-number setting. The text is checked as it is typed and the Save button appears only for a changed value that
 * passes [parse]; the core never sees a number outside its range.
 */
@Composable
internal fun NumberSettingRow(
    title: String,
    hint: String?,
    current: Long,
    parse: (String) -> NumberInput,
    rangeError: String,
    onCommit: (Long) -> Unit,
) {
    var text by remember(current) { mutableStateOf(current.toString()) }
    val parsed = parse(text)
    val error = when (parsed) {
        NumberInput.NotANumber -> stringResource(R.string.set_intErr)
        NumberInput.OutOfRange -> rangeError
        is NumberInput.Valid -> null
    }
    Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        LavTextField(
            text, { text = it }, label = title, mono = true, error = error,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        )
        if (hint != null) LavText(hint, style = Lav.type.caption, color = Lav.colors.textTertiary)
        val valid = parsed as? NumberInput.Valid
        if (valid != null && valid.value != current) {
            LavButton(stringResource(R.string.common_save), { onCommit(valid.value) }, variant = ButtonVariant.Primary)
        }
    }
}

/** A multi-line text setting (one value per line) checked by [validate], which returns an error text or null. */
@Composable
internal fun LinesSettingRow(
    title: String,
    hint: String?,
    current: String,
    validate: (String) -> String?,
    onCommit: (String) -> Unit,
) {
    var text by remember(current) { mutableStateOf(current) }
    val error = validate(text)
    Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        LavTextField(text, { text = it }, label = title, mono = true, singleLine = false, minLines = 3, error = error)
        if (hint != null) LavText(hint, style = Lav.type.caption, color = Lav.colors.textTertiary)
        if (error == null && text != current) {
            LavButton(stringResource(R.string.common_save), { onCommit(text.trim()) }, variant = ButtonVariant.Primary)
        }
    }
}
