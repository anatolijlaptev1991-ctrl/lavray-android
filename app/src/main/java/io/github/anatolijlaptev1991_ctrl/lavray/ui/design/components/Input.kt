// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius

/**
 * Input of section 6: label above, 48 dp field on bg.surface-2 with a border.control frame (accent
 * when focused, error colour when [error] is set), optional leading icon, [mono] for addresses and links.
 * The label is also the TalkBack name; the error text sits under the field.
 */
@Composable
fun LavTextField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String? = null,
    leading: LavIcon? = null,
    mono: Boolean = false,
    singleLine: Boolean = true,
    minLines: Int = 1,
    error: String? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    trailing: (@Composable () -> Unit)? = null,
) {
    val c = Lav.colors
    val source = remember { MutableInteractionSource() }
    val focused by source.collectIsFocusedAsState()
    val shape = RoundedCornerShape(Radius.md)
    val frame = when {
        error != null -> c.error
        focused -> c.accent
        else -> c.borderControl
    }
    val textStyle = (if (mono) Lav.type.mono else Lav.type.body).copy(color = c.textPrimary)
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (label != null) LavText(label, style = Lav.type.small, color = c.textSecondary)
        Row(
            Modifier
                .fillMaxWidth()
                .heightIn(min = 48.dp)
                .background(c.bgSurface2, shape)
                .border(if (focused || error != null) 2.dp else 1.dp, frame, shape)
                .padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (leading != null) LavIconView(leading, c.textTertiary, size = 18.dp)
            Box(Modifier.weight(1f).padding(vertical = 10.dp)) {
                if (value.isEmpty() && placeholder != null) {
                    LavText(placeholder, style = textStyle, color = c.textTertiary, maxLines = 1)
                }
                BasicTextField(
                    value = value,
                    onValueChange = onValueChange,
                    modifier = Modifier.fillMaxWidth(),
                    textStyle = textStyle,
                    singleLine = singleLine,
                    minLines = minLines,
                    cursorBrush = SolidColor(c.accent),
                    keyboardOptions = keyboardOptions,
                    keyboardActions = keyboardActions,
                    interactionSource = source,
                )
            }
            trailing?.invoke()
        }
        if (error != null) LavText(error, style = Lav.type.small, color = c.errorText)
    }
}
