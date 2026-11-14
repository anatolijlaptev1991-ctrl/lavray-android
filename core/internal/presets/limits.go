// SPDX-License-Identifier: GPL-3.0-only

package presets

// limits.go — лимиты пользовательских наборов (T-80) и единая проверка
// набора перед сохранением: Create, Update и Import вызывают PrepareUser.

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// MaxCustomPresets — сколько пользовательских наборов можно хранить.
	MaxCustomPresets = 100
	// MaxListEntries — записей в одном из шести списков.
	MaxListEntries = 10000
	// MaxProcessRules — процессных правил в наборе.
	MaxProcessRules = 200
	// MaxProcessNames — имён процессов в одном правиле.
	MaxProcessNames = 64
	// MaxNameRunes — длина имени набора в символах.
	MaxNameRunes = 64
	// MaxAllCustomBytes — размер всех пользовательских наборов вместе (JSON в meta).
	MaxAllCustomBytes = 4 << 20
	// MaxImportBytes — размер файла при импорте набора.
	MaxImportBytes = 1 << 20
	// MaxRevision — потолок ревизии набора: один подписанный огромный rev не
	// должен навсегда закрыть набор для всех последующих ревизий.
	MaxRevision = 1_000_000_000
)

var (
	// ErrLimit — превышен лимит количества или общего размера (RESOURCE_EXHAUSTED).
	ErrLimit = errors.New("превышен лимит пользовательских наборов")
	// ErrTooLarge — файл больше MaxImportBytes (RESOURCE_EXHAUSTED).
	ErrTooLarge = errors.New("файл набора больше 1 МиБ")
	// ErrFormat — файл не похож ни на набор LAVRay, ни на маршрут NekoRay (INVALID_ARGUMENT).
	ErrFormat = errors.New("формат файла набора не распознан")
)

// PrepareUser приводит пользовательский набор к хранимому виду и проверяет его:
// custom_rules → массив, пустые списки вместо null, пользовательские лимиты, затем
// Validate по схеме. Ошибки называют путь поля.
func (p *Preset) PrepareUser() error {
	rules, err := NormalizeCustomRules(p.Routing.CustomRules)
	if err != nil {
		return err
	}
	p.Routing.CustomRules = rules
	p.fillEmptyLists()
	// Лимиты — до разбора записей: гигантский список не обходится целиком.
	if err := p.checkUserLimits(); err != nil {
		return err
	}
	return p.Validate()
}

// fillEmptyLists заменяет nil на пустые срезы: в JSON получается [], не null.
func (p *Preset) fillEmptyLists() {
	r := &p.Routing
	for _, l := range []*[]string{&r.ProxyDomain, &r.DirectDomain, &r.BlockDomain, &r.ProxyIP, &r.DirectIP, &r.BlockIP} {
		if *l == nil {
			*l = []string{}
		}
	}
	if r.ProcessRules == nil {
		r.ProcessRules = []ProcessRule{}
	}
	if p.Name == nil {
		p.Name = Localized{}
	}
	if p.Description == nil {
		p.Description = Localized{}
	}
}

func (p *Preset) checkUserLimits() error {
	named := false
	for lang, name := range p.Name {
		if n := len([]rune(name)); n > MaxNameRunes {
			return fmt.Errorf("name.%s: %d символов, предел %d", lang, n, MaxNameRunes)
		}
		named = named || strings.TrimSpace(name) != ""
	}
	if !named {
		return errors.New("name: пустое имя")
	}
	r := &p.Routing
	for _, l := range []struct {
		path  string
		items []string
	}{
		{"routing.proxy_domain", r.ProxyDomain}, {"routing.direct_domain", r.DirectDomain},
		{"routing.block_domain", r.BlockDomain}, {"routing.proxy_ip", r.ProxyIP},
		{"routing.direct_ip", r.DirectIP}, {"routing.block_ip", r.BlockIP},
	} {
		if len(l.items) > MaxListEntries {
			return fmt.Errorf("%s: %d записей, предел %d", l.path, len(l.items), MaxListEntries)
		}
	}
	if len(r.ProcessRules) > MaxProcessRules {
		return fmt.Errorf("routing.process_rules: %d правил, предел %d", len(r.ProcessRules), MaxProcessRules)
	}
	for i, pr := range r.ProcessRules {
		if len(pr.ProcessNames) > MaxProcessNames {
			return fmt.Errorf("routing.process_rules[%d].process_names: %d имён, предел %d", i, len(pr.ProcessNames), MaxProcessNames)
		}
	}
	return nil
}
