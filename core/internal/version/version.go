// SPDX-License-Identifier: GPL-3.0-only

// Package version — единственный источник версии LAVRay для ядра: «Версия
// ядра» в UI (CoreStatus.core_version), самопроверка, текущая версия
// приложения в CheckUpdates и User-Agent подписок. Раньше их было два
// независимых плейсхолдера (inst.CoreVersion = "0.1.0-w6" и subs.Version =
// "0.1.0-w3"), которые разъезжались друг с другом и с приложением.
package version

// Version — версия LAVRay. Совпадает с versionName приложения Android в
// app/build.gradle.kts (это проверяет version_test.go). Сборку можно подменить
// флагом компоновщика (подменить можно только переменную со строковым литералом,
// поэтому остальные пакеты читают именно её):
//
//	go build -ldflags "-s -w -X github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version.Version=1.2.3"
var Version = "0.9.5"
