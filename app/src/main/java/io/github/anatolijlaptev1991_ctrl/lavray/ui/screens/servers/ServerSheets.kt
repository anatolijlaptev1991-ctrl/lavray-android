// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.PersistableBundle
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import kotlinx.coroutines.CoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TestLatencyRequest
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavConfirmDialog
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSheet
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.QrEncoder
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.QrMatrix
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.ServerSort
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Which sheet or dialog the servers screen shows. One at a time; null means none. */
sealed interface ServerSheet {
    data class RowMenu(val row: ServerRow) : ServerSheet
    data class Qr(val name: String, val link: String) : ServerSheet
    data class Move(val ids: Set<Long>) : ServerSheet
    data class ConfirmDelete(val ids: Set<Long>) : ServerSheet
    data object View : ServerSheet
    data object Groups : ServerSheet
}

/** Everything the sheets need from the screen, as plain values and callbacks. */
class SheetEnv(
    val state: ServersState,
    val model: ServersModel,
    val canReorder: Boolean,
    val compact: Boolean,
    val onCompact: (Boolean) -> Unit,
    val onSort: (ServerSort) -> Unit,
    val onGroupBy: (Boolean) -> Unit,
    val onConnect: (Long) -> Unit,
    val onCopied: () -> Unit,
    val show: (ServerSheet?) -> Unit,
    /** Outlives the sheets: an action that closes its sheet and then works on must not run in the sheet's own scope. */
    val scope: CoroutineScope,
)

@Composable
fun ServerSheets(sheet: ServerSheet?, env: SheetEnv) {
    val close = { env.show(null) }
    when (sheet) {
        null -> Unit
        is ServerSheet.RowMenu -> RowMenuSheet(sheet.row, env, close)
        is ServerSheet.Qr -> QrSheet(sheet.name, sheet.link, close)
        is ServerSheet.Move -> MoveSheet(sheet.ids, env, close)
        is ServerSheet.ConfirmDelete -> ConfirmDelete(sheet.ids, env, close)
        ServerSheet.View -> ViewSheet(env, close)
        ServerSheet.Groups -> GroupsSheet(env, close)
    }
}

@Composable
private fun MenuRow(icon: LavIcon, text: String, onClick: () -> Unit, danger: Boolean = false) {
    val c = Lav.colors
    LavListRow(
        title = text, onClick = onClick,
        leading = { LavIconView(icon, if (danger) c.errorText else c.textSecondary, size = 20.dp) },
    )
}

@Composable
private fun RowMenuSheet(row: ServerRow, env: SheetEnv, close: () -> Unit) {
    val context = LocalContext.current
    val scope = env.scope
    LavSheet(row.name, close) {
        MenuRow(LavIcon.Plug, stringResource(R.string.conn_connect), { close(); env.onConnect(row.id) })
        MenuRow(LavIcon.Star, stringResource(if (row.favorite) R.string.prof_favRm else R.string.prof_favAdd), { close(); env.model.toggleFavorite(row) })
        MenuRow(LavIcon.Copy, stringResource(R.string.common_copyLink), {
            close()
            scope.launch { env.model.shareLink(row.id)?.let { copyLink(context, it); env.onCopied() } }
        })
        MenuRow(LavIcon.Share, stringResource(R.string.srv_share), {
            close()
            scope.launch { env.model.shareLink(row.id)?.let { shareLink(context, it) } }
        })
        MenuRow(LavIcon.Qr, stringResource(R.string.qr_show), {
            close()
            scope.launch { env.model.shareLink(row.id)?.let { env.show(ServerSheet.Qr(row.name, it)) } }
        })
        MenuRow(LavIcon.Copy, stringResource(R.string.srv_menu_clone), { close(); env.model.clone(setOf(row.id)) })
        MenuRow(LavIcon.Folder, stringResource(R.string.srv_menu_move), { env.show(ServerSheet.Move(setOf(row.id))) })
        if (env.canReorder) {
            MenuRow(LavIcon.ArrowUp, stringResource(R.string.srv_move_up), { close(); env.model.reorder(row.id, up = true) })
            MenuRow(LavIcon.ArrowDown, stringResource(R.string.srv_move_down), { close(); env.model.reorder(row.id, up = false) })
        }
        MenuRow(LavIcon.Trash, stringResource(R.string.common_delete), { env.show(ServerSheet.ConfirmDelete(setOf(row.id))) }, danger = true)
    }
}

