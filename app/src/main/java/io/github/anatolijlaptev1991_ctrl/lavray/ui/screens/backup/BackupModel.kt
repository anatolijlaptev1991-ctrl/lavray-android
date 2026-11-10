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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException

@Immutable
data class BackupState(
    val busy: Boolean = false,
    @StringRes val error: Int? = null,
    /** An encrypted backup made and waiting for the person to choose where to save it; the suggested file name. */
    val exportName: String? = null,
    /** The made backup was written to the chosen document; the screen shows it once and calls [BackupModel.acknowledgeSaved]. */
    val saved: Boolean = false,
    val pickedName: String? = null,
    val summary: BackupSummary? = null,
    /** Restore finished: whether the core wants the app restarted. */
    val restored: Boolean = false,
    val needsRestart: Boolean = false,
) {
    /** What the screen showed about files it no longer holds, once it was left; see [BackupModel.release]. */
    fun withoutFileState(): BackupState =
        if (restored) this else copy(error = null, exportName = null, saved = false, pickedName = null, summary = null)
}

/** A document the person chose (null only in tests). */
class FileTarget(val uri: Uri?)

/** The system file picker side of backup, so the model can be tested without a content resolver. */
interface BackupFiles {
    fun write(target: FileTarget, data: ByteArray): Boolean
    fun read(source: FileTarget, limit: Int): FileRead
}

sealed interface FileRead {
    class Ok(val bytes: ByteArray, val name: String) : FileRead
    data object TooLarge : FileRead
    data object Failed : FileRead
}

class ResolverBackupFiles(private val resolver: ContentResolver) : BackupFiles {
    override fun write(target: FileTarget, data: ByteArray): Boolean = try {
        val uri = target.uri ?: return false
        resolver.openOutputStream(uri, "wt")?.use { it.write(data) } != null
    } catch (_: IOException) {
        false
    } catch (_: SecurityException) {
        false
    }

