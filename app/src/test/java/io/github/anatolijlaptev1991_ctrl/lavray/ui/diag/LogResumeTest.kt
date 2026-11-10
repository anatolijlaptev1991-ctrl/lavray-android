// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LogLevel
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LogLine
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogBuffer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogModel
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** E-12: every return to the app opens a new log subscription, and it replays the tail. */
class LogResumeTest {
    private class Source {
        val asked = mutableListOf<Int?>()
        val open: (Int?) -> Flow<LogLine> = { tail ->
            asked += tail
            flow {
                (1..3).forEach { emit(LogLine(1_700_000_000_000L + it, LogLevel.LOG_LEVEL_INFO, "core", "m$it")) }
                awaitCancellation()
            }
        }
    }

    private fun runWindow(model: LogModel) = runBlocking { withTimeoutOrNull(400) { model.stream() } }

    @Test fun reopeningTheStreamDoesNotAddTheTailAgain() {
        val source = Source()
        val model = LogModel(source.open)
        runWindow(model)
        assertEquals(listOf("m1", "m2", "m3"), model.lines.value.map { it.message })
        runWindow(model)
        runWindow(model)
        assertEquals(listOf("m1", "m2", "m3"), model.lines.value.map { it.message })
        assertEquals(listOf<Int?>(LogModel.TAIL_LINES, LogModel.TAIL_LINES, LogModel.TAIL_LINES), source.asked)
    }

    @Test fun shareCarriesLessTextThanCopy() {
        val buffer = LogBuffer(5_000)
        (1..4_000).forEach { buffer.push(LogLine(1_700_000_000_000L + it, LogLevel.LOG_LEVEL_INFO, "core", "x".repeat(100))) }
        val all = buffer.snapshot()
        assertTrue(LogFilter.export(all, LogFilter.SHARE_MAX_CHARS).length <= LogFilter.SHARE_MAX_CHARS)
        assertTrue(LogFilter.SHARE_MAX_CHARS < LogFilter.EXPORT_MAX_CHARS)
    }

    @Test fun aCutExportSaysHowManyLinesWereTaken() {
        val buffer = LogBuffer(5_000)
        (1..4_000).forEach { buffer.push(LogLine(1_700_000_000_000L + it, LogLevel.LOG_LEVEL_INFO, "core", "x".repeat(100))) }
        val all = buffer.snapshot()
        val cut = LogFilter.exportWithInfo(all, LogFilter.SHARE_MAX_CHARS)
        assertTrue(cut.truncated)
        assertEquals(4_000, cut.totalLines)
        assertTrue(cut.lines in 1 until 4_000)
        assertEquals(cut.lines, cut.text.lines().size)
        val whole = LogFilter.exportWithInfo(all.take(10), LogFilter.SHARE_MAX_CHARS)
        assertFalse(whole.truncated)
    }

    @Test fun theWindowKeepsItsLinesUntilTheTailOfTheNewSubscriptionArrives() = runBlocking {
        // Not emptied up front: the list must not flash empty on every return to the app.
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        val opens = java.util.concurrent.atomic.AtomicInteger()
        val model = LogModel { _ ->
            if (opens.incrementAndGet() == 1) {
                flow { emit(LogLine(1_700_000_000_000L + 1, LogLevel.LOG_LEVEL_INFO, "core", "old")); awaitCancellation() }
            } else {
                flow {
                    release.await()
                    emit(LogLine(1_700_000_000_000L + 9, LogLevel.LOG_LEVEL_INFO, "core", "tail"))
                    awaitCancellation()
                }
            }
        }
        runWindow(model)
        assertEquals(listOf("old"), model.lines.value.map { it.message })
        val scope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.Default)
        val second = scope.launch { model.stream() }
        kotlinx.coroutines.delay(200)
        assertEquals("still the old window while the new tail has not arrived", listOf("old"), model.lines.value.map { it.message })
        release.complete(Unit)
        withTimeout(5_000) { model.lines.first { l -> l.map { it.message } == listOf("tail") } }
        second.cancel()
        scope.cancel()
    }

    @Test fun clearingEmptiesTheWindow() {
        val model = LogModel(Source().open)
        runWindow(model)
        model.clear()
        assertEquals(emptyList<String>(), model.lines.value.map { it.message })
    }
}
