// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The preview must not promise what the core refuses: the core's ss:// rules are checked first. */
class ImportIssueTests {
    private val padded = "ss://YWVzLTI1Ni1nY206c2VjcmV0MQ==@host.example:8388#ok"
    private val unpadded = "ss://YWVzLTI1Ni1nY206c2VjcmV0MQ@host.example:8388#short"

    @Test fun paddedBase64UserInfoIsAccepted() {
        assertNull(SsLinkCheck.check(padded))
    }

    @Test fun unpaddedBase64IsTheCaseTheCoreRefuses() {
        assertEquals(LinkProblem.SsBase64, SsLinkCheck.check(unpadded))
    }

    @Test fun urlSafeAlphabetNeedsPaddingToo() {
        assertNull(SsLinkCheck.check("ss://YWVzLTI1Ni1nY206c2VjcmV0MQ==@h:1"))
        assertEquals(LinkProblem.SsBase64, SsLinkCheck.check("ss://YWVzLTI1Ni1nY206c2VjcmV0MQ@h:1"))
        // "-" and "_" belong to the URL alphabet only; a mix of both alphabets is not accepted by either decoder.
        assertEquals(LinkProblem.SsBase64, SsLinkCheck.check("ss://YW-z+Q==@h:1"))
    }

    @Test fun percentEncodedPaddingIsDecodedFirst() {
        assertNull(SsLinkCheck.check("ss://YWVzLTI1Ni1nY206c2VjcmV0MQ%3D%3D@h:1"))
    }

    @Test fun plainMethodAndPasswordIsTheTwentyTwentyTwoForm() {
        assertNull(SsLinkCheck.check("ss://2022-blake3-aes-128-gcm:cGFzc3dvcmQ=@h:1"))
        assertEquals(LinkProblem.SsMethod, SsLinkCheck.check("ss://rot13:pw@h:1"))
    }

    @Test fun aKeyPartWithoutAPairIsNamedAsSuch() {
        // base64 of "justonetoken1234"
        assertEquals(LinkProblem.SsPair, SsLinkCheck.check("ss://anVzdG9uZXRva2VuMTIzNA==@h:1"))
    }

    @Test fun anUnknownCipherInsideBase64IsNamed() {
        // base64 of "rot13-cipher:pass"
        assertEquals(LinkProblem.SsMethod, SsLinkCheck.check("ss://cm90MTMtY2lwaGVyOnBhc3M=@h:1"))
    }

    @Test fun v2rayNBlobNeedsPaddingAndAnAddress() {
        // base64 of "aes-128-gcm:pass@h.example:8388"
        assertNull(SsLinkCheck.check("ss://YWVzLTEyOC1nY206cGFzc0BoLmV4YW1wbGU6ODM4OA==#n"))
        assertEquals(LinkProblem.SsBase64, SsLinkCheck.check("ss://YWVzLTEyOC1nY206cGFzc0BoLmV4YW1wbGU6ODM4OA#n"))
        // base64 of "aes-128-gcm:pass" - no @host part
        assertEquals(LinkProblem.SsPair, SsLinkCheck.check("ss://YWVzLTEyOC1nY206cGFzcw==#n"))
    }

    @Test fun theClassifierNamesTheLineThatFailsAndCountsTheRest() {
        val r = LinkClassifier.classify("$padded\n\n$unpadded") as ImportInput.Links
        assertEquals(2, r.links.size)
        assertEquals(1, r.readable)
        assertEquals(1, r.issues.size)
        // Line numbers are those of the pasted text, blank lines included.
        assertEquals(3, r.issues[0].line)
        assertEquals(LinkProblem.SsBase64, r.issues[0].problem)
        assertFalse(r.issues[0].masked.contains("YWVzLTI1Ni1nY206c2VjcmV0MQ@"))
    }

    @Test fun onlyAnUnreadableLinkMeansNothingToImport() {
        val input = LinkClassifier.classify(unpadded)
        assertTrue(input is ImportInput.Links && input.readable == 0)
        val ui = ImportUi(open = true, payload = unpadded, input = input)
        assertFalse("the Import button must stay off for a link the core refuses", ui.canSubmit)
    }

    @Test fun aReadableLinkKeepsTheButtonOn() {
        val ui = ImportUi(open = true, payload = padded, input = LinkClassifier.classify(padded))
        assertTrue(ui.canSubmit)
    }

    @Test fun otherSchemesAreLeftToTheCore() {
        val r = LinkClassifier.classify("vless://a@h:1") as ImportInput.Links
        assertTrue(r.issues.isEmpty())
        assertEquals(1, r.readable)
    }
}
