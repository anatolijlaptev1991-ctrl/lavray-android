// SPDX-License-Identifier: GPL-3.0-only

// Package links — Link2Bean/Bean2Link: разбор и генерация share-ссылок
// профилей (порт fmt/Link2Bean.cpp + fmt/Bean2Link.cpp из nekoray; W2,
// core-port-plan §4.1, §9-W2; FR-UI-190/191/164).
//
// Поддерживаемые схемы (импорт и экспорт):
//
//	схема                      | импорт | экспорт (BuildLink) | заметки
//	---------------------------+--------+---------------------+------------------
//	ss://                      | да     | да                  | традиционный (base64url userinfo), 2022 (plain userinfo) и v2rayN (полный base64)
//	vmess://                   | да     | да                  | v2rayN base64-JSON (по умолчанию, FR-UI-164) и URL-формат (XTLS #716)
//	vless://                   | да     | да                  |
//	trojan://                  | да     | да                  |
//	hy2:// / hysteria2://      | да     | да (hy2)            |
//	tuic://                    | да     | да                  |
//	socks:// socks4:// socks5:// | да  | да (socks5/socks4)  |
//	http:// / https://         | да     | да                  | https => security=tls
//	nekoray://                 | да     | да (BuildNekoLink)  | FR-UI-191, все типы, включая custom/chain
//
// Не поддерживается здесь:
//
//   - happ:// — расшифровка happ-ссылок выполняется в W3
//     (internal/subs/happ, порт go/cmd/nekobox_core/happ + фикс LNB-13);
//     ParseLink возвращает понятную ошибку.
//   - naive+https:// — экспорт отключён (внешние ядра за флагом, решение №5);
//     BuildLink для типа naive возвращает ошибку «не экспортируется».
//   - custom*/chain — не имеют share-схемы; используйте BuildNekoLink
//     (FR-UI-191).
//
// Применённые исправления дефектов NekoBox:
//
//   - LNB-15 (мёртвый дефолт порта TUIC/QUIC): порт проверяется на
//     1..65535 для ВСЕХ схем; дефолты (tuic/hy2/trojan/vless/vmess-URL — 443,
//     socks — 1080, http — 80, https — 443) применяются ровно один раз после
//     парсинга и не перетираются; ссылка с портом 0/70000 — внятная ошибка,
//     порт 0 никогда не попадает в модель.
//   - LNB-17 (слабая валидация полей): VMess-порт обязателен (0/отсутствие —
//     ошибка), метод Shadowsocks — по белому списку Preset::SingBox::
//     ShadowsocksMethods, flow — по Preset::Flows, uTLS-отпечаток — по
//     Preset::UtlsFingerPrint, security/vmess-scy — по спискам; длины полей
//     ограничены (см. лимиты ниже). Ошибки привязаны к полю.
//
// Канонизация (core-port-plan §4.1 «Нормализация», LNB-15/17):
// trim всей ссылки; схема — lowercase; security none/0/false→"",
// 1/true→tls; REALITY: security=reality ⇔ reality_public_key непусто;
// сеть h2→http; ALPN — тримминг по запятой; IPv6-хост — каноническая форма
// net.IP.String(); SNI наследуется из Host, если адрес — IP, SNI пуст и TLS
// включён; пароль/логин — decode %-escape (net/url).
//
// Отклонения от C++-oracle (фиксируются также в deviations.yaml, §3.3 плана):
//
//   - кодирование компонентов: Qt FullyEncoded vs собственный percent-encoder
//     (только unreserved-символы не экранируются; '+' в query не используется
//     как пробел — как в Qt);
//   - base64: oracle местами прощает некорректный padding — LAVRay в vmess/
//     ss-userinfo принимает только строгий padded base64 (std и url);
//   - http:// без порта — дефолт 80 (oracle: 443);
//   - allowInsecure=0/false больше не включает allow_insecure (oracle:
//     «любое непустое значение»);
//   - utls-отпечаток из глобальных настроек НЕ подставляется парсером
//     (в модели "" = глобальный дефолт);
//   - query-параметр, отсутствующий и присутствующий-пустой, различаются
//     (oracle GetQueryValue смешивает их);
//   - расширения для полноты roundtrip (NekoBox эти параметры не переносил):
//     hy2 — alpn/hop_interval/upmbps/downmbps/disable_sni; tuic —
//     zero_rtt_handshake/heartbeat; vmess v2rayN-JSON — alpn/fp/pbk/sid/spx;
//     socks/http экспорт дополнительно кодирует sni/security в query
//     (импорт их читает ещё в NekoBox);
//   - LNB-25/FR-UI-257: у nekoray://-ссылок поля c_cfg/c_out ОТБРАСЫВАЮТСЯ
//     на входе парсера (импортированные профили не могут доставить конфиг
//     ядра; ручное включение — двухфазный ImportContent в UI, W3).
package links

// Дополнения по ревью L2 (октябрь 2026):
//   - IPv6-зона (fe80::1%eth0) в адресе отвергается (roundtrip-инвариант).
//   - allowInsecure/insecure/allow_insecure: строго 1/true (иначе false) —
//     строже oracle «непусто → true».
//   - vmess "aid":"" (пустая строка) → 0 (как oracle toInt); aid вне 0..65535 — ошибка.
//   - vmess v2rayN экспорт пишет port/aid числами (v2rayN — строками; клиенты толерантны).
//   - socks/http base64-логин без ':' остаётся base64-текстом (oracle дублровал
//     decoded в username и password — сам оракул квоздравно).
//   - hy2 obfs: только salamander (oracle ключ игнорировал); сети вне whitelist — ошибка.
