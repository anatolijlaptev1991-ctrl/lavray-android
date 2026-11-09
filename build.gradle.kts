// SPDX-License-Identifier: GPL-3.0-only

// The root project applies no plugin on purpose, and its build directory is moved below,
// so Gradle never writes into the root `build/` that belongs to the Go core build.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
}

// Gradle's own reports (configuration cache, problems) default to <root>/build; keep them out of it.
layout.buildDirectory.set(layout.projectDirectory.dir(".gradle/root-build"))
