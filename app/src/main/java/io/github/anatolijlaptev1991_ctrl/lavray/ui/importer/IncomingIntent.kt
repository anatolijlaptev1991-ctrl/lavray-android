// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

/**
 * Picks the importable text out of an intent that opened the app: a deep link (VIEW with one of the share schemes)
 * or text shared from another app (SEND). Pure, so it can be tested without Android. Nothing is imported here:
 * the result only opens the import sheet, where the person confirms.
 */
object IncomingIntent {
    const val ACTION_VIEW = "android.intent.action.VIEW"
    const val ACTION_SEND = "android.intent.action.SEND"

    fun extract(action: String?, dataString: String?, sharedText: CharSequence?): String? {
        val candidate = when (action) {
            ACTION_VIEW -> dataString?.takeIf { hasDeepLinkScheme(it) }
            ACTION_SEND -> sharedText?.toString()
            else -> null
        }
        return candidate?.takeIf { LinkClassifier.isImportable(it) }
    }

    private fun hasDeepLinkScheme(link: String): Boolean {
        val scheme = link.substringBefore("://", missingDelimiterValue = "").lowercase()
        return scheme in LinkClassifier.deepLinkSchemes
    }
}
