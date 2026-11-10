// SPDX-License-Identifier: GPL-3.0-only

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Release signing comes from four environment variables. When any is missing the
// release build falls back to the debug key (local builds only). No keystore is
// ever created or stored in the repository.
val signingEnv: Map<String, String> = listOf(
    "LAVRAY_KEYSTORE_PATH",
    "LAVRAY_KEYSTORE_PASSWORD",
    "LAVRAY_KEY_ALIAS",
    "LAVRAY_KEY_PASSWORD",
).mapNotNull { name -> providers.environmentVariable(name).orNull?.takeIf { it.isNotBlank() }?.let { name to it } }.toMap()
val hasReleaseSigning = signingEnv.size == 4

// The Go core AAR is produced by scripts/build-core.sh. Until it exists the
// stubs in src/coreStub compile in its place; once the file appears the stubs
// drop out and the AAR is used, with no further change here.
val coreAar = layout.projectDirectory.file("libs/lavray-core.aar").asFile
val useCoreStub = !coreAar.exists()

// Opt-in for device measurements (docs/MEASUREMENTS.md): -Plavray.measure=true adds the debug-only
// shell driver (guarded by android.permission.DUMP) to the minified release build, so release
// code can be driven by adb. Off by default and never used for published builds.
val measureBuild = providers.gradleProperty("lavray.measure").map { it.toBoolean() }.getOrElse(false)

// versionCode of an APK is baseVersionCode * 10 + the index of its ABI (abiVersionIndex): the three ABI APKs must not
// share one code, or a 32-bit APK installs over a 64-bit one as a plain update and silently drops the app to a 32-bit
// runtime. scripts/ci-apk.sh verify checks the result; keep the name and the literal on one line.
val baseVersionCode = 903
val abiVersionIndex = mapOf("armeabi-v7a" to 1, "arm64-v8a" to 2, "x86_64" to 3)

android {
    namespace = "io.github.anatolijlaptev1991_ctrl.lavray"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.anatolijlaptev1991_ctrl.lavray"
        minSdk = 26
        targetSdk = 36
        versionCode = baseVersionCode
        versionName = "0.9.3"
    }

    signingConfigs {
        if (hasReleaseSigning) {
            create("release") {
                storeFile = file(signingEnv.getValue("LAVRAY_KEYSTORE_PATH"))
                storePassword = signingEnv.getValue("LAVRAY_KEYSTORE_PASSWORD")
                keyAlias = signingEnv.getValue("LAVRAY_KEY_ALIAS")
                keyPassword = signingEnv.getValue("LAVRAY_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName(if (hasReleaseSigning) "release" else "debug")
        }
    }

    // One APK per ABI, no universal APK: every installed copy carries one Go library.
    splits {
        abi {
            isEnable = true
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = false
        }
    }

    buildFeatures {
        compose = true
        aidl = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    packaging {
        jniLibs {
            // Compressed in the APK and extracted at install: the APK that every update downloads is 61% smaller
            // (arm64-v8a 52.96 -> 20.65 MB) for +19 MB installed; first launch unchanged, :vpn PSS 2.4 MB lower
            // (docs/MEASUREMENTS.md). -Plavray.legacyJni=false builds the old layout (stored, page-aligned,
            // mapped from the APK) for comparison.
            useLegacyPackaging = providers.gradleProperty("lavray.legacyJni").map { it.toBoolean() }.getOrElse(true)
        }
        resources.excludes += setOf("/META-INF/{AL2.0,LGPL2.1}", "DebugProbesKt.bin", "kotlin-tooling-metadata.json")
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }

    sourceSets.getByName("main").java.srcDir("src/main/java")
    if (useCoreStub) {
        sourceSets.getByName("main").java.srcDir("src/coreStub/java")
    }
    if (measureBuild) {
        sourceSets.getByName("release").java.srcDir("src/debug/java")
        sourceSets.getByName("release").kotlin.directories.add("src/debug/java")
        sourceSets.getByName("release").manifest.srcFile("src/debug/AndroidManifest.xml")
    }
}

androidComponents {
    onVariants { variant ->
        variant.outputs.forEach { output ->
            val abi = output.filters
                .firstOrNull { it.filterType == com.android.build.api.variant.FilterConfiguration.FilterType.ABI }?.identifier
            // An output without an ABI filter (no universal APK is built) keeps the base code times ten.
            output.versionCode.set(baseVersionCode * 10 + (abiVersionIndex[abi] ?: 0))
        }
    }
}

dependencies {
    if (!useCoreStub) {
        implementation(files(coreAar))
    }
    implementation(libs.kotlinx.coroutines.core)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.foundation)
    implementation(libs.activity.compose)
    implementation(libs.camerax.camera2)
    implementation(libs.camerax.lifecycle)
    implementation(libs.camerax.view)
    implementation(libs.zxing.core)

    testImplementation(libs.junit)
}
