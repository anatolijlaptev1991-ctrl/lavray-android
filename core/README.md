# core/ — Go-ядро LAVRay Android

Go-ядро LAVRay: порт логики из `nekoray/go` (nekobox_core) + конвейер генерации конфигов
ConfigBuilder (оригинал — `db/ConfigBuilder.cpp` в nekoray, порт на Go с golden-тестами).

Ответвлено от ядра настольной LAVRay 0.9.3 (репозиторий `lavray`, коммит `9899ae9`) и с 0.9.5
развивается здесь только для Android: общее ядро для Windows, Linux, Android и OpenWrt оказалось
слишком сложным. Модуль Go — `github.com/anatolijlaptev1991-ctrl/lavray-android/core`. Убраны
части, которые Android не собирает: `cmd/` (настольный бинарь и подпись обновлений),
`internal/headless`, `internal/mcp`, `internal/conformance`, `internal/buildtags`. Доработки
под телефон, раньше лежавшие патчами в `core-overlay/`, внесены прямо в код. Сборка AAR —
`../scripts/build-core.sh`.

## Базовая линия

| Компонент | Источник | Пин |
|---|---|---|
| sing-box (форк с neko-слоем) | `https://github.com/anatolijlaptev1991-ctrl/sing-box` | **`35f3fd0`** (= sing-box 1.14.1-neko.1 `628a9ed` + патч P1 из `NEKO_PATCHES.md`) |

Форк подключается в `go.mod` через `replace` на зафиксированный коммит. Изменение пина —
только через гейт обновления из [UPDATE-SYSTEM.md в lavray 0.9.3](https://github.com/anatolijlaptev1991-ctrl/lavray/blob/v0.9.3/docs/UPDATE-SYSTEM.md) (аудит changelog, лицензия upstream,
перенос neko-патчей, полный прогон L1/L2, smoke-тесты коннективности, тег `core-<version>`).

## Структура

- `mobile/` — мост с Android (пакет gomobile `mobile`, Java-пакет `lavray.core.mobile`): запуск ядра в процессе `:vpn`, платформа для sing-box, приведение конфига к правилам Android, списки приложений.
- `internal/cfgbuild/` — ConfigBuilder: профили → конфиг sing-box (golden-тесты).
- `internal/model/` — модель профилей, групп и настроек.
- `internal/store/` — хранилище профилей/групп/настроек (SQLite); `store/nekoimport` — импортёр каталога NekoBox (экран переноса, `MigrationService`).
- `internal/links/` — парсеры ссылок.
- `internal/subs/` — подписки, импорт/экспорт, декриптор ссылок других клиентов.
- `internal/presets/` — формат пресетов LAVRay и конвертер из NekoRay 4.x (см. `internal/presets/README.md` и `SCHEMA.md`; встроенные наборы — `internal/presets/builtin`).
- `internal/inst/` — менеджер инстанса sing-box.
- `internal/api/` — реализация сервисов из `proto/lavray.proto`; без заглушек, кроме `WebSessionService`, который отвечает только с `--web-bind` (список — `rpc_ratchet_test.go`).
- `internal/traffic/`, `internal/backup/`, `internal/textdiff/` — учёт трафика и скорость соединений, зашифрованная резервная копия (LAVBK1), diff для подтверждения своего JSON.
- `internal/logs/`, `internal/updates/`, `internal/web/`, `internal/gateway/` — журнал, клиент обновлений (подключён; без корня доверия отвечает «обновления не настроены»), веб-панель и шлюз OpenWrt (на телефоне не включаются; их убирать — отдельная задача, от них зависят пакеты API).

## Neko-патчи над sing-box

Слой интеграции с sing-box документируется пофайлово: [`NEKO_PATCHES.md`](NEKO_PATCHES.md).
Список ведётся всегда актуальным — он используется гейтом обновления ядра.

## Справочные документы

- [AUDIT.md в lavray 0.9.3](https://github.com/anatolijlaptev1991-ctrl/lavray/blob/v0.9.3/docs/AUDIT.md) — аудит Go-ядра NekoBox (входные данные порта).
- [BUGS.md в lavray 0.9.3](https://github.com/anatolijlaptev1991-ctrl/lavray/blob/v0.9.3/docs/BUGS.md) — каталог багов (LNB-NN), которые порт не должен воспроизвести.
- [REQUIREMENTS.md в lavray 0.9.3](https://github.com/anatolijlaptev1991-ctrl/lavray/blob/v0.9.3/docs/REQUIREMENTS.md) — карта «фича → файл-источник» (раздел 19), Приложения А/Б.
