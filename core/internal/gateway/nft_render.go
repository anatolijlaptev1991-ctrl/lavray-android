// SPDX-License-Identifier: GPL-3.0-only

package gateway

// nft_render.go — детерминированный рендер include-файла fw4 (OPENWRT-GATEWAY §3):
// таблица inet lavray, tproxy из PREROUTING только для трафика с интерфейсов LAN,
// исключения (адреса роутера, приватные сети, DHCP), перехват DNS, правила по
// MAC. Любой текст из конфигурации и политик проходит очистку: файл исполняется
// `nft -f -` от root. Перед выпуском golden-файлы из testdata/ проверяет
// `nft -c -f` (job CI nft-check).

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// NftRenderer — рендер include-файла fw4.
type NftRenderer struct{}

// Умолчания Config.
const (
	defaultFwMark   = 1
	defaultLANIface = "br-lan"
	defaultSelfUser = "lavray"
)

// Render возвращает полный nft-скрипт. selfUser — пользователь lavrayd для
// исключения его собственного трафика; пусто — cfg.SelfUser, затем "lavray".
func (NftRenderer) Render(cfg Config, policies []DevicePolicy, selfUser string) string {
	var b strings.Builder
	b.WriteString("#!/usr/sbin/nft -f\n")
	b.WriteString("# LAVRay gateway include (fw4): /usr/share/nftables.d/ruleset-post/lavray.nft\n")
	b.WriteString("# Сгенерировано lavrayd; правь политики в UCI, не здесь.\n")
	b.WriteString("table inet lavray\ndelete table inet lavray\n\n")
	b.WriteString("table inet lavray {\n")
	writeSets(&b, cfg)
	writeProxyChains(&b, cfg)
	writePrerouting(&b, cfg, policies)
	writeOutput(&b, firstNonEmpty(selfUser, cfg.SelfUser, defaultSelfUser))
	b.WriteString("}\n")
	return b.String()
}

func writeSets(b *strings.Builder, cfg Config) {
	b.WriteString("\tset exclude_addr {\n\t\ttype ipv4_addr\n\t\tflags interval\n")
	b.WriteString("\t\telements = { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, 224.0.0.0/4 }\n\t}\n")
	if cfg.IPv6 {
		b.WriteString("\tset exclude_addr6 {\n\t\ttype ipv6_addr\n\t\tflags interval\n")
		b.WriteString("\t\telements = { fc00::/7, fe80::/10, ::1, ff00::/8 }\n\t}\n")
	}
}

// writeProxyChains — цепочки, куда ведут все правила «через прокси». Разделение
// по семейству: tproxy в таблице inet требует явного ip или ip6.
func writeProxyChains(b *strings.Builder, cfg Config) {
	mark := cfg.FwMark
	if mark <= 0 {
		mark = defaultFwMark
	}
	for _, fam := range families(cfg) {
		fmt.Fprintf(b, "\tchain proxy_%s {\n", fam.chain)
		for _, proto := range []string{"tcp", "udp"} {
			fmt.Fprintf(b, "\t\tmeta l4proto %s tproxy %s to :%d meta mark set %d accept\n", proto, fam.stmt, cfg.TProxyPort, mark)
		}
		b.WriteString("\t}\n")
	}
}

// family — семейство адресов, для которого строятся правила.
type family struct{ chain, stmt, nfproto string }

func families(cfg Config) []family {
	out := []family{{chain: "v4", stmt: "ip", nfproto: "ipv4"}}
	if cfg.IPv6 {
		out = append(out, family{chain: "v6", stmt: "ip6", nfproto: "ipv6"})
	}
	return out
}

func writePrerouting(b *strings.Builder, cfg Config, policies []DevicePolicy) {
	b.WriteString("\tchain prerouting {\n")
	b.WriteString("\t\ttype filter hook prerouting priority mangle; policy accept;\n")
	fmt.Fprintf(b, "\t\tiifname != { %s } return\n", quotedList(lanIfaces(cfg)))
	if cfg.DNSHijack { // до исключений: запросы к dnsmasq роутера тоже идут в ядро
		for _, fam := range families(cfg) {
			fmt.Fprintf(b, "\t\tudp dport 53 meta nfproto %s jump proxy_%s\n", fam.nfproto, fam.chain)
			fmt.Fprintf(b, "\t\ttcp dport 53 meta nfproto %s jump proxy_%s\n", fam.nfproto, fam.chain)
		}
	}
	b.WriteString("\t\tfib daddr type local return\n")
	b.WriteString("\t\tip daddr @exclude_addr return\n")
	b.WriteString("\t\tudp dport { 67, 68 } return\n")
	if cfg.IPv6 {
		b.WriteString("\t\tip6 daddr @exclude_addr6 return\n\t\tudp dport { 546, 547 } return\n")
	}
	writeDeviceRules(b, cfg, policies)
	for _, fam := range families(cfg) {
		fmt.Fprintf(b, "\t\tmeta nfproto %s jump proxy_%s\n", fam.nfproto, fam.chain)
	}
	b.WriteString("\t}\n")
}

// writeDeviceRules — политики по MAC, отсортированные (детерминизм); записи с
// недопустимым MAC пропускаются: в файл попадает только нормализованный адрес.
func writeDeviceRules(b *strings.Builder, cfg Config, policies []DevicePolicy) {
	type rule struct{ mac, policy, preset string }
	rules := make([]rule, 0, len(policies))
	for _, p := range policies {
		if mac, err := NormalizeMAC(p.MAC); err == nil {
			rules = append(rules, rule{mac, p.Policy, safeToken(p.PresetID)})
		}
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].mac < rules[j].mac })
	for _, r := range rules {
		if r.policy == PolicyDirect {
			fmt.Fprintf(b, "\t\tether saddr %s return\n", r.mac)
			continue
		}
		if r.policy == PolicyPreset {
			fmt.Fprintf(b, "\t\t# %s: пресет %q, трафик идёт через прокси по активным правилам ядра\n", r.mac, r.preset)
		}
		for _, fam := range families(cfg) {
			fmt.Fprintf(b, "\t\tether saddr %s meta nfproto %s jump proxy_%s\n", r.mac, fam.nfproto, fam.chain)
		}
	}
}

// writeOutput — цепочка исходящего трафика самого роутера: он не заворачивается,
// трафик lavrayd исключается явно (защита от петли при будущих правилах).
func writeOutput(b *strings.Builder, user string) {
	b.WriteString("\tchain output {\n\t\ttype route hook output priority mangle; policy accept;\n")
	if _, err := strconv.ParseUint(user, 10, 32); err == nil {
		fmt.Fprintf(b, "\t\tmeta skuid %s return\n", user)
	} else {
		fmt.Fprintf(b, "\t\tmeta skuid %q return\n", safeToken(user))
	}
	b.WriteString("\t}\n")
}

func lanIfaces(cfg Config) []string {
	var out []string
	for _, n := range cfg.LANIfaces {
		if t := safeToken(n); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []string{defaultLANIface}
	}
	return out
}

func quotedList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = strconv.Quote(s)
	}
	return strings.Join(q, ", ")
}

// safeToken оставляет только [A-Za-z0-9._-] и не больше 32 символов: имена
// интерфейсов, пользователей и пресетов не должны выходить за кавычки правила.
func safeToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 && (r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9') || (r|0x20 >= 'a' && r|0x20 <= 'z')) {
			b.WriteRune(r)
		}
		if b.Len() >= 32 {
			break
		}
	}
	return b.String()
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if t := safeToken(s); t != "" {
			return t
		}
	}
	return defaultSelfUser
}
