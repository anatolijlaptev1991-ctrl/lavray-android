// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Line icons on a 24 grid with a 1.75 stroke (section 5), as SVG path data. Parsed once on first
 * draw and drawn as vectors: no bitmaps, no icon font, no resource per icon.
 */
enum class LavIcon(private val data: String) {
    Zap("M13 2 3 14h9l-1 8 10-12h-9l1-8z"),
    Layers("M12 2 2 7l10 5 10-5-10-5z M2 17l10 5 10-5 M2 12l10 5 10-5"),
    Route("M3 19a3 3 0 1 0 6 0 3 3 0 1 0-6 0 M15 5a3 3 0 1 0 6 0 3 3 0 1 0-6 0 M9 19h8.5a3.5 3.5 0 0 0 0-7h-11a3.5 3.5 0 0 1 0-7H15"),
    Activity("M22 12h-4l-3 9L9 3l-3 9H2"),
    Sliders("M21 4h-7 M10 4H3 M21 12h-9 M8 12H3 M21 20h-5 M12 20H3 M14 2v4 M8 10v4 M16 18v4"),
    Plug("M12 22v-5 M9 8V2 M15 8V2 M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8z"),
    Plus("M5 12h14 M12 5v14"),
    Qr("M3 3h5v5H3z M16 3h5v5h-5z M3 16h5v5H3z M21 16h-3a2 2 0 0 0-2 2v3 M21 21v.01 M12 7v3a2 2 0 0 1-2 2H7 M3 12h.01 M12 3h.01 M12 16v.01 M16 12h1 M21 12v.01 M12 21v-1"),
    Camera("M14.5 4h-5L7 7H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-3l-2.5-3z M12 11a3 3 0 1 0 0 6 3 3 0 0 0 0-6z"),
    Image("M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z M9 8.5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3z M21 15l-5-5L5 21"),
    Clipboard("M9 2h6v4H9z M9 4H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2h-2"),
    Link("M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71 M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"),
    Copy("M9 9h11v11H9z M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"),
    Trash("M3 6h18 M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6 M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2 M10 11v6 M14 11v6"),
    Share("M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8 M16 6l-4-4-4 4 M12 2v13"),
    Check("M20 6 9 17l-5-5"),
    Close("M18 6 6 18 M6 6l12 12"),
    ChevronDown("m6 9 6 6 6-6"),
    ChevronRight("m9 18 6-6-6-6"),
    ArrowLeft("M19 12H5 M12 19l-7-7 7-7"),
    More("M12 5.01V5 M12 12.01V12 M12 19.01V19"),
    Search("M11 3a8 8 0 1 0 0 16 8 8 0 0 0 0-16z M21 21l-4.3-4.3"),
    Gauge("M12 14l4-4 M3.34 19a10 10 0 1 1 17.32 0"),
    Rss("M4 11a9 9 0 0 1 9 9 M4 4a16 16 0 0 1 16 16 M5 19.01V19"),
    Alert("M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z M12 9v4 M12 17.01V17"),
    Star("M12 2l3.1 6.3 6.9 1-5 4.9 1.2 6.8-6.2-3.3-6.2 3.3L7 14.2 2 9.3l6.9-1z"),
    Pencil("M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5z"),
    Folder("M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.8-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2z"),
    Refresh("M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8 M21 3v5h-5 M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16 M8 16H3v5"),
    Stop("M6 6h12v12H6z"),
    ArrowUp("M12 19V5 M5 12l7-7 7 7"),
    ArrowDown("M12 5v14 M19 12l-7 7-7-7"),
    File("M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z M14 2v6h6"),
    Sort("m21 16-4 4-4-4 M17 20V4 M3 8l4-4 4 4 M7 4v16"),
    Chart("M12 20V10 M18 20V4 M6 20v-4"),
    Download("M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M7 10l5 5 5-5 M12 15V3"),
    Globe("M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20z M2 12h20 M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"),
    List("M8 6h13 M8 12h13 M8 18h13 M3 6h.01 M3 12h.01 M3 18h.01"),
    Network("M9 2h6v6H9z M2 16h6v6H2z M16 16h6v6h-6z M12 8v4 M5 16v-4h14v4"),
    Shield("M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"),
    Info("M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20z M12 16v-4 M12 8.01V8");

    /** Parsed lazily and shared: the path is only read after that. */
    val path: Path by lazy(LazyThreadSafetyMode.NONE) { PathParser().parsePathString(data).toPath() }
}

private const val GRID = 24f
private const val STROKE = 1.75f

/** A [LavIcon] at [size]; decorative unless the caller gives the surrounding element a label. */
@Composable
fun LavIconView(icon: LavIcon, tint: Color, modifier: Modifier = Modifier, size: Dp = 20.dp) {
    Canvas(modifier.size(size)) {
        val k = this.size.minDimension / GRID
        scale(k, k, pivot = androidx.compose.ui.geometry.Offset.Zero) {
            drawPath(
                path = icon.path,
                color = tint,
                style = Stroke(width = STROKE, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
        }
    }
}
