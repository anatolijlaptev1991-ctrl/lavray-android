// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LinkClassifierTest {
    @Test
    fun emptyAndBlankAreEmpty() {
        assertEquals(ImportInput.Empty, LinkClassifier.classify(null))
        assertEquals(ImportInput.Empty, LinkClassifier.classify("  \n "))
    }

    @Test
    fun everyShareSchemeOfTheCoreIsRecognised() {
        for (scheme in listOf("ss", "vmess", "vless", "trojan", "hy2", "hysteria2", "tuic", "socks", "socks4", "socks4a", "socks5", "nekoray")) {
            val r = LinkClassifier.classify("$scheme://abc@host:443#name")
            assertTrue("$scheme -> $r", r is ImportInput.Links && r.schemes == listOf(scheme))
        }
    }

    @Test
    fun schemeMatchingIsCaseInsensitiveButLinksAreKeptVerbatim() {
        val r = LinkClassifier.classify("  VLESS://Uuid@Host:443?x=1#N  ") as ImportInput.Links
        assertEquals(listOf("VLESS://Uuid@Host:443?x=1#N"), r.links)
        assertEquals(listOf("vless"), r.schemes)
    }

    @Test
    fun severalLinksOneUnknownSchemeLineIsSkipped() {
        val r = LinkClassifier.classify("vless://a@h:1\nftp://x/y\n\ntrojan://p@h:2") as ImportInput.Links
        assertEquals(2, r.links.size)
        assertEquals(1, r.skipped)
        assertEquals(listOf("vless", "trojan"), r.schemes)
    }

    @Test
    fun singleHttpAddressIsASubscriptionAndPlainHttpIsFlaggedInsecure() {
        assertEquals(ImportInput.SubscriptionUrl("https://p.example/sub?t=1", insecure = false), LinkClassifier.classify("https://p.example/sub?t=1"))
        assertEquals(ImportInput.SubscriptionUrl("http://p.example/sub", insecure = true), LinkClassifier.classify("http://p.example/sub"))
    }

    @Test
    fun encryptedSubscriptionLinkIsItsOwnKind() {
        assertTrue(LinkClassifier.classify("happ://crypt3/abcdef") is ImportInput.EncryptedLink)
    }

    @Test
    fun documentsAreJsonYamlOrBase64() {
        assertEquals(DocKind.Json, (LinkClassifier.classify("{\"outbounds\": []}") as ImportInput.Document).kind)
        assertEquals(DocKind.Yaml, (LinkClassifier.classify("port: 7890\nproxies:\n  - name: a") as ImportInput.Document).kind)
        assertEquals(DocKind.Base64, (LinkClassifier.classify("dmxlc3M6Ly9hQGg6MQp0cm9qYW46Ly9wQGg6Mg==") as ImportInput.Document).kind)
    }

    @Test
    fun everythingElseIsUnrecognisedAndNotImportable() {
        assertEquals(ImportInput.Unrecognized, LinkClassifier.classify("hello world"))
        assertEquals(ImportInput.Unrecognized, LinkClassifier.classify("ftp://only.unknown/scheme"))
        assertFalse(LinkClassifier.isImportable("hello"))
        assertFalse(LinkClassifier.isImportable(""))
        assertTrue(LinkClassifier.isImportable("vmess://eyJhZGQiOiJoIn0="))
    }

    @Test
    fun httpsIsNotADeepLinkScheme() {
        assertFalse("https" in LinkClassifier.deepLinkSchemes)
        assertFalse("http" in LinkClassifier.deepLinkSchemes)
        assertTrue("vless" in LinkClassifier.deepLinkSchemes)
    }

    @Test
    fun maskHidesTheMiddleOfALink() {
        val link = "vless://0123456789abcdef-secret-uuid@host.example:443?security=reality#Name"
        val masked = LinkClassifier.mask(link)
        assertFalse(masked.contains("secret"))
        assertTrue(masked.startsWith("vless://"))
        assertTrue(masked.contains("…"))
        assertEquals("ss://short", LinkClassifier.mask("ss://short"))
    }
}

class IncomingIntentTest {
    @Test
    fun deepLinksOfKnownSchemesAreOffered() {
        assertEquals("vless://a@h:1", IncomingIntent.extract(IncomingIntent.ACTION_VIEW, "vless://a@h:1", null))
    }

