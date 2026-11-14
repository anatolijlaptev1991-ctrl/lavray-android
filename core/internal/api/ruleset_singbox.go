// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package api

import (
	"bytes"
	"net/netip"
	"regexp"
	"strings"

	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
)

// ruleSetsSupported — тестер маршрута читает гео-наборы (.srs) читателем самого
// sing-box, поэтому в сборке с раннером его ответы по rule_set точны.
const ruleSetsSupported = true

// srsSet — правила одного .srs; логические правила не вычисляются.
type srsSet []option.HeadlessRule

func loadRuleSet(raw []byte) (cfgbuild.RuleSetMatcher, error) {
	compat, err := srs.Read(bytes.NewReader(raw), false)
	if err != nil {
		return nil, err
	}
	return srsSet(compat.Options.Rules), nil
}

func (s srsSet) MatchDomain(host string) bool {
	for _, r := range s {
		if r.Type != C.RuleTypeDefault {
			continue
		}
		d := r.DefaultOptions
		if d.DomainMatcher != nil && d.DomainMatcher.Match(host) {
			return true
		}
		for _, k := range d.DomainKeyword {
			if strings.Contains(host, k) {
				return true
			}
		}
		for _, expr := range d.DomainRegex {
			if ok, _ := regexp.MatchString(expr, host); ok {
				return true
			}
		}
	}
	return false
}

func (s srsSet) MatchIP(ip netip.Addr) bool {
	for _, r := range s {
		if r.Type == C.RuleTypeDefault && r.DefaultOptions.IPSet != nil && r.DefaultOptions.IPSet.Contains(ip) {
			return true
		}
	}
	return false
}
