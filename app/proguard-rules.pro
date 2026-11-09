# SPDX-License-Identifier: GPL-3.0-only
# R8 full mode. Only what native code reaches by name is kept.

# gomobile: the Go runtime and the generated proxies are looked up from JNI by name.
-keep class go.** { *; }
-keep class lavray.core.** { *; }

# kotlinx.serialization ships its own consumer rules; nothing else is reflective.
