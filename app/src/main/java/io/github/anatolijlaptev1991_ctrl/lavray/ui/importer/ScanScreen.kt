// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import kotlinx.coroutines.launch
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Scans a QR code with the camera. The camera permission is asked for here, when it is first needed; when it is
 * refused (or the phone has no camera) the screen stays useful: it offers to ask again, to open the app's
 * settings, to read a QR image from a file, or to go back and paste the link. The camera is bound to the
 * lifecycle, so it is released the moment the screen is not started.
 */
@Composable
fun ScanScreen() {
    val context = LocalContext.current
    val env = LocalEnv.current
    val nav = LocalNavigator.current
    val hasCamera = remember { context.packageManager.hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY) }
    var granted by remember { mutableStateOf(hasPermission(context)) }
    var asked by remember { mutableStateOf(false) }
    val request = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok -> granted = ok; asked = true }
    LaunchedEffect(hasCamera) { if (hasCamera && !granted) request.launch(Manifest.permission.CAMERA) }

    val done = remember { AtomicBoolean(false) }
    val onCode: (String) -> Unit = { text ->
        env.importer.offerScanned(text)
        nav.back()
    }
    Box(Modifier.fillMaxSize()) {
        if (hasCamera && granted) {
            CameraPreview(done, onCode)
            Viewfinder()
        } else {
            Fallback(hasCamera, asked, onAsk = { request.launch(Manifest.permission.CAMERA) }, onCode = onCode)
        }
    }
}

private fun hasPermission(context: Context): Boolean =
    ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED

@Composable
private fun CameraPreview(done: AtomicBoolean, onCode: (String) -> Unit) {
    val context = LocalContext.current
    val owner = LocalLifecycleOwner.current
    val executor: ExecutorService = remember { Executors.newSingleThreadExecutor() }
    val main = remember { Handler(Looper.getMainLooper()) }
    val previewView = remember { PreviewView(context).apply { scaleType = PreviewView.ScaleType.FILL_CENTER } }

    DisposableEffect(owner) {
        val future = ProcessCameraProvider.getInstance(context)
        future.addListener({
            val provider = runCatching { future.get() }.getOrNull() ?: return@addListener
            val preview = Preview.Builder().build().also { it.surfaceProvider = previewView.surfaceProvider }
            val analysis = ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build()
            analysis.setAnalyzer(executor) { image ->
                val text = decodeFrame(image)
                image.close()
                if (text != null && done.compareAndSet(false, true)) main.post { onCode(text) }
            }
            provider.unbindAll()
            runCatching { provider.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis) }
        }, ContextCompat.getMainExecutor(context))
        onDispose {
            runCatching { future.get().unbindAll() }
            executor.shutdown()
        }
    }
    AndroidView({ previewView }, Modifier.fillMaxSize())
}

/** Reads the luminance plane of a camera frame; the QR reader is rotation tolerant, so the frame is not rotated. */
private fun decodeFrame(image: ImageProxy): String? {
    val plane = image.planes.firstOrNull() ?: return null
    val buffer = plane.buffer
    val data = ByteArray(buffer.remaining())
    buffer.get(data)
    return QrDecoder.decodeYPlane(data, plane.rowStride, image.width, image.height)
}

@Composable
private fun Viewfinder() {
    Box(Modifier.fillMaxSize()) {
        Canvas(Modifier.fillMaxSize()) {
            val side = minOf(size.width, size.height) * 0.62f
            val topLeft = Offset((size.width - side) / 2, (size.height - side) / 2)
            drawRoundRect(Color.White, topLeft, Size(side, side), CornerRadius(24.dp.toPx()), Stroke(3.dp.toPx()))
        }
        LavText(
            stringResource(R.string.imp_qr_aim),
            Modifier.align(Alignment.BottomCenter).padding(24.dp),
            style = Lav.type.body, color = Color.White, textAlign = TextAlign.Center,
        )
    }
}

@Composable
private fun Fallback(hasCamera: Boolean, asked: Boolean, onAsk: () -> Unit, onCode: (String) -> Unit) {
    val context = LocalContext.current
    val resources = LocalResources.current
    val nav = LocalNavigator.current
    val env = LocalEnv.current
    val scope = rememberCoroutineScope()
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri != null) scope.launch {
            when (val r = QrImageReader.read(context.contentResolver, uri)) {
                is QrImageResult.Found -> onCode(r.text)
                QrImageResult.NotFound -> env.toast.show(resources.getString(R.string.imp_qrNotFound))
                QrImageResult.Unreadable -> env.toast.show(resources.getString(R.string.imp_qrBadImage))
            }
        }
    }
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        LavText(
            stringResource(if (hasCamera) R.string.imp_cam_denied_t else R.string.imp_cam_none_t),
            style = Lav.type.h2, textAlign = TextAlign.Center,
        )
        LavText(
            stringResource(if (hasCamera) R.string.imp_cam_denied_d else R.string.imp_cam_none_d),
            style = Lav.type.body, color = Lav.colors.textSecondary, textAlign = TextAlign.Center,
        )
        if (hasCamera && !asked) {
            LavButton(stringResource(R.string.imp_cam_allow), onAsk, variant = ButtonVariant.Primary, icon = LavIcon.Camera, modifier = Modifier.fillMaxWidth())
        }
        if (hasCamera && asked) {
            LavButton(stringResource(R.string.imp_cam_settings), { openAppSettings(context) }, variant = ButtonVariant.Primary, modifier = Modifier.fillMaxWidth())
        }
        LavButton(stringResource(R.string.imp_qrFile), { picker.launch("image/*") }, icon = LavIcon.Image, modifier = Modifier.fillMaxWidth())
        LavButton(
            stringResource(R.string.imp_clipPaste), { nav.back(); env.importer.open() },
            icon = LavIcon.Clipboard, variant = ButtonVariant.Ghost, modifier = Modifier.fillMaxWidth(),
        )
    }
}

private fun openAppSettings(context: Context) {
    val intent = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null))
    runCatching { context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
}

