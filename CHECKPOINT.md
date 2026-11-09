# CHECKPOINT — LAVRay Android

Обновлено: 2026-11-10.

## Состояние
- Локальный git в `D:\ZCode\nekobox\lavray-android`, субмодуль `third_party/lavray` на `v0.9.3` (9899ae9).
- Создание публичного репозитория `anatolijlaptev1991-ctrl/lavray-android` **заблокировано** проверкой прав (Create Public Surface). Нужно подтверждение владельца. Обходить запрет другими командами нельзя.
- Решение по стеку: `docs/STACK.md`. Контракт: `docs/ARCHITECTURE.md`.
- SDK: `%LOCALAPPDATA%\Android\Sdk`, cmdline-tools поставлены, ставятся NDK 28.2.13676358 и образ android-36 google_apis x86_64.

## Заметки по среде
- Java на этом ПК падает с `Unable to establish loopback connection`, если TEMP длинный. Лечится `-Djdk.net.unixdomain.tmpdir=D:/ZCode/.t` и `TEMP=D:\ZCode\.t` (sdkmanager, Gradle).
- Python на ПК нет. Для скриптов использовать Node или Go.
- Запрещено трогать: LAVRay.exe, lavray-shell.exe, lavray-core.exe на порту 2080, системный прокси, TUN хоста, реестр, `%APPDATA%\LAVRay`, `%APPDATA%\nekoray`, `D:\ZCode\nekobox\lavray`.

## Что дальше
1. Мост Go и сборка AAR.
2. Сервисный слой Android, клиент API.
3. Интерфейс Compose.
4. Автозапуск, замеры, документация.
5. После разрешения владельца: публикация, CI, релиз.

## Проверка на эмуляторе (ветка verify/emulator, 2026-11-10)
- Android 16 x86_64 (google_apis), отладочный приёмник `app/src/debug` (`am broadcast ... lavray.debug.API`) вызывает API ядра; в релиз он не входит.
- Исправлено: SIGSYS на x86_64 (libc), tun0 после отключения, физическая сеть при поднятом VPN, подписка через удалённый mixed-in.
- Открыто для владельца: публичный IP и самопроверка «через туннель» не работают без mixed-in (см. docs/ARCHITECTURE.md).
