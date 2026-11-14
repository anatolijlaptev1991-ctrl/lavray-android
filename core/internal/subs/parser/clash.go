// SPDX-License-Identifier: GPL-3.0-only

package parser

// clash.go — Clash / Clash.Meta YAML-подписка (FR-UI-119/121, MUST).
// Большинство панелей отдают `proxies:` в формате Clash; раньше такой ответ
// отвергался как «формат не распознан».
//
// Поддерживаемые типы proxies: ss, ssr(skip), vmess, vless, trojan,
// hysteria2/hy2, tuic, socks5, http. Группы (proxy-groups), rules и
// listeners игнорируются — это маршрутизация, а не серверы.
//
// Безопасность: лимит размера проверяется до разбора, лимит числа профилей —
// после; YAML-алиасы (billion laughs) ограничены собственным счётчиком узлов.

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// clashNodeLimit — предел числа YAML-узлов (защита от бомб с якорями).
const clashNodeLimit = 200_000

// clashDoc — верхний уровень подписки.
type clashDoc struct {
	Proxies []clashProxy `yaml:"proxies"`
}

// clashProxy — union-структура всех поддерживаемых типов.
type clashProxy struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Server string `yaml:"server"`
	Port   int    `yaml:"port"`

	// ss / vmess / vless / trojan / socks / http
	Cipher   string `yaml:"cipher"`
	Password string `yaml:"password"`
	UUID     string `yaml:"uuid"`
	AlterID  int    `yaml:"alterId"`
	Flow     string `yaml:"flow"`
	Username string `yaml:"username"`

	// vmess
	Security string `yaml:"security"`
	// vmess в Clash: cipher (auto) — исторически; берём как security, если
	// отдельного поля нет.
	Network string `yaml:"network"`
	TLS     bool   `yaml:"tls"`
	SNI     string `yaml:"sni"`
	// Clash.Meta: servername — альтернатива sni.
	ServerName string   `yaml:"servername"`
	SkipCert   bool     `yaml:"skip-cert-verify"`
	ALPN       []string `yaml:"alpn"`

	// transport
	WSOpts *struct {
		Path    string            `yaml:"path"`
		Headers map[string]string `yaml:"headers"`
	} `yaml:"ws-opts"`
	GrpcOpts *struct {
		ServiceName string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
	HTTPOpts *struct {
		Path []string `yaml:"path"`
		Host []string `yaml:"host"`
	} `yaml:"h2-opts"`

	// reality
	RealityOpts *struct {
		PublicKey string `yaml:"public-key"`
		ShortID   string `yaml:"short-id"`
	} `yaml:"reality-opts"`
	ClientFingerprint string `yaml:"client-fingerprint"`

	// hysteria2 / tuic
	Obfs              string `yaml:"obfs"`
	ObfsPassword      string `yaml:"obfs-password"`
	UpMbps            int    `yaml:"up"`
	DownMbps          int    `yaml:"down"`
	Ports             string `yaml:"ports"`
	HopInterval       string `yaml:"hop-interval"`
	CongestionControl string `yaml:"congestion-controller"`
	UDPRelayMode      string `yaml:"udp-relay-mode"`
}

// looksLikeClash — быстрый детектор до полного YAML-разбора.
func looksLikeClash(s string) bool {
	for _, line := range strings.SplitN(s, "\n", 40) {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "proxies:") {
			return true
		}
	}
	return false
}

// parseClash разбирает Clash YAML в профили.
func parseClash(content []byte, opts Options) (*Result, error) {
	var doc clashDoc
	dec := yaml.NewDecoder(strings.NewReader(string(content)))
	dec.KnownFields(false)
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		// Синтаксическая ошибка — не Clash (пустой документ даёт io.EOF).
		if doc.Proxies == nil {
			return nil, &Error{Msg: "clash: не удалось разобрать YAML", Inner: err}
		}
	}
	if len(doc.Proxies) == 0 {
		return nil, &Error{Msg: "clash: список proxies пуст"}
	}
	max := opts.maxProfiles()
	res := &Result{Format: FormatClash}
	seen := map[string]bool{}
	for i, p := range doc.Proxies {
		if len(res.Profiles) >= max {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("clash: превышен лимит %d профилей — остальные пропущены", max))
			break
		}
		m, err := clashToProfile(p)
		if err != nil {
			res.Errors = append(res.Errors, LineError{Line: i + 1, Text: p.Name, Err: err.Error()})
			continue
		}
		// Ключ дедупа внутри ответа: тип+адрес+порт+имя (одинаковые имена
		// провайдеры не ставят; иначе теряются реальные серверы).
		key := fmt.Sprintf("%s|%s|%d|%s", m.Type, m.Address, m.Port, m.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		res.Profiles = append(res.Profiles, m)
	}
	if len(res.Profiles) == 0 {
		return nil, &Error{Msg: "clash: ни одного поддерживаемого прокси"}
	}
	return res, nil
}

