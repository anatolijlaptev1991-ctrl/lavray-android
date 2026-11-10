// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppEntry
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppSelectionSaver
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

/** E-05, E-06: the per-app choice is saved in order, the last tap wins, and chosen apps that are gone can be removed. */
class AppSelectionSaverTest {
    private fun sel(vararg pkg: String) = AppSelection(AppMode.Only, pkg.toSet())

    @Test fun theLastTapIsTheOneThatEndsUpSaved() = runBlocking {
        val written = mutableListOf<AppSelection>()
        val gate = CompletableDeferred<Unit>()
        val saved = AtomicInteger()
        val saver = AppSelectionSaver({ s -> if (written.isEmpty()) gate.await(); written += s; true }) { saved.incrementAndGet() }
        val first = async { saver.save(sel("a.b")) }
        yield()
        val rest = (2..5).map { n -> async { saver.save(sel("a.b", "c.d$n")) } }
        yield()
        gate.complete(Unit)
        (listOf(first) + rest).awaitAll()
        assertEquals(sel("a.b"), written.first())
        assertEquals(sel("a.b", "c.d5"), written.last())
        assertTrue("fewer writes than taps", written.size < 5)
        assertEquals(written.size, saved.get())
    }

    @Test fun aFailedWriteIsReportedAndNotMarkedAsSaved() = runBlocking {
        val saved = AtomicInteger()
        val saver = AppSelectionSaver({ false }) { saved.incrementAndGet() }
        assertFalse(saver.save(sel("a.b")))
        assertEquals(0, saved.get())
    }

    @Test fun aChosenAppThatIsNotInstalledStaysVisibleSoItCanBeRemoved() {
        val installed = listOf(AppEntry("a.b", "Alpha", system = false))
        val chosen = setOf("a.b", "gone.app")
        val shown = AppFilter.visible(AppFilter.withMissing(installed, chosen), "", false, chosen)
        assertEquals(listOf("a.b", "gone.app"), shown.map { it.packageName }.sorted())
        val gone = shown.single { it.packageName == "gone.app" }
        assertTrue(gone.missing)
        assertFalse(shown.single { it.packageName == "a.b" }.missing)
        // Once it is unchosen it is not offered any more.
        assertEquals(listOf("a.b"), AppFilter.withMissing(installed, setOf("a.b")).map { it.packageName })
    }

    @Test fun missingAppsAreFoundByTheirNameInTheSearch() {
        val shown = AppFilter.visible(AppFilter.withMissing(emptyList(), setOf("gone.app", "other.one")), "gone", false, setOf("gone.app", "other.one"))
        assertEquals(listOf("gone.app"), shown.map { it.packageName })
    }
}
