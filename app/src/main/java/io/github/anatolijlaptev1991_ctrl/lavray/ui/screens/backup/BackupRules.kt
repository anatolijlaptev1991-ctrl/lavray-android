// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup

import androidx.annotation.StringRes
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import java.text.Normalizer

enum class PasswordIssue { Short, Long }

/** The password and file rules of the desktop backup dialogs (lib/password.ts) and of the core (64 MiB, 8 characters). */
object BackupRules {
    const val MIN_PASSWORD_CHARS = 8
    const val MAX_PASSWORD_BYTES = 1024
    const val MAX_FILE_BYTES = 64 * 1024 * 1024

    /** NFC is the form that reaches the core, so the same password typed on another keyboard still opens the file. */
    fun normalize(raw: String): String = Normalizer.normalize(raw, Normalizer.Form.NFC)

    fun issue(raw: String): PasswordIssue? {
        val pw = normalize(raw)
        return when {
            pw.toByteArray(Charsets.UTF_8).size > MAX_PASSWORD_BYTES -> PasswordIssue.Long
            pw.codePointCount(0, pw.length) < MIN_PASSWORD_CHARS -> PasswordIssue.Short
            else -> null
        }
    }

    fun same(a: String, b: String): Boolean = normalize(a) == normalize(b)

    /** The extension the desktop uses; the picker still lists every file because Android has no registered type for it. */
    const val EXTENSION = ".lavbk"
}

/** Words an error of the backup calls by the reason the core gives (desktop lib/backupErrors.ts), else by its kind. */
object BackupErrors {
    @StringRes
    fun text(reason: String?, kind: ErrorKind): Int = when (reason) {
        "password_too_short" -> R.string.bk_err_pwShort
        "password_too_long" -> R.string.bk_err_pwLong
        "backup_not_recognized" -> R.string.bk_err_notRecognized
        "backup_wrong_password" -> R.string.bk_err_wrongPassword
        "backup_tampered" -> R.string.bk_err_tampered
        "backup_from_newer_version" -> R.string.bk_err_newer
        "confirm_replace_required" -> R.string.bk_err_confirm
        "connection_active" -> R.string.bk_err_connected
        "backup_too_large" -> R.string.bk_err_tooLarge
        "backup_busy" -> R.string.bk_err_busy
        "backup_failed" -> R.string.bk_err_failed
        else -> kind.textRes()
    }
}
