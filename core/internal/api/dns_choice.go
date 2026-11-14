// SPDX-License-Identifier: GPL-3.0-only

// dns_choice.go — выбор карточки на экране DNS (T-31). Переопределение
// хранится для активного пресета и действует только на него: пресет со своим
// раздельным DNS (lav-ru, lav-cn) не ломается выбором на другом пресете.
// «Авто» — переопределения нет, действует DNS пресета (docs/PRESET-LAV-RU.md §3a).
package api

import (
	"encoding/json"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// dnsCards — серверы карточек. remoteDirect: запросы к серверу идут напрямую,
// а не внутри туннеля («DoH напрямую»). Все карточки резолвят все имена одним
// сервером (раздельного DNS пресета нет); direct_dns — системный.
var dnsCards = map[string]struct {
	remote       string
	remoteDirect bool
}{
	"tunnel": {"https://dns.google/dns-query", false},
	"doh":    {"https://dns.google/dns-query", true},
	"dot":    {"tls://1.1.1.1", false},
	"local":  {"local", false},
}

// dnsChoice — переопределение DNS одного пресета (значение meta[metaDNSOverride][id]).
type dnsChoice struct {
	Choice       string `json:"choice"` // tunnel | doh | dot | local | custom | advanced
	Mode         string `json:"mode"`   // simple | advanced
	Remote       string `json:"remote,omitempty"`
	Direct       string `json:"direct,omitempty"`
	Advanced     string `json:"advanced,omitempty"`
	Strategy     string `json:"strategy,omitempty"`
	Routing      bool   `json:"routing,omitempty"`
	RemoteDirect bool   `json:"remote_direct,omitempty"`
}

func (s *Server) dnsOverrides() map[string]dnsChoice {
	out := map[string]dnsChoice{}
	if raw, ok, err := s.st.GetMeta(metaDNSOverride); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &out) // повреждённая запись = переопределений нет
	}
	return out
}

// activeDNSChoice — переопределение активного пресета.
func (s *Server) activeDNSChoice() (dnsChoice, bool) {
	ov, ok := s.dnsOverrides()[s.activePresetID()]
	return ov, ok
}

// saveDNSChoice записывает переопределение активного пресета (nil — «Авто»: сбросить).
func (s *Server) saveDNSChoice(ov *dnsChoice) error {
	all := s.dnsOverrides()
	if ov == nil {
		delete(all, s.activePresetID())
	} else {
		all[s.activePresetID()] = *ov
	}
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return s.st.SetMeta(metaDNSOverride, string(b))
}

// dropDNSChoice забывает переопределение DNS набора id (набор удалён): иначе
// новый набор, занявший тот же id, унаследовал бы чужой выбор.
func (s *Server) dropDNSChoice(id string) error {
	all := s.dnsOverrides()
	if _, ok := all[id]; !ok {
		return nil
	}
	delete(all, id)
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return s.st.SetMeta(metaDNSOverride, string(b))
}

// choiceFromRequest проверяет SetDnsRequest и строит переопределение
// (nil, nil — «Авто»). Запрос без choice: пустой remote_dns = «Авто»,
// непустой = «custom» (совместимость со старыми клиентами).
func choiceFromRequest(m *lavrayv1.DnsSettings) (*dnsChoice, error) {
	strategy := ""
	if m.RemoteQueryStrategy != nil {
		strategy = domainStrategyStr(*m.RemoteQueryStrategy)
	}
	if m.Mode == lavrayv1.DnsSettings_MODE_ADVANCED {
		if strings.TrimSpace(optStr(m.AdvancedJson)) == "" {
			return nil, invalidArg("advanced_json: пустой JSON")
		}
		return &dnsChoice{Choice: "advanced", Mode: "advanced", Advanced: *m.AdvancedJson, Strategy: strategy, Routing: m.RoutingEnabled}, nil
	}
	choice := m.GetChoice()
	if choice == "" {
		choice = "custom"
		if strings.TrimSpace(m.GetRemoteDns()) == "" {
			choice = "auto"
		}
	}
	switch choice {
	case "auto":
		return nil, nil
	case "tunnel", "doh", "dot", "local":
		c := dnsCards[choice]
		return &dnsChoice{Choice: choice, Mode: "simple", Remote: c.remote, Direct: "local", Strategy: strategy, RemoteDirect: c.remoteDirect}, nil
	case "custom":
		remote := strings.TrimSpace(m.GetRemoteDns())
		if err := cfgbuild.ValidateDNSAddress(remote); err != nil {
			return nil, invalidArg("remote_dns: %v", err)
		}
		direct := strings.TrimSpace(m.GetDirectDns())
		if direct == "" {
			direct = "local"
		} else if err := cfgbuild.ValidateDNSAddress(direct); err != nil {
			return nil, invalidArg("direct_dns: %v", err)
		}
		return &dnsChoice{Choice: "custom", Mode: "simple", Remote: remote, Direct: direct, Strategy: strategy, Routing: m.RoutingEnabled}, nil
	}
	return nil, invalidArg("choice: неизвестная карточка DNS %q", choice)
}

// applyDNSOverride — копия пресета с DNS из переопределения активного пресета.
// p == nil (пресет не найден) — берём нейтральный, чтобы выбор пользователя
// всё равно доехал до конфига.
func (s *Server) applyDNSOverride(p *presets.Preset) *presets.Preset {
	ov, ok := s.activeDNSChoice()
	if !ok {
		return p
	}
	if p == nil {
		p = cfgbuild.NeutralPreset()
	}
	cp := *p
	dns := p.DNS
	dns.Mode = ov.Mode
	if ov.Mode == "advanced" {
		dns.AdvancedJSON = ov.Advanced
	} else {
		dns.RemoteDNS, dns.DirectDNS = ov.Remote, ov.Direct
	}
	dns.RoutingEnabled = ov.Routing
	if ov.Strategy != "" {
		dns.RemoteQueryStrategy = ov.Strategy
	}
	cp.DNS = dns
	return &cp
}