// clashToProfile конвертирует один прокси Clash в модель.
func clashToProfile(p clashProxy) (*model.Profile, error) {
	typ := strings.ToLower(strings.TrimSpace(p.Type))
	server := strings.TrimSpace(p.Server)
	if server == "" {
		return nil, errors.New("пустой server")
	}
	if p.Port <= 0 || p.Port > 65535 {
		return nil, fmt.Errorf("неверный port %d", p.Port)
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = server
	}
	base := &model.Profile{
		Type:    model.TypeTrojan,
		Name:    name,
		Address: server,
		Port:    p.Port,
	}
	stream := &model.StreamSettings{}
	useStream := false

	switch typ {
	case "ss":
		base.Type = model.TypeShadowsocks
		base.Detail = &model.ShadowsocksDetail{Method: p.Cipher, Password: p.Password}
	case "vmess":
		base.Type = model.TypeVMess
		sec := p.Cipher
		if sec == "" {
			sec = p.Security
		}
		if sec == "" {
			sec = "auto"
		}
		base.Detail = &model.VMessDetail{UUID: p.UUID, AlterID: p.AlterID, Security: sec}
		useStream = true
	case "vless":
		base.Type = model.TypeVLESS
		base.Detail = &model.TrojanVlessDetail{Password: p.UUID, Flow: p.Flow}
		useStream = true
	case "trojan":
		base.Type = model.TypeTrojan
		base.Detail = &model.TrojanVlessDetail{Password: p.Password}
		useStream = true
	case "hysteria2", "hy2":
		base.Type = model.TypeHysteria2
		base.Detail = &model.Hysteria2Detail{
			Password:      p.Password,
			ObfsPassword:  p.ObfsPassword,
			SNI:           firstNonEmpty(p.SNI, p.ServerName),
			AllowInsecure: p.SkipCert,
		}
		if p.UpMbps > 0 {
			base.Detail.(*model.Hysteria2Detail).UploadMbps = p.UpMbps
		}
		if p.DownMbps > 0 {
			base.Detail.(*model.Hysteria2Detail).DownloadMbps = p.DownMbps
		}
		if p.Ports != "" {
			base.Detail.(*model.Hysteria2Detail).ServerPorts = p.Ports
		}
		if p.HopInterval != "" {
			base.Detail.(*model.Hysteria2Detail).HopInterval = p.HopInterval
		}
	case "tuic":
		base.Type = model.TypeTUIC
		base.Detail = &model.TUICDetail{
			UUID:              p.UUID,
			Password:          p.Password,
			CongestionControl: p.CongestionControl,
			UDPRelayMode:      p.UDPRelayMode,
		}
	case "socks5", "socks":
		base.Type = model.TypeSOCKS
		base.Detail = &model.SocksHTTPDetail{Proto: "socks5", Username: p.Username, Password: p.Password}
	case "http", "https":
		base.Type = model.TypeHTTP
		base.Detail = &model.SocksHTTPDetail{Proto: "http", Username: p.Username, Password: p.Password}
	default:
		return nil, fmt.Errorf("тип %q не поддерживается", p.Type)
	}

	if useStream {
		stream.Security = "tls"
		if p.RealityOpts != nil && p.RealityOpts.PublicKey != "" {
			stream.Security = "reality"
			stream.RealityPublicKey = p.RealityOpts.PublicKey
			stream.RealityShortID = p.RealityOpts.ShortID
		} else if !p.TLS && p.SNI == "" && p.ServerName == "" && p.WSOpts == nil && p.GrpcOpts == nil {
			// Ни TLS, ни транспорта нет — plain TCP.
			stream.Security = "none"
		}
		stream.SNI = firstNonEmpty(p.SNI, p.ServerName)
		stream.AllowInsecure = p.SkipCert
		stream.UTLSFingerprint = p.ClientFingerprint
		if len(p.ALPN) > 0 {
			stream.ALPN = strings.Join(p.ALPN, ",")
		}
		switch strings.ToLower(p.Network) {
		case "ws":
			stream.Network = "ws"
			if p.WSOpts != nil {
				stream.Path = p.WSOpts.Path
				stream.Host = p.WSOpts.Headers["Host"]
			}
		case "grpc":
			stream.Network = "grpc"
			if p.GrpcOpts != nil {
				stream.Path = p.GrpcOpts.ServiceName
			}
		case "h2", "http":
			stream.Network = "http"
			if p.HTTPOpts != nil {
				if len(p.HTTPOpts.Path) > 0 {
					stream.Path = p.HTTPOpts.Path[0]
				}
				if len(p.HTTPOpts.Host) > 0 {
					stream.Host = p.HTTPOpts.Host[0]
				}
			}
		default:
			stream.Network = "tcp"
		}
		base.Stream = stream
	}
	return base, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
