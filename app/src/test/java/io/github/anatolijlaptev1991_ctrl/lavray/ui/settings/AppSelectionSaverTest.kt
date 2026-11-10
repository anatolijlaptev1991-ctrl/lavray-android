// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppEntry
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppSelectionSaver
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppSelectionSaver.Result
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.canonical
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList

/** E-05, E-06 and review M4/M5/L8: the per-app choice is saved in order, the newest tap wins, and reads share the lock. */
class AppSelectionSaverTest {
    private fun sel(vararg pkg: String) = AppSelection(AppMode.Only, pkg.toSet())
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private class Store(initial: AppSelection = AppSelection()) {
        var stored: AppSelection? = initial
        val written = CopyOnWriteArrayList<AppSelection>()
        val saved = CopyOnWriteArrayList<Pair<AppSelection?, AppSelection>>()
    }

    private fun saver(store: Store, write: suspend (AppSelection) -> Boolean = { s -> store.written += s; store.stored = s; true }, timeoutMs: Long = 5_000) =
        AppSelectionSaver(read = { store.stored }, write = write, onSaved = { b, a -> store.saved += b to a }, timeoutMs = timeoutMs)

    @Test fun theNewestTapWinsInTheOrderOfTheCallsWhateverTheSchedulingIs() = runBlocking {
        val store = Store()
        val gate = CompletableDeferred<Unit>()
        val saver = saver(store, write = { s -> gate.await(); store.written += s; store.stored = s; true })
        val first = saver.submit(sel("a.b"), scope)
        // Recorded on this thread, one after the other: the call order IS the tap order.
        val rest = (2..6).map { n -> saver.submit(sel("a.b", "c.d$n"), scope) }
        gate.complete(Unit)
        val results = (listOf(first) + rest).awaitAll()
        assertEquals(sel("a.b", "c.d6"), store.written.last())
        assertTrue("fewer writes than taps", store.written.size < 6)
        assertEquals("every write that happened is reported saved by exactly one call", store.written.size, results.count { it is Result.Saved })
        assertTrue(results.all { it is Result.Saved || it is Result.Superseded })
        scope.cancel()
    }

    @Test fun aFailedWriteReportsWhatTheServiceHasAndIsNotMarkedSaved() = runBlocking {
        val store = Store(sel("old.app"))
        val saver = saver(store, write = { false })
        val result = saver.submit(sel("new.app"), scope).await()
        assertEquals(Result.Failed(sel("old.app")), result)
        assertTrue(store.saved.isEmpty())
        scope.cancel()
    }

    @Test fun aServiceThatNeverAnswersDoesNotHoldTheLockForEver() = runBlocking {
        val store = Store(sel("old.app"))
        val saver = saver(store, write = { awaitCancellation() }, timeoutMs = 300)
        assertTrue(saver.submit(sel("x.y"), scope).await() is Result.Failed)
        // The lock was released: the next call is served.
        assertEquals(sel("old.app"), saver.current())
        scope.cancel()
    }

    @Test fun aReadWaitsForTheWriteThatIsRunning() = runBlocking {
        val store = Store(sel("old.app"))
        val gate = CompletableDeferred<Unit>()
        val saver = saver(store, write = { s -> gate.await(); store.stored = s; true })
        val write = saver.submit(sel("new.app"), scope)
        val read = scope.async { saver.current() }
        kotlinx.coroutines.delay(100)
        assertFalse("the read must not see the state in the middle of a write", read.isCompleted)
        gate.complete(Unit)
        write.await()
        assertEquals(sel("new.app"), read.await())
        scope.cancel()
    }

    @Test fun theSavedCallbackGetsTheStoredChoiceBeforeAndAfter() = runBlocking {
        val store = Store(sel("old.app"))
        saver(store).submit(sel("new.app"), scope).await()
        assertEquals(listOf<Pair<AppSelection?, AppSelection>>(sel("old.app") to sel("new.app")), store.saved.toList())
        scope.cancel()
    }

    @Test fun choicesThatMeanTheSameCompareEqualForTheLedger() {
        assertEquals(AppSelection(AppMode.All, setOf("a.b")).canonical(), AppSelection().canonical())
        assertEquals("only, with nothing chosen, is all apps", AppSelection(AppMode.Only, emptySet()).canonical(), AppSelection().canonical())
        assertFalse(sel("a.b").canonical() == AppSelection().canonical())
        assertFalse(sel("a.b").canonical() == sel("a.c").canonical())
    }

    @Test fun aChosenAppThatIsNotInTheLauncherListStaysVisibleSoItCanBeRemoved() {
        val installed = listOf(AppEntry("a.b", "Alpha", system = false))
        val chosen = setOf("a.b", "gone.app")
        val shown = AppFilter.visible(AppFilter.withMissing(installed, chosen), "", false, chosen)
        assertEquals(listOf("a.b", "gone.app"), shown.map { it.packageName }.sorted())
        assertTrue(shown.single { it.packageName == "gone.app" }.missing)
        assertFalse(shown.single { it.packageName == "a.b" }.missing)
        assertEquals(listOf("a.b"), AppFilter.withMissing(installed, setOf("a.b")).map { it.packageName })
    }

    @Test fun missingAppsAreFoundByTheirNameInTheSearch() {
        val shown = AppFilter.visible(AppFilter.withMissing(emptyList(), setOf("gone.app", "other.one")), "gone", false, setOf("gone.app", "other.one"))
        assertEquals(listOf("gone.app"), shown.map { it.packageName })
    }
}
