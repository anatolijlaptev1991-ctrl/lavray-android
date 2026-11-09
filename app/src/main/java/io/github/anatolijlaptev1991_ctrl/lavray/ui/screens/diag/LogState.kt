// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LogLevel
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LogLine
import java.time.Instant

/** Levels the filter offers, lowest first (desktop screen_log.tsx LEVELS). Panic is shown as an error. */
enum class LogSeverity(val label: String) {
    Trace("TRACE"), Debug("DEBUG"), Info("INFO"), Warn("WARN"), Error("ERROR");

    companion object {
        fun of(level: LogLevel): LogSeverity = when (level) {
            LogLevel.LOG_LEVEL_TRACE -> Trace
            LogLevel.LOG_LEVEL_DEBUG -> Debug
            LogLevel.LOG_LEVEL_WARN -> Warn
            LogLevel.LOG_LEVEL_ERROR, LogLevel.LOG_LEVEL_PANIC -> Error
            LogLevel.LOG_LEVEL_INFO, LogLevel.LOG_LEVEL_UNSPECIFIED -> Info
        }
    }
}

/** One log line. [seq] is a running number that keys the list; the text is exactly what the core sent (already redacted). */
@Immutable
data class LogEntry(val seq: Long, val tsMs: Long, val severity: LogSeverity, val source: String, val message: String) {
    /** `2026-11-10T12:00:00.123Z INFO core: message`, the desktop export line. */
    fun toLine(): String = "${Instant.ofEpochMilli(tsMs)} ${severity.label} $source: $message"
}

/** Fixed-size window over the newest lines. Not thread-safe: the model guards it. */
class LogBuffer(private val capacity: Int = CAPACITY) {
    private val items = ArrayDeque<LogEntry>()
    private var counter = 0L

    init {
        require(capacity > 0) { "capacity must be positive" }
    }

    fun push(line: LogLine) {
        counter += 1
        items.addLast(LogEntry(counter, line.tsMs, LogSeverity.of(line.level), line.source, line.message))
        while (items.size > capacity) items.removeFirst()
    }

    fun snapshot(): List<LogEntry> = items.toList()

    fun clear() = items.clear()

    companion object {
        /** About 2000 lines of a few hundred bytes: well under a megabyte, however long the screen stays open. */
        const val CAPACITY = 2000
    }
}

/** Level and text filter (desktop screen_log.tsx), and the text handed to Copy and Share. */
object LogFilter {
    fun apply(list: List<LogEntry>, min: LogSeverity, query: String): List<LogEntry> {
        val needle = query.trim().lowercase()
        if (min == LogSeverity.Trace && needle.isEmpty()) return list
        return list.filter { e ->
            e.severity >= min && (needle.isEmpty() || e.message.lowercase().contains(needle) || e.source.lowercase().contains(needle))
        }
    }

    /**
     * Newest lines that fit [maxChars], oldest first. Share hands the text over a Binder transaction (about 1 MB
     * for everything in flight), so an unbounded log would fail there; Copy uses the same bound for consistency.
     */
    fun export(list: List<LogEntry>, maxChars: Int = EXPORT_MAX_CHARS): String {
        val lines = ArrayDeque<String>()
        var total = 0
        for (e in list.asReversed()) {
            val line = e.toLine()
            if (total + line.length + 1 > maxChars && lines.isNotEmpty()) break
            lines.addFirst(line)
            total += line.length + 1
        }
        return lines.joinToString("\n")
    }

    const val EXPORT_MAX_CHARS = 200_000
}
