# presets/SCHEMA.md — формат маршрутных пресетов LAVRay (схема v1, правка 2)

> ЧЕРНОВИК Этапа 3 (stage3-drafts), ревизия 2 по L2-ревью 2026-10-23.
> Соответствует [PRESET-LAV-RU.md в lavray 0.9.3](https://github.com/anatolijlaptev1991-ctrl/lavray/blob/v0.9.3/docs/PRESET-LAV-RU.md) §3, §3a–§3d (актуальная редакция) и
> `lavray/presets/README.md`. Файлы набора:
> [`lav-ru.json`](lav-ru.json) (rev 3) · [`lav-cn.json`](lav-cn.json) (rev 2) ·
> [`global.json`](global.json) · [`all-proxy.json`](all-proxy.json) ·
> [`all-direct.json`](all-direct.json) — все 5 builtin (PRESET-LAV-RU §3a;
> «Выборочный» — пустой пользовательский набор, не файл). Эталон конвертации
> из NekoRay 4.x — `lavray/presets/lav-ru.source.json`.

## 1. Структура файла

| Поле | Тип | Описание |
|---|---|---|
| `$schema` | string | Ссылка на схему (файл `preset.schema.v1.json` в репо; внешних запросов нет). |
| `version` | int | Версия СХЕМЫ (1). Меняется при изменении структуры; ядро читает все минорные назад. |
| `revision` | int | Версия ДАННЫХ, монотонно растёт. Изменение доменов/процессов = `revision+1` + PR + golden-тесты. В манифесте обновлений пресет идёт с `rev` (не semver, UPDATE-SYSTEM §8). |
| `id` | string | Слаг: `lav-ru`, `lav-cn`, `global`, `all-proxy`, `all-direct`, пользовательские `custom:<slug>`. |
| `builtin` | bool | Зашит в сборку: нельзя Update/Delete — только клонировать или Reset к revision. |
| `name` | map<lang,string> | Бренды не переводятся; говорящие имена переводятся (5 языков). |
| `description` | map<lang,string> | Тултип/онбординг, 5 языков. |
| `maintainer_notes` | string | Не-нормативно; в UI и golden не участвует. |
| `routing` | object | §2. |
| `dns` | object | §3. |

## 2. Блок `routing`

| Поле | Тип | NekoRay 4.x | Примечание |
|---|---|---|---|
| `def_outbound` | `proxy`\|`direct`\|`block` | `def_outbound` | FR-UI-139. |
| `outbound_domain_strategy` | `""`\|`ipv4_only`\|`ipv6_only`\|`prefer_ipv4`\|`prefer_ipv6` | `outbound_domain_strategy` | FR-UI-133 (MUST). |
| `sniffing_mode` | `0`\|`1` | `sniffing_mode` | FR-UI-132: только Disable/For-routing; третий режим некора — DROP аудита (в схеме отсутствует; конвертер логирует warning). |
| `proxy_domain`, `direct_domain`, `block_domain` | array\<string\> | `proxy_domain`/`direct_domain` (+block из 6 списков Routing) | Синтаксис FR-UI-135 (`geosite:`, `geoip:` (в ip), `domain:`, `full:`, `keyword:`, `regexp:`, суффикс, `#`-комментарий). **`block_domain` у всех builtin содержит `geosite:category-ads-all`** (правило рекламы, PRESET-LAV-RU §3b). |
| `proxy_ip`, `direct_ip`, `block_ip` | array\<string\> | `proxy_ip`/`direct_ip` | CIDR/`geoip:X`. `.srs` — из подписанного набора поставки, `download_detour`=прокси при туннеле (LNB-31/36). |
| `process_rules` | array (§2.1) | внутри `custom` | Структурные platform-scoped правила (§3c PRESET-LAV-RU). |
| `custom_rules` | string (JSON, опц.) | `custom` (кроме процессных) | FR-UI-137; валидация при загрузке. |

**Приоритет правил** (конфигбилдер, core-port-plan §3.1.5): `block → proxy
(явные списки) → direct → process_rules пресета → tun-bypass → built-in →
custom`. Proxy перед direct-geosite сохраняет семантику `google.ru`
(PRESET-LAV-RU §3.3). Whitelist-режим TUN инвертирует только tun-списки.

### 2.1 `process_rules[]`

```json
{
  "platforms": ["windows"],          // пусто = все платформы
  "outbound": "proxy",               // proxy | direct | block
  "process_names": ["Cursor.exe"]    // Windows: *.exe; POSIX: basename
}
```

- `*.exe` автоматически считается windows-only (§3c); POSIX-имена — отдельными
  записями `platforms: ["linux"]/["macos"]`.
- На OpenWrt процессные правила опускаются с локализованным предупреждением
  («Не действует на роутере»); ядро не вставляет их в конфиг.
- Автобайпас путей внешних ядер добавляет конфигбилдер (FR-UI-234) — в
  пресете не дублируется.

## 3. Блок `dns`

| Поле | Тип | NekoRay | Примечание |
|---|---|---|---|
| `mode` | `simple`\|`advanced` | `use_dns_object` | advanced = полный JSON dns sing-box (FR-UI-149). |
| `remote_dns` | string | `remote_dns` | Трансляция в типизированные серверы 1.12+ (FR-UI-155). |
| `remote_query_strategy` | enum как `outbound_domain_strategy` | — | Query Strategy — единственная живая DNS-стратегия некора (FR-UI-148). |
| `direct_dns` | string | `direct_dns` | Обычно `local`. |
| `routing_enabled` | bool | `dns_routing` | FR-UI-150. |
| `underlying_dns` | string | Core Options | FR-UI-152. |

**DROP аудита 2026-10 (в схеме отсутствуют, конвертер игнорирует с warning):**
`domain_strategy` (FR-UI-131 — мёртвое поле), `direct_dns_strategy`,
`dns_final_out` (FR-UI-148 — не влияли на конфиг). Пере-включение — решение
владельца (см. допущения stage3-drafts/README.md).

## 4. Валидация (ядро, при загрузке)

1. Структура по схеме (`version ≤ поддерживаемой`); ошибки — с путём поля.
2. Строки списков: непустые, известные префиксы; `geoip:` — только ip-поля,
   `geosite:` — только domain-поля.
3. `process_names`: только имена (без путей, `..`, слэшей) — анти-инъекция.
4. `dns.mode=advanced` — валидация парсером конфига форка (LNB-08).
5. Golden: каждый builtin конвертируется детерминированно (G28–G30 в
   core-port-plan §3.2) + `sing-box check`; приоритеты — тестами
   «google.ru → proxy, bigmodel.cn → direct, deepseek.com → direct, api.kimi.ai → direct, zcode.z.ai → proxy» (RU), «baidu.com → direct, google.com → proxy» (CN),
   «lan → direct, ads → block» (Global/AllProxy/AllDirect); G34 — старт с
   geosite-правилом без сети (кэш/поставка, FR-UI-266).

## 5. Обновление пресетов

- Подписываются тем же механизмом TUF-lite, что и релизы: секция `presets`
  манифеста — `{rev, path, sha256}` (UPDATE-SYSTEM **v2.0** §3.3/§8;
  PRESET-LAV-RU §3d); serial манифеста защищает от downgrade.
- Кастомизированный builtin (`customized=1` в SQLite) при обновлении —
  только diff/merge, никогда молча; `ResetBuiltinPreset` возвращает заводскую
  ревизию, кастомизация сохраняется отдельным набором.

## 6. Конвертация NekoRay 4.x → LAVRay

Источник — файлы **`config/routes_box/<name>`** (поправка аудита 2026-10);
каталог `routes/` старых форков принимается тоже (tolerate).

| NekoRay | LAVRay |
|---|---|
| `custom` с правилами `process_name` | `routing.process_rules[]` (proxy/bypass/block → proxy/direct/block) |
| `custom` без процессных правил | `routing.custom_rules` |
| `proxy_domain`/`direct_domain`/`proxy_ip`/`direct_ip` (строки `\n`) | массивы (построчно, `#` вырезается) |
| `block_domain`/`block_ip` (из 6-списков Routing, в файлах наборов обычно пусто) | соответствующие массивы |
| `def_outbound`, `sniffing_mode`, `outbound_domain_strategy` | те же поля (sniffing 2 → warning + 1) |
| `domain_strategy` | — DROP (игнорируется, warning; FR-UI-131) |
| `remote_dns`/`direct_dns`/`dns_routing`/`use_dns_object` | `dns.*` (`use_dns_object: true` → `mode: advanced` + объект в `advanced_json`) |
| `direct_dns_strategy`, `dns_final_out` | — DROP (игнорируется, warning; FR-UI-148) |

Конвертер — в ядре (`internal/presets`, `RoutingService.ImportNekoRayRoute`);
`lav-ru.source.json` — эталонный вектор (сгенерированный им `lav-ru.json`
должен совпадать с файлом в этом наборе byte-for-byte — тест); fuzzing —
`FuzzConvertNekoRayRoute` (core-port-plan §4.3).

## 7. Онбординг (FR-UI-276)

Предвыбор пресета при первом запуске: язык ru → LAV Preset RU; zh → LAV Preset
CN; остальные → Глобальный (уточнение по часовому поясу: UTC+8 → CN, зона
RU/калуга-… → RU) — правила предвыбора уточняются на ревью UI (допущение).
Порядок карточек в UI: RU, CN, Глобальный, Всё через прокси, Всё напрямую,
Выборочный (PRESET-LAV-RU §3a).

## 8. Пользовательские наборы и обмен файлом (T-80)

Пользовательские наборы хранятся в том же формате (`builtin: false`, id
`custom:<slug>`) и обмениваются файлами (`RoutingService.ExportPreset` /
`ImportPreset`). Дополнения к схеме v1 (поля необязательные, файлы встроенных
наборов не меняются):

| Поле | Тип | Описание |
|---|---|---|
| `source_id` | string | Встроенный набор, с которого снята копия. Пишет ядро; из импортируемого файла игнорируется. |
| `source_revision` | int | `revision` источника в момент копирования. Если у встроенного она выше, UI показывает «Исходный набор обновился», а `ResetBuiltinPreset` возвращает копию к источнику. |
| `routing.custom_rules` | string (JSON) | Массив правил sing-box `[{…}]` или `{"rules":[{…}]}`; хранится массивом, элементы — только объекты, до 256 КиБ. |
| `routing.process_rules[].platforms` | array | Допустимы `windows`, `linux`, `macos`, `openwrt`. |

Лимиты: 100 пользовательских наборов (все вместе до 4 МиБ), имя до 64 символов,
до 10 000 записей в каждом из 6 списков, до 200 процессных правил по 64 имени,
файл импорта до 1 МиБ.

**Импорт** принимает набор LAVRay (есть объект `routing`) либо маршрут NekoRay
4.x (§6; формат определяется по полям) и всегда создаёт новый пользовательский
набор: `id`, `revision`, `builtin` и источник из файла не используются — файл
не может занять id встроенного набора или перезаписать существующий.
