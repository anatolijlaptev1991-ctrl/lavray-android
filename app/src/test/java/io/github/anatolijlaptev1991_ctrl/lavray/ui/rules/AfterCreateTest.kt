// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.rules

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetDraft
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.afterCreate
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.IOException

/** E-27: a failed multi-step change leaves nothing half-made, and an entry that was not accepted stays in the field. */
class AfterCreateTest {
    @Test fun aSuccessfulChainKeepsWhatWasCreated() = runBlocking {
        val undone = mutableListOf<String>()
        val result = afterCreate("copy-1", { undone += it }) { it.uppercase() }
        assertEquals("COPY-1", result)
        assertTrue(undone.isEmpty())
    }

    @Test fun aFailureAfterCreationRemovesTheCreatedPresetAndRethrows() = runBlocking {
        val undone = mutableListOf<String>()
        try {
            afterCreate("copy-2", { undone += it }) { throw IOException("activation failed") }
            fail("the failure must reach the caller")
        } catch (e: IOException) {
            assertEquals("activation failed", e.message)
        }
        assertEquals(listOf("copy-2"), undone)
    }

    @Test fun aFailingUndoDoesNotHideTheOriginalFailureAndIsAttachedToIt() = runBlocking {
        try {
            afterCreate("copy-3", { throw IOException("delete failed") }) { throw IllegalStateException("first") }
            fail("expected a failure")
        } catch (e: IllegalStateException) {
            assertEquals("first", e.message)
            assertEquals("the copy that could not be removed is on record", "delete failed", e.suppressed.single().message)
        }
    }

    @Test fun anEntryCountsAsAddedOnlyWhenTheListHasIt() {
        assertTrue(PresetDraft.contains(listOf("a.example", "b.example"), " B.example "))
        assertFalse(PresetDraft.contains(listOf("a.example"), "b.example"))
        assertFalse(PresetDraft.contains(emptyList(), "  "))
    }
}