    override fun read(source: FileTarget, limit: Int): FileRead = try {
        val uri = source.uri ?: return FileRead.Failed
        val name = displayName(uri) ?: "backup"
        val stream = resolver.openInputStream(uri)
        if (stream == null) {
            FileRead.Failed
        } else {
            stream.use { input ->
                val out = java.io.ByteArrayOutputStream()
                val buffer = ByteArray(BUFFER)
                var total = 0
                var result: FileRead? = null
                while (result == null) {
                    val n = input.read(buffer)
                    if (n < 0) break
                    total += n
                    if (total > limit) result = FileRead.TooLarge else out.write(buffer, 0, n)
                }
                result ?: FileRead.Ok(out.toByteArray(), name)
            }
        }
    } catch (_: IOException) {
        FileRead.Failed
    } catch (_: SecurityException) {
        FileRead.Failed
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

/**
 * Backup export and restore through the system file picker (no storage permission). Every operation runs on [scope],
 * the app's scope, and clears `busy` however it ends: the model outlives the screen, so an operation that the screen's
 * own coroutine scope would have cancelled mid-way must not leave the buttons disabled for the rest of the process.
 * The password is a parameter of the call (read by the caller, never stored); the encrypted bytes live here only while
 * a save or a restore is under way and are dropped right after.
 */
class BackupModel(
    private val session: CoreSession,
    private val files: BackupFiles,
    private val scope: CoroutineScope,
    /** Called before the core is asked to replace the data (what the app remembers about the old data is stale from here on). */
    private val onBeforeReplace: () -> Unit = {},
    /** Called once after the core replaced all data. */
    private val onRestored: () -> Unit = {},
) {
    private val mutable = MutableStateFlow(BackupState())
    val state: StateFlow<BackupState> = mutable.asStateFlow()

    @Volatile private var exported: ByteArray? = null
    @Volatile private var picked: ByteArray? = null
    @Volatile private var attached = true

    /** Marks the first operation of a screen visit; false when another one is still running. */
    private fun claim(reset: (BackupState) -> BackupState): Boolean {
        while (true) {
            val cur = mutable.value
            if (cur.busy) return false
            if (mutable.compareAndSet(cur, reset(cur).copy(busy = true, error = null))) return true
        }
    }

    // ATOMIC: even a job cancelled before it started runs its `finally`, so the busy flag set by [claim] is always cleared.
    private fun launchOp(body: suspend () -> Unit): Job = scope.launch(start = CoroutineStart.ATOMIC) {
        try {
            body()
        } finally {
            // Also when cancelled: busy must never outlive the operation.
            withContext(NonCancellable) {
                mutable.update { it.copy(busy = false) }
                if (!attached) dropFiles()
            }
        }
    }

    fun createBackup(password: String): Job? {
        if (!claim { it.copy(exportName = null, saved = false) }) return null
        val normalized = BackupRules.normalize(password)
        return launchOp {
            when (val result = session.call { backupExportBackup(ExportBackupRequest(normalized)) }) {
                is CoreResult.Ok -> {
                    exported = result.value.data
                    mutable.update { it.copy(exportName = result.value.suggestedName.ifBlank { "lavray.lavbk" }) }
                }
                is CoreResult.Fail -> mutable.update { it.copy(error = BackupErrors.text(result.reason, result.kind)) }
            }
        }
    }

    /** Writes the made backup to the document the person chose. */
    fun saveTo(target: FileTarget): Job? {
        val data = exported ?: return null
        if (!claim { it }) return null
        return launchOp {
            val ok = withContext(Dispatchers.IO) { files.write(target, data) }
            if (ok) exported = null
            mutable.update { it.copy(saved = ok, exportName = if (ok) null else it.exportName, error = if (ok) null else R.string.bk_err_save) }
        }
    }

    fun acknowledgeSaved() = mutable.update { it.copy(saved = false) }

    fun cancelExport() {
        exported = null
        mutable.update { it.copy(exportName = null) }
    }

    /** Reads the chosen file (at most 64 MiB) so it can be checked; a bigger one is refused before it fills memory. */
    fun pick(source: FileTarget): Job? {
        if (!claim { it.copy(summary = null, pickedName = null, restored = false) }) return null
        picked = null
        return launchOp {
            when (val read = withContext(Dispatchers.IO) { files.read(source, BackupRules.MAX_FILE_BYTES) }) {
                is FileRead.Ok -> {
                    picked = read.bytes
                    mutable.update { it.copy(pickedName = read.name) }
                }
                FileRead.TooLarge -> mutable.update { it.copy(error = R.string.bk_err_tooLarge) }
                FileRead.Failed -> mutable.update { it.copy(error = R.string.bk_err_read) }
            }
        }
    }

    /** Opens the file with the password and reads what is inside; the core changes nothing. */
    fun check(password: String): Job? {
        val data = picked ?: return null
        if (!claim { it.copy(summary = null) }) return null
        val normalized = BackupRules.normalize(password)
        return launchOp {
            when (val r = session.call { backupPreviewBackup(PreviewBackupRequest(data, normalized)) }) {
                is CoreResult.Ok -> mutable.update { it.copy(summary = r.value) }
                is CoreResult.Fail -> mutable.update { it.copy(error = BackupErrors.text(r.reason, r.kind)) }
            }
        }
    }

    /** Replaces all data. Only called after the person confirmed the warning. */
    fun replace(password: String): Job? {
        val data = picked ?: return null
        if (!claim { it }) return null
        val normalized = BackupRules.normalize(password)
        return launchOp {
            onBeforeReplace()
            when (val r = session.call { backupImportBackup(ImportBackupRequest(data, normalized, confirmReplace = true)) }) {
                is CoreResult.Ok -> {
                    picked = null
                    mutable.update { it.copy(restored = true, needsRestart = r.value.needsAppRestart, summary = null, pickedName = null) }
                    onRestored()
                }
                is CoreResult.Fail -> mutable.update { it.copy(error = BackupErrors.text(r.reason, r.kind)) }
            }
        }
    }

    /** A new password means the earlier check no longer applies. */
    fun invalidateCheck() = mutable.update { it.copy(summary = null, error = null) }

    /** The backup screen is on show again. */
    fun attach() {
        attached = true
    }

    /**
     * The screen was left: file contents and what the screen showed about them are dropped, now if nothing is running,
     * otherwise as soon as the running operation ends (it keeps going on the app's scope). A finished restore is kept:
     * that is the answer the person is waiting for.
     */
    fun release() {
        attached = false
        if (!mutable.value.busy) dropFiles()
    }

    private fun dropFiles() {
        exported = null
        picked = null
        mutable.update { it.withoutFileState() }
    }
}
