// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

// trackmeta.go — trafficcontrol.TrackerMetadata форка → traffic.ConnInfo (T-79).

import (
	"unicode/utf8"

	"github.com/sagernet/sing-box/common/trafficcontrol"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// maxRuleRunes — предел длины текста правила: правило с длинным списком доменов
// не должно раздувать каждый кадр потока.
const maxRuleRunes = 200

// connInfoFromMeta переносит метаданные трекера в нейтральную модель.
//
// Метаданные активного соединения читаются без блокировки трекера: ID, время
// создания, цепочка, правило и контекст не меняются после создания, счётчики
// байт — атомарные. ClosedAt трекер пишет при закрытии, поэтому у активного
// соединения его не читаем (ClosedConnections отдаёт копии с готовым ClosedAt).
func connInfoFromMeta(m *trafficcontrol.TrackerMetadata, closed bool) traffic.ConnInfo {
	c := traffic.ConnInfo{
		ID:          m.ID.String(),
		StartMs:     m.CreatedAt.UnixMilli(),
		Outbound:    m.Outbound,
		Chain:       chainFromMeta(m.Chain),
		Destination: m.Metadata.Destination.String(),
		Inbound:     m.Metadata.Inbound,
		Network:     m.Metadata.Network,
		Rule:        ruleText(m),
	}
	if d := m.Metadata.Domain; d != "" && d != m.Metadata.Destination.AddrString() {
		c.Domain = d
	}
	if m.Upload != nil {
		c.Up = m.Upload.Load()
	}
	if m.Download != nil {
		c.Down = m.Download.Load()
	}
	if closed && !m.ClosedAt.IsZero() {
		c.EndMs = m.ClosedAt.UnixMilli()
	}
	if p := m.Metadata.ProcessInfo; p != nil {
		c.Process = traffic.ProcessName(p.ProcessPath)
	}
	return c
}

// chainFromMeta: форк хранит цепочку от конечного исходящего к выбранному
// правилом (формат clash-api); в модели порядок обратный — по ходу трафика.
func chainFromMeta(chain []string) []string {
	if len(chain) == 0 {
		return nil
	}
	out := make([]string, len(chain))
	for i, tag := range chain {
		out[len(chain)-1-i] = tag
	}
	return out
}

// ruleText — «правило => действие», как показывает clash-api; обрезается по длине.
func ruleText(m *trafficcontrol.TrackerMetadata) string {
	if m.Rule == nil {
		return ""
	}
	text := m.Rule.String()
	if a := m.Rule.Action(); a != nil {
		text += " => " + a.String()
	}
	if utf8.RuneCountInString(text) > maxRuleRunes {
		text = string([]rune(text)[:maxRuleRunes]) + "…"
	}
	return text
}
