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
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.VisualTransformation
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
    visualTransformation: VisualTransformation = VisualTransformation.None,
    trailing: (@Composable () -> Unit)? = null,
    /** False: the text stays visible and selectable but cannot be changed (a request that was made from it is still running). */
    enabled: Boolean = true,
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
            verticalAlignment = if (singleLine) Alignment.CenterVertically else Alignment.Top,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            // A multi-line field keeps the icon level with the first line, not the middle of the box.
            if (leading != null) Box(Modifier.padding(top = if (singleLine) 0.dp else 15.dp)) { LavIconView(leading, c.textTertiary, size = 18.dp) }
            Box(Modifier.weight(1f).padding(vertical = 10.dp)) {
                if (value.isEmpty() && placeholder != null) {
                    LavText(placeholder, style = textStyle, color = c.textTertiary, maxLines = 1)
                }
                BasicTextField(
                    value = value,
                    onValueChange = onValueChange,
                    readOnly = !enabled,
                    // The visible placeholder is not exposed to TalkBack by itself: without a name the field is just "edit box".
                    modifier = Modifier.fillMaxWidth().semantics { (label ?: placeholder)?.let { contentDescription = it } },
                    textStyle = textStyle,
                    singleLine = singleLine,
                    minLines = minLines,
                    cursorBrush = SolidColor(c.accent),
                    keyboardOptions = keyboardOptions,
                    keyboardActions = keyboardActions,
                    visualTransformation = visualTransformation,
                    interactionSource = source,
                )
            }
            trailing?.invoke()
        }
        if (error != null) LavText(error, style = Lav.type.small, color = c.errorText)
    }
}
