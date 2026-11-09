// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup

import android.content.ContentResolver
import android.net.Uri
import android.provider.OpenableColumns
import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.BackupSummary
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ExportBackupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportBackupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.PreviewBackupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.backupExportBackup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.backupImportBackup
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.backupPreviewBackup
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import java.io.IOException

@Immutable
data class BackupState(
    val busy: Boolean = false,
    @StringRes val error: Int? = null,
    /** An encrypted backup made and waiting for the person to choose where to save it; the suggested file name. */
    val exportName: String? = null,
    val saved: Boolean = false,
    val pickedName: String? = null,
    val summary: BackupSummary? = null,
    /** Restore finished: whether the core wants the app restarted. */
    val restored: Boolean = false,
    val needsRestart: Boolean = false,
)

/**
 * Backup export and restore through the system file picker (no storage permission). The encrypted bytes live only in
 * this object while a save or a restore is under way and are dropped right after; the password is passed on and never
 * stored. Files are read and written on the IO dispatcher.
 */
class BackupModel(private val session: CoreSession, private val resolver: ContentResolver) {
    private val mutable = MutableStateFlow(BackupState())
    val state: StateFlow<BackupState> = mutable.asStateFlow()

    private var exported: ByteArray? = null
    private var picked: ByteArray? = null

    suspend fun createBackup(password: String) = withContext(Dispatchers.Default) {
        mutable.update { it.copy(busy = true, error = null, exportName = null, saved = false) }
        val result = session.call { backupExportBackup(ExportBackupRequest(BackupRules.normalize(password))) }
        when (result) {
            is CoreResult.Ok -> {
                exported = result.value.data
                mutable.update { it.copy(busy = false, exportName = result.value.suggestedName.ifBlank { "lavray.lavbk" }) }
            }
            is CoreResult.Fail -> mutable.update { it.copy(busy = false, error = BackupErrors.text(result.reason, result.kind)) }
        }
    }

    /** Writes the made backup to the document the person chose. */
    suspend fun saveTo(uri: Uri) {
        val data = exported ?: return
        mutable.update { it.copy(busy = true, error = null) }
        val ok = withContext(Dispatchers.IO) {
            try {
                resolver.openOutputStream(uri, "wt")?.use { it.write(data) } != null
            } catch (_: IOException) {
                false
            } catch (_: SecurityException) {
                false
            }
        }
        if (ok) exported = null
        mutable.update { it.copy(busy = false, saved = ok, exportName = if (ok) null else it.exportName, error = if (ok) null else R.string.bk_err_save) }
    }

    fun cancelExport() {
        exported = null
        mutable.update { it.copy(exportName = null) }
    }

    /** Reads the chosen file (at most 64 MiB) so it can be checked; a bigger one is refused before it fills memory. */
    suspend fun pick(uri: Uri) {
        picked = null
        mutable.update { it.copy(busy = true, error = null, summary = null, pickedName = null, restored = false) }
        val read = withContext(Dispatchers.IO) { readLimited(uri) }
        when (read) {
            is Read.Ok -> {
                picked = read.bytes
                mutable.update { it.copy(busy = false, pickedName = read.name) }
            }
            Read.TooLarge -> mutable.update { it.copy(busy = false, error = R.string.bk_err_tooLarge) }
            Read.Failed -> mutable.update { it.copy(busy = false, error = R.string.bk_err_read) }
        }
    }

    /** Opens the file with the password and reads what is inside; the core changes nothing. */
    suspend fun check(password: String) = withContext(Dispatchers.Default) {
        val data = picked ?: return@withContext
        mutable.update { it.copy(busy = true, error = null, summary = null) }
        when (val r = session.call { backupPreviewBackup(PreviewBackupRequest(data, BackupRules.normalize(password))) }) {
            is CoreResult.Ok -> mutable.update { it.copy(busy = false, summary = r.value) }
            is CoreResult.Fail -> mutable.update { it.copy(busy = false, error = BackupErrors.text(r.reason, r.kind)) }
        }
    }

    /** Replaces all data. Only called after the person confirmed the warning. */
    suspend fun replace(password: String) = withContext(Dispatchers.Default) {
        val data = picked ?: return@withContext
        mutable.update { it.copy(busy = true, error = null) }
        when (val r = session.call { backupImportBackup(ImportBackupRequest(data, BackupRules.normalize(password), confirmReplace = true)) }) {
            is CoreResult.Ok -> {
                picked = null
                mutable.update { it.copy(busy = false, restored = true, needsRestart = r.value.needsAppRestart, summary = null, pickedName = null) }
            }
            is CoreResult.Fail -> mutable.update { it.copy(busy = false, error = BackupErrors.text(r.reason, r.kind)) }
        }
    }

    /** A new password means the earlier check no longer applies. */
    fun invalidateCheck() = mutable.update { it.copy(summary = null, error = null) }

    /** Leaving the screen drops the file contents held in memory. */
    fun release() {
        exported = null
        picked = null
    }

    private sealed interface Read {
        data class Ok(val bytes: ByteArray, val name: String) : Read
        data object TooLarge : Read
        data object Failed : Read
    }

    private fun readLimited(uri: Uri): Read = try {
        val name = displayName(uri) ?: "backup"
        val stream = resolver.openInputStream(uri) ?: return Read.Failed
        stream.use { input ->
            val limit = BackupRules.MAX_FILE_BYTES
            val out = java.io.ByteArrayOutputStream()
            val buffer = ByteArray(BUFFER)
            var total = 0
            while (true) {
                val n = input.read(buffer)
                if (n < 0) break
                total += n
                if (total > limit) return Read.TooLarge
                out.write(buffer, 0, n)
            }
            Read.Ok(out.toByteArray(), name)
        }
    } catch (_: IOException) {
        Read.Failed
    } catch (_: SecurityException) {
        Read.Failed
    }

    private fun displayName(uri: Uri): String? = try {
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
            if (c.moveToFirst()) c.getString(0) else null
        }
    } catch (_: SecurityException) {
        null
    }

    private companion object {
        const val BUFFER = 64 * 1024
    }
}