@Composable
private fun ConfirmDelete(ids: Set<Long>, env: SheetEnv, close: () -> Unit) {
    LavConfirmDialog(
        title = stringResource(R.string.common_delete),
        text = stringResource(R.string.srv_delConfirm, ids.size),
        confirmLabel = stringResource(R.string.common_delete),
        dismissLabel = stringResource(R.string.common_cancel),
        onConfirm = { close(); env.model.delete(ids) },
        onDismiss = close,
        danger = true,
    )
}

@Composable
private fun MoveSheet(ids: Set<Long>, env: SheetEnv, close: () -> Unit) {
    // Subscription groups are not offered: the next update would delete the moved servers.
    val targets = env.state.groups.filterNot { it.subscription }
    LavSheet(stringResource(R.string.mv_title), close) {
        LavText(stringResource(R.string.mv_text, ids.size), style = Lav.type.small, color = Lav.colors.textSecondary)
        if (targets.isEmpty()) LavText(stringResource(R.string.mv_none), style = Lav.type.body)
        targets.forEach { g ->
            MenuRow(LavIcon.Folder, g.name, { close(); env.model.move(ids, g.id) })
        }
    }
}

@Composable
private fun ViewSheet(env: SheetEnv, close: () -> Unit) {
    val s = env.state
    LavSheet(stringResource(R.string.srv_view), close) {
        LavText(stringResource(R.string.prof_sortTip), style = Lav.type.small, color = Lav.colors.textSecondary)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LavChip(stringResource(R.string.srv_sort_default), s.sort == ServerSort.Default, { env.onSort(ServerSort.Default) })
            LavChip(stringResource(R.string.prof_sortDelay), s.sort == ServerSort.Latency, { env.onSort(ServerSort.Latency) })
            LavChip(stringResource(R.string.prof_sortName), s.sort == ServerSort.Name, { env.onSort(ServerSort.Name) })
        }
        LavSwitchRow(stringResource(R.string.srv_group), s.groupBy, env.onGroupBy)
        LavText(stringResource(R.string.dens_title), style = Lav.type.small, color = Lav.colors.textSecondary)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LavChip(stringResource(R.string.dens_normal), !env.compact, { env.onCompact(false) })
            LavChip(stringResource(R.string.dens_compact), env.compact, { env.onCompact(true) })
        }
        MenuRow(LavIcon.Gauge, stringResource(R.string.test_url), {
            close()
            env.model.startTest(s.items.mapNotNull { (it as? ListItem.Row)?.row?.id }, TestLatencyRequest.Mode.MODE_URL_TEST)
        })
        MenuRow(LavIcon.Folder, stringResource(R.string.grp_manage), { env.show(ServerSheet.Groups) })
    }
}

