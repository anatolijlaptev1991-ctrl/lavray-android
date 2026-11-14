// SPDX-License-Identifier: GPL-3.0-only
// LAV Preset RU Linux = LAV Preset RU без process_rules (правил для программ Windows).
// Запуск из корня репозитория: node tools/presets/lav-ru-linux.mjs. Пишет
// core-overlay/presets/lav-ru-linux.json; сверку с lav-ru держит core-overlay/default_preset_test.go.
import fs from 'fs';
const src = JSON.parse(fs.readFileSync('third_party/lavray/core/internal/presets/builtin/lav-ru.json', 'utf8'));
const p = structuredClone(src);
p.revision = 1;
p.id = 'lav-ru-linux';
for (const l of Object.keys(p.name)) p.name[l] = 'LAV Preset RU Linux';
p.description = {
  ru: 'LAV Preset RU без правил для программ Windows. Русские сайты (geosite category-ru/gov-ru), игровые домены, BigModel, DeepSeek и Kimi/Moonshot — напрямую; остальные AI- и dev-сервисы и прочий трафик — через прокси; реклама блокируется. Раздельный DNS: RU, игры, BigModel, DeepSeek и Kimi/Moonshot — DNS провайдера, остальное — DoH через туннель.',
  zh: '去掉 Windows 程序规则的 LAV Preset RU。俄罗斯网站（category-ru/gov-ru）、游戏域名、BigModel、DeepSeek 和 Kimi/Moonshot 直连；其他 AI 与开发服务及其余流量走代理；广告拦截。DNS 分流：俄罗斯网站、游戏、BigModel、DeepSeek 和 Kimi/Moonshot 走运营商 DNS，其余经隧道 DoH。',
  en: 'LAV Preset RU without the rules for Windows programs. Russian sites (geosite category-ru/gov-ru), game domains, BigModel, DeepSeek, and Kimi/Moonshot go direct; other AI/dev services and all other traffic go through the proxy; ads are blocked. Split DNS: RU, games, BigModel, DeepSeek, and Kimi/Moonshot via ISP DNS; everything else via DoH through the tunnel.',
  es: 'LAV Preset RU sin las reglas de programas de Windows. Los sitios rusos (geosite category-ru/gov-ru), los dominios de juegos, BigModel, DeepSeek y Kimi/Moonshot van directos; los demás servicios de IA/desarrollo y el resto del tráfico van por proxy; los anuncios se bloquean. DNS dividido: RU, juegos, BigModel, DeepSeek y Kimi/Moonshot usan el DNS del proveedor; el resto usa DoH a través del túnel.',
  de: 'LAV Preset RU ohne die Regeln für Windows-Programme. Russische Webseiten (geosite category-ru/gov-ru), Spiele-Domains, BigModel, DeepSeek und Kimi/Moonshot laufen direkt; andere KI-/Dev-Dienste und der übrige Datenverkehr laufen über den Proxy; Werbung wird blockiert. Getrenntes DNS: RU, Spiele, BigModel, DeepSeek und Kimi/Moonshot über den Provider-DNS, der Rest über DoH durch den Tunnel.',
};
p.maintainer_notes = 'LAVRay Android: копия lav-ru rev ' + src.revision + ' без process_rules (правил для программ Windows, *.exe). Все списки доменов и IP, custom_rules и DNS совпадают с lav-ru; сверку держит тест TestLinuxPresetIsLavRuWithoutWindowsApps (core-overlay/default_preset_test.go). При смене lav-ru пересоздать: node tools/presets/lav-ru-linux.mjs.';
p.routing.process_rules = [];
fs.writeFileSync('core-overlay/presets/lav-ru-linux.json', JSON.stringify(p, null, 2) + '\n');