    @Test
    fun webLinksAndOtherSchemesAreNotOfferedAsDeepLinks() {
        assertNull(IncomingIntent.extract(IncomingIntent.ACTION_VIEW, "https://p.example/sub", null))
        assertNull(IncomingIntent.extract(IncomingIntent.ACTION_VIEW, "market://details?id=x", null))
    }

    @Test
    fun sharedTextIsOfferedWhenItIsImportable() {
        assertEquals("https://p.example/sub", IncomingIntent.extract(IncomingIntent.ACTION_SEND, null, "https://p.example/sub"))
        assertNull(IncomingIntent.extract(IncomingIntent.ACTION_SEND, null, "just some text"))
        assertNull(IncomingIntent.extract(IncomingIntent.ACTION_SEND, null, null))
    }

    @Test
    fun otherActionsAreIgnored() {
        assertNull(IncomingIntent.extract("android.intent.action.MAIN", "vless://a@h:1", "vless://a@h:1"))
        assertNull(IncomingIntent.extract(null, null, null))
    }
}

class ImportTabTest {
    @Test
    fun leavingTheLinkTabForgetsTheTypedLink() {
        val typed = ImportUi(open = true, tab = ImportTab.Link, payload = "ss://YWVzLTEyOC1nY206cGFzcw==@h:1", input = LinkClassifier.classify("ss://YWVzLTEyOC1nY206cGFzcw==@h:1"))
        assertTrue(typed.canSubmit)
        val file = typed.switchedTo(ImportTab.File)
        assertEquals(ImportTab.File, file.tab)
        assertEquals("", file.payload)
        assertFalse(file.canSubmit)
    }

    @Test
    fun tappingTheCurrentTabKeepsThePayload() {
        val typed = ImportUi(open = true, tab = ImportTab.Link, payload = "ss://YWVzLTEyOC1nY206cGFzcw==@h:1", input = LinkClassifier.classify("ss://YWVzLTEyOC1nY206cGFzcw==@h:1"))
        assertEquals(typed, typed.switchedTo(ImportTab.Link))
    }
}

class QrTest {
    @Test
    fun encodeThenDecodeRoundTrips() {
        val link = "vless://11111111-2222-3333-4444-555555555555@host.example:443?security=reality&sni=a.example#Тест"
        val matrix = QrEncoder.encode(link)
        assertNotNull(matrix)
        val m = matrix!!
        val scale = 6
        val quiet = 4
        val side = (m.size + 2 * quiet) * scale
        val pixels = IntArray(side * side) { -1 }
        for (y in 0 until m.size) for (x in 0 until m.size) {
            if (!m.isDark(x, y)) continue
            for (dy in 0 until scale) for (dx in 0 until scale) {
                pixels[((y + quiet) * scale + dy) * side + (x + quiet) * scale + dx] = 0xFF000000.toInt()
            }
        }
        assertEquals(link, QrDecoder.decodePixels(pixels, side, side))
    }

    @Test
    fun aLightCodeOnADarkBackgroundIsReadToo() {
        val m = QrEncoder.encode("vmess://eyJhZGQiOiJoIn0=")!!
        val scale = 5
        val quiet = 4
        val side = (m.size + 2 * quiet) * scale
        val pixels = IntArray(side * side) { 0xFF000000.toInt() }
        for (y in 0 until m.size) for (x in 0 until m.size) {
            if (!m.isDark(x, y)) continue
            for (dy in 0 until scale) for (dx in 0 until scale) {
                pixels[((y + quiet) * scale + dy) * side + (x + quiet) * scale + dx] = -1
            }
        }
        // Dark modules are drawn light on a dark field: the reader must still find the code.
        assertEquals("vmess://eyJhZGQiOiJoIn0=", QrDecoder.decodePixels(pixels, side, side))
    }

    @Test
    fun nothingFoundInABlankImage() {
        assertNull(QrDecoder.decodePixels(IntArray(100 * 100) { -1 }, 100, 100))
    }

    @Test
    fun aLinkThatDoesNotFitIsRefusedNotCrashed() {
        assertNull(QrEncoder.encode("x".repeat(5000)))
        assertNull(QrEncoder.encode(""))
    }

    @Test
    fun imageSampleSizeKeepsTheLongSideWithinTwoThousandPixels() {
        assertEquals(1, QrImageReader.sampleSize(1500, 900))
        assertEquals(2, QrImageReader.sampleSize(4000, 3000))
        assertEquals(4, QrImageReader.sampleSize(8000, 6000))
    }
}