@Composable
private fun GroupsSheet(env: SheetEnv, close: () -> Unit) {
    var editing by remember { mutableStateOf<GroupInfo?>(null) }
    var deleting by remember { mutableStateOf<GroupInfo?>(null) }
    var name by remember { mutableStateOf("") }
    val counts = remember(env.state.rows) { env.state.rows.groupingBy { it.groupId }.eachCount() }
    LavSheet(stringResource(R.string.grp_title), close) {
        env.state.groups.forEach { g ->
            LavListRow(
                title = g.name,
                subtitle = pluralStringResource(R.plurals.pl_srv, counts[g.id] ?: 0, counts[g.id] ?: 0),
                leading = { LavIconView(if (g.subscription) LavIcon.Rss else LavIcon.Folder, Lav.colors.textSecondary, size = 20.dp) },
                trailing = if (g.subscription) null else ({
                    Row {
                        LavIconButton(LavIcon.Pencil, stringResource(R.string.grp_rename), { editing = g; name = g.name })
                        LavIconButton(LavIcon.Trash, stringResource(R.string.grp_delete), { deleting = g })
                    }
                }),
            )
        }
        LavTextField(
            name, { name = it }, label = stringResource(R.string.grp_name),
            modifier = Modifier.fillMaxWidth(),
        )
        val target = editing
        LavButton(
            text = stringResource(if (target == null) R.string.grp_create else R.string.common_save),
            onClick = {
                if (target == null) env.model.createGroup(name) else env.model.renameGroup(target.id, name)
                name = ""
                editing = null
            },
            variant = ButtonVariant.Primary,
            enabled = name.isNotBlank(),
        )
    }
    deleting?.let { g ->
        LavConfirmDialog(
            title = stringResource(R.string.grp_delete),
            text = stringResource(R.string.grp_delConfirm, counts[g.id] ?: 0, g.name),
            confirmLabel = stringResource(R.string.grp_delete),
            dismissLabel = stringResource(R.string.common_cancel),
            onConfirm = { env.model.deleteGroup(g.id); deleting = null },
            onDismiss = { deleting = null },
            danger = true,
        )
    }
}

private sealed interface QrState {
    data object Loading : QrState
    data object TooLong : QrState
    data class Ready(val matrix: QrMatrix) : QrState
}

/** The QR of a share link, in a sheet. The link holds access keys, so the warning is always shown. */
@Composable
private fun QrSheet(name: String, link: String, close: () -> Unit) {
    val state by produceState<QrState>(QrState.Loading, link) {
        value = withContext(Dispatchers.Default) { QrEncoder.encode(link) }?.let { QrState.Ready(it) } ?: QrState.TooLong
    }
    LavSheet(stringResource(R.string.qr_title), close) {
        LavBanner(StatusTone.Connecting, stringResource(R.string.qr_secretWarn))
        when (val st = state) {
            is QrState.Ready -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                QrView(st.matrix, stringResource(R.string.qr_alt, name), Modifier.widthIn(max = 320.dp).fillMaxWidth())
            }
            QrState.TooLong -> LavText(stringResource(R.string.qr_tooLong), style = Lav.type.body, color = Lav.colors.textSecondary)
            QrState.Loading -> Unit
        }
    }
}

/**
 * Draws [matrix] as vector squares on a white card with the four-module quiet zone the standard asks for.
 * Always dark on white, in both themes, because scanners expect it.
 */
@Composable
fun QrView(matrix: QrMatrix, description: String, modifier: Modifier = Modifier) {
    val dark = Color(0xFF111111)
    Canvas(
        modifier
            .aspectRatio(1f)
            .background(Color.White, RoundedCornerShape(12.dp))
            .semantics { contentDescription = description },
    ) {
        val quiet = 4
        val cell = size.width / (matrix.size + 2 * quiet)
        for (y in 0 until matrix.size) {
            for (x in 0 until matrix.size) {
                if (matrix.isDark(x, y)) {
                    drawRect(dark, Offset((x + quiet) * cell, (y + quiet) * cell), Size(cell + 0.5f, cell + 0.5f))
                }
            }
        }
    }
}

/** Copies a link; marked sensitive so the keyboard and the clipboard preview do not show it (API 33+, ignored below). */
fun copyLink(context: Context, link: String) {
    val clip = ClipData.newPlainText("link", link)
    clip.description.extras = PersistableBundle().apply { putBoolean("android.content.extra.IS_SENSITIVE", true) }
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(clip)
}

fun shareLink(context: Context, link: String) {
    val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, link)
    context.startActivity(Intent.createChooser(send, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}
