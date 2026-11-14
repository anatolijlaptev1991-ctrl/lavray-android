// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// mapProfile конвертирует ProxyEntity NekoBox в профиль LAVRay.
// Ошибка = профиль не переносится (неизвестный тип) → в отчёт, без падения.
// Custom-поля (c_cfg/c_out и core/cmd кастомного bean) переносятся только в
// CustomFieldsConfirmed=false-виде (LNB-25, §5.3).
func mapProfile(ent *nekoEntity) (*model.Profile, error) {
	b := &nekoBean{}
	if len(ent.Bean) > 0 {
		if err := json.Unmarshal(ent.Bean, b); err != nil {
			return nil, fmt.Errorf("invalid bean JSON: %w", err)
		}
	}
	addr := store.SanitizeAddress(b.Addr)
	if addr == "" {
		addr = "127.0.0.1" // дефолт AbstractBean::serverAddress (пустое в JSON не пишется)
	}
	p := &model.Profile{
		ID:                 ent.ID,
		GroupID:            ent.Gid,
		Name:               b.Name,
		Address:            addr,
		Port:               b.Port,
		CustomConfigJSON:   b.CCfg,
		CustomOutboundJSON: b.COut, // CustomFieldsConfirmed = false (LNB-25)
		LegacyBean:         string(ent.Bean),
	}
	if ent.Yc != nil && *ent.Yc != 0 { // yc 0 = не тестировали
		yc := *ent.Yc
		p.TestResult.LatencyMs = &yc
	}
	// A17: страна нужна и перенесённым профилям, иначе группировка «по странам»
	// работала бы только для новых.
	p.Geo = model.DetectGeo(p.Name, p.Address)
	p.TestResult.FullReport = ent.Report
	p.Traffic = model.TrafficCounters{DownlinkBytes: ent.Traffic.DL, UplinkBytes: ent.Traffic.UL}
	if err := fillDetail(p, ent.Type, b); err != nil {
		return nil, err
	}
	return p, nil
}

// fillDetail выбирает тип и детали профиля по типу NekoBox.
func fillDetail(p *model.Profile, typ string, b *nekoBean) error {
	switch typ {
	case "socks", "http", "shadowsocks", "vmess", "trojan", "vless":
		fillStreamed(p, typ, b)
	case "naive", "hysteria2", "tuic":
		fillQUIC(p, typ, b)
	case "custom":
		fillCustom(p, b)
	case "chain":
		p.Type = model.TypeChain
		p.Detail = &model.ChainDetail{ProfileIDs: b.List}
	default:
		return fmt.Errorf("unknown profile type %q", typ)
	}
	return nil
}

// fillStreamed — типы с транспортом (stream): socks, http, ss, vmess, trojan, vless.
func fillStreamed(p *model.Profile, typ string, b *nekoBean) {
	p.Stream = mapStream(b.Stream)
	switch typ {
	case "socks", "http":
		p.Type = model.TypeSOCKS
		proto := "socks5"
		if typ == "http" {
			p.Type = model.TypeHTTP
			proto = "http"
		} else if b.SocksType == 4 {
			proto = "socks4"
		}
		p.Detail = &model.SocksHTTPDetail{Proto: proto, Username: b.Username, Password: b.Password}
	case "shadowsocks":
		p.Type = model.TypeShadowsocks
		p.Detail = &model.ShadowsocksDetail{Method: b.Method, Password: b.Pass, Plugin: b.Plugin, UDPOverTCP: b.Uot}
	case "vmess":
		p.Type = model.TypeVMess
		p.Detail = &model.VMessDetail{UUID: b.VMessID, AlterID: b.Aid, Security: b.Sec}
	case "trojan":
		p.Type = model.TypeTrojan
		p.Detail = &model.TrojanVlessDetail{Password: b.Pass, Flow: b.Flow}
	case "vless":
		p.Type = model.TypeVLESS
		p.Detail = &model.TrojanVlessDetail{Password: b.Pass, Flow: b.Flow}
	}
}

// fillCustom — custom-профиль: внутренний outbound, полный конфиг или внешнее ядро.
func fillCustom(p *model.Profile, b *nekoBean) {
	d := &model.CustomDetail{
		Core:             b.Core,
		ExternalCoreName: b.Core, // внешнее ядро именуется значением core
		Command:          b.Command,
		ConfigSuffix:     b.ConfigSuffix,
		MappingPort:      b.MappingPort,
		SocksPort:        b.SocksPort,
	}
	switch b.Core {
	case "internal":
		p.Type = model.TypeCustomOutbound
		d.OutboundJSON = b.ConfigSimple
	case "internal-full":
		p.Type = model.TypeCustomConfig
		d.FullConfigJSON = b.ConfigSimple
	default:
		// core=external: за флагом enable_external_cores (решение №5);
		// при импорте сохраняем, применение — на стороне W6/API.
		// Core = литерал "external" (контракт модели/proto); имя ядра —
		// в ExternalCoreName (см. N2 ревью).
		p.Type = model.TypeCustomExternal
		d.Core = "external"
		d.ConfigTemplate = b.ConfigSimple
	}
	p.Detail = d
}

// mapStream конвертирует V2rayStreamSettings (nil → nil).
func mapStream(ns *nekoStream) *model.StreamSettings {
	if ns == nil {
		return nil
	}
	return &model.StreamSettings{
		Network:           ns.Network,
		HeaderType:        ns.HeaderType,
		Path:              ns.Path,
		Host:              ns.Host,
		WSEarlyDataHeader: ns.EdName,
		WSEarlyDataMax:    ns.EdLen,
		Security:          ns.Security,
		SNI:               ns.SNI,
		ALPN:              ns.ALPN,
		AllowInsecure:     ns.Insecure,
		Certificate:       ns.Certificate,
		UTLSFingerprint:   ns.UTLS,
		RealityPublicKey:  ns.Pbk,
		RealityShortID:    ns.Sid,
		RealitySPX:        ns.Spx,
		PacketEncoding:    ns.PacketEncoding,
		Multiplex:         ns.MuxS,
	}
}
