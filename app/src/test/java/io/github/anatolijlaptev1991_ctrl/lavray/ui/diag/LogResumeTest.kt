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
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.Assert.assertEquals
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

    @Test fun clearingEmptiesTheWindow() {
        val model = LogModel(Source().open)
        runWindow(model)
        model.clear()
        assertEquals(emptyList<String>(), model.lines.value.map { it.message })
    }
}
