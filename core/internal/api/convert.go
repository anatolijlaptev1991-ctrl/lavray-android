// SPDX-License-Identifier: GPL-3.0-only

package api

// convert.go — конвертация контрактов lavray.v1 (internal/gen/lavrayv1) ↔
// каноническая модель (internal/model, формат presets). Канонический JSON
// profiles.data — источник истины; proto-типы существуют только на границе
// RPC. Ошибка конвертации detail (несоответствие типа) → INVALID_ARGUMENT.

import (
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ---- мелкие хелперы прото-optional ----

func strp(s string) *string { return &s }
func int64p(v int64) *int64 { return &v }
func int32p(v int32) *int32 { return &v }
func boolp(v bool) *bool    { return &v }
func optStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func optInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func optInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
func optBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
func optFloat64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// ---- ProfileType ----

var protoTypeToModel = map[lavrayv1.ProfileType]model.ProfileType{
	lavrayv1.ProfileType_PROFILE_TYPE_SOCKS:           model.TypeSOCKS,
	lavrayv1.ProfileType_PROFILE_TYPE_HTTP:            model.TypeHTTP,
	lavrayv1.ProfileType_PROFILE_TYPE_SHADOWSOCKS:     model.TypeShadowsocks,
	lavrayv1.ProfileType_PROFILE_TYPE_TROJAN:          model.TypeTrojan,
	lavrayv1.ProfileType_PROFILE_TYPE_VMESS:           model.TypeVMess,
	lavrayv1.ProfileType_PROFILE_TYPE_VLESS:           model.TypeVLESS,
	lavrayv1.ProfileType_PROFILE_TYPE_NAIVE:           model.TypeNaive,
	lavrayv1.ProfileType_PROFILE_TYPE_HYSTERIA2:       model.TypeHysteria2,
	lavrayv1.ProfileType_PROFILE_TYPE_TUIC:            model.TypeTUIC,
	lavrayv1.ProfileType_PROFILE_TYPE_CUSTOM_OUTBOUND: model.TypeCustomOutbound,
	lavrayv1.ProfileType_PROFILE_TYPE_CUSTOM_CONFIG:   model.TypeCustomConfig,
	lavrayv1.ProfileType_PROFILE_TYPE_CUSTOM_EXTERNAL: model.TypeCustomExternal,
	lavrayv1.ProfileType_PROFILE_TYPE_CHAIN:           model.TypeChain,
}

var modelTypeToProto = func() map[model.ProfileType]lavrayv1.ProfileType {
	m := map[model.ProfileType]lavrayv1.ProfileType{}
	for k, v := range protoTypeToModel {
		m[v] = k
	}
	return m
}()

func strpIf(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---- Profile ----

// profileToModel — контракт → модель. Проверяет соответствие detail↔type.
func profileToModel(p *lavrayv1.Profile) (*model.Profile, error) {
	if p == nil {
		return nil, invalidArg("profile: пустой профиль")
	}
	mt, ok := protoTypeToModel[p.GetType()]
	if !ok {
		return nil, invalidArg("profile: неизвестный тип %d", int32(p.GetType()))
	}
	m := &model.Profile{
		ID:       p.GetId(),
		GroupID:  p.GetGroupId(),
		Type:     mt,
		Name:     p.GetName(),
		Geo:      p.GetGeo(),
		Address:  strings.TrimSpace(p.GetAddress()), // FR-UI-083
		Port:     int(p.GetPort()),
		Favorite: p.GetFavorite(),
		Tags:     p.GetTags(),
		Notes:    optStr(p.Notes),
		// custom_fields_confirmed клиента не читается: подтверждение — хэш (T-08B).
		CustomOutboundJSON: optStr(p.CustomOutboundJson),
		CustomConfigJSON:   optStr(p.CustomConfigJson),
		Stream:             streamToModel(p.GetStream()),
	}
	if tr := p.GetTestResult(); tr != nil {
		m.TestResult = model.TestResult{
			FullReport: "",
			LastError:  optStr(tr.LastError),
		}
		if tr.LatencyMs != nil {
			m.TestResult.LatencyMs = tr.LatencyMs
		}
		if tr.TestedAtMs != nil {
			m.TestResult.TestedAtMs = tr.TestedAtMs
		}
	}
	if tc := p.GetTraffic(); tc != nil {
		m.Traffic = model.TrafficCounters{UplinkBytes: tc.UplinkBytes, DownlinkBytes: tc.DownlinkBytes}
	}
	if p.CreatedAtMs != nil {
		m.CreatedAtMs = *p.CreatedAtMs
	}
	if p.UpdatedAtMs != nil {
		m.UpdatedAtMs = *p.UpdatedAtMs
	}
	d, err := detailToModel(mt, p)
	if err != nil {
		return nil, err
	}
	m.Detail = d
	return m, nil
}

// detailToModel — oneof контракта → иерархия модели (ровно одна деталь;
// несоответствие типу профиля — INVALID_ARGUMENT).
func detailToModel(t model.ProfileType, p *lavrayv1.Profile) (model.Detail, error) {
	switch d := p.GetDetail().(type) {
	case *lavrayv1.Profile_SocksHttp:
		if t != model.TypeSOCKS && t != model.TypeHTTP {
			return nil, invalidArg("profile: деталь socks_http не соответствует типу %s", t)
		}
		var proto string
		switch d.SocksHttp.GetProto() {
		case lavrayv1.SocksHttpDetail_PROTO_SOCKS4:
			proto = "socks4"
		case lavrayv1.SocksHttpDetail_PROTO_SOCKS5:
			proto = "socks5"
		case lavrayv1.SocksHttpDetail_PROTO_HTTP:
			proto = "http"
		}
		return &model.SocksHTTPDetail{
			Proto:    proto,
			Username: optStr(d.SocksHttp.Username),
			Password: optStr(d.SocksHttp.Password),
		}, nil
	case *lavrayv1.Profile_Shadowsocks:
		if t != model.TypeShadowsocks {
			return nil, invalidArg("profile: деталь shadowsocks не соответствует типу %s", t)
		}
		uot := 0
		switch d.Shadowsocks.GetUdpOverTcp() {
		case lavrayv1.ShadowsocksDetail_UOT_V1:
			uot = 1
		case lavrayv1.ShadowsocksDetail_UOT_V2:
			uot = 2
		}
		return &model.ShadowsocksDetail{
			Method:     d.Shadowsocks.Method,
			Password:   optStr(d.Shadowsocks.Password),
			Plugin:     optStr(d.Shadowsocks.Plugin),
			UDPOverTCP: uot,
		}, nil
	case *lavrayv1.Profile_Vmess:
		if t != model.TypeVMess {
			return nil, invalidArg("profile: деталь vmess не соответствует типу %s", t)
		}
		return &model.VMessDetail{
			UUID:     d.Vmess.Uuid,
			AlterID:  int(d.Vmess.AlterId),
			Security: d.Vmess.Security,
		}, nil
	case *lavrayv1.Profile_TrojanVless:
		if t != model.TypeTrojan && t != model.TypeVLESS {
			return nil, invalidArg("profile: деталь trojan_vless не соответствует типу %s", t)
		}
		return &model.TrojanVlessDetail{
			Password: d.TrojanVless.Password,
			Flow:     optStr(d.TrojanVless.Flow),
		}, nil
	case *lavrayv1.Profile_Naive:
		if t != model.TypeNaive {
			return nil, invalidArg("profile: деталь naive не соответствует типу %s", t)
		}
		proto := ""
		switch d.Naive.GetProto() {
		case lavrayv1.NaiveDetail_PROTO_HTTPS:
			proto = "https"
		case lavrayv1.NaiveDetail_PROTO_HTTP:
			proto = "http"
		}
		return &model.NaiveDetail{
			Username:            optStr(d.Naive.Username),
			Password:            optStr(d.Naive.Password),
			Proto:               proto,
			ExtraHeaders:        optStr(d.Naive.ExtraHeaders),
			SNI:                 optStr(d.Naive.Sni),
			Certificate:         optStr(d.Naive.Certificate),
			InsecureConcurrency: int(optInt32(d.Naive.InsecureConcurrency)),
			DisableLogs:         optBool(d.Naive.DisableLogs),
		}, nil
	case *lavrayv1.Profile_Hysteria2:
		if t != model.TypeHysteria2 {
			return nil, invalidArg("profile: деталь hysteria2 не соответствует типу %s", t)
		}
		return &model.Hysteria2Detail{
			Password:      optStr(d.Hysteria2.Password),
			ObfsPassword:  optStr(d.Hysteria2.ObfsPassword),
			UploadMbps:    int(optInt32(d.Hysteria2.UploadMbps)),
			DownloadMbps:  int(optInt32(d.Hysteria2.DownloadMbps)),
			ServerPorts:   optStr(d.Hysteria2.ServerPorts),
			HopInterval:   optStr(d.Hysteria2.HopInterval),
			SNI:           optStr(d.Hysteria2.Sni),
			CA:            optStr(d.Hysteria2.Ca),
			AllowInsecure: optBool(d.Hysteria2.AllowInsecure),
			DisableSNI:    optBool(d.Hysteria2.DisableSni),
		}, nil
	case *lavrayv1.Profile_Tuic:
		if t != model.TypeTUIC {
			return nil, invalidArg("profile: деталь tuic не соответствует типу %s", t)
		}
		cc := ""
		switch d.Tuic.GetCongestionControl() {
		case lavrayv1.TuicDetail_CONGESTION_CONTROL_BBR:
			cc = "bbr"
		case lavrayv1.TuicDetail_CONGESTION_CONTROL_CUBIC:
			cc = "cubic"
		case lavrayv1.TuicDetail_CONGESTION_CONTROL_NEW_RENO:
			cc = "new_reno"
		}
		urm := ""
		switch d.Tuic.GetUdpRelayMode() {
		case lavrayv1.TuicDetail_UDP_RELAY_MODE_NATIVE:
			urm = "native"
		case lavrayv1.TuicDetail_UDP_RELAY_MODE_QUIC:
			urm = "quic"
		}
		return &model.TUICDetail{
			UUID:              d.Tuic.Uuid,
			Password:          optStr(d.Tuic.Password),
			CongestionControl: cc,
			UDPRelayMode:      urm,
			ZeroRttHandshake:  optBool(d.Tuic.ZeroRttHandshake),
			Heartbeat:         optStr(d.Tuic.Heartbeat),
			SNI:               optStr(d.Tuic.Sni),
			ALPN:              optStr(d.Tuic.Alpn),
			CA:                optStr(d.Tuic.Ca),
			AllowInsecure:     optBool(d.Tuic.AllowInsecure),
			DisableSNI:        optBool(d.Tuic.DisableSni),
			UDPOverStream:     optBool(d.Tuic.UdpOverStream),
		}, nil
	case *lavrayv1.Profile_Custom:
		if t != model.TypeCustomOutbound && t != model.TypeCustomConfig && t != model.TypeCustomExternal {
			return nil, invalidArg("profile: деталь custom не соответствует типу %s", t)
		}
		core := ""
		switch d.Custom.GetCore() {
		case lavrayv1.CustomDetail_CORE_INTERNAL:
			core = "internal"
		case lavrayv1.CustomDetail_CORE_INTERNAL_FULL:
			core = "internal-full"
		case lavrayv1.CustomDetail_CORE_EXTERNAL:
			core = "external"
		}
		var cmd []string
		if s := optStr(d.Custom.Command); s != "" {
			cmd = strings.Fields(s)
		}
		return &model.CustomDetail{
			Core:             core,
			ExternalCoreName: optStr(d.Custom.ExternalCoreName),
			Command:          cmd,
			ConfigSuffix:     optStr(d.Custom.ConfigSuffix),
			ConfigTemplate:   optStr(d.Custom.ConfigTemplate),
			MappingPort:      int(optInt32(d.Custom.MappingPort)),
			SocksPort:        int(optInt32(d.Custom.SocksPort)),
			OutboundJSON:     optStr(d.Custom.OutboundJson),
			FullConfigJSON:   optStr(d.Custom.FullConfigJson),
		}, nil
	case *lavrayv1.Profile_Chain:
		if t != model.TypeChain {
			return nil, invalidArg("profile: деталь chain не соответствует типу %s", t)
		}
		ids := make([]int64, 0, len(d.Chain.ProfileIds))
		ids = append(ids, d.Chain.ProfileIds...)
		return &model.ChainDetail{ProfileIDs: ids}, nil
	default:
		// деталь обязательна для всех типов (oneof контракта)
		return nil, invalidArg("profile: не задана типоспецифичная деталь (oneof detail) для типа %s", t)
	}
}

// profileToProto — модель → контракт.
func profileToProto(m *model.Profile) *lavrayv1.Profile {
	if m == nil {
		return nil
	}
	pt, ok := modelTypeToProto[m.Type]
	if !ok {
		pt = lavrayv1.ProfileType_PROFILE_TYPE_UNSPECIFIED
	}
	p := &lavrayv1.Profile{
		Id:                    m.ID,
		GroupId:               m.GroupID,
		Type:                  pt,
		Name:                  m.Name,
		Geo:                   m.Geo,
		Address:               m.Address,
		Port:                  int32(m.Port),
		Favorite:              m.Favorite,
		Tags:                  m.Tags,
		CustomFieldsConfirmed: m.CustomFieldsConfirmed,
		Stream:                streamToProto(m.Stream),
		UnsupportedReason:     cfgbuild.UnsupportedReason(m), // вычисляется, не хранится
	}
	if m.Notes != "" {
		p.Notes = strp(m.Notes)
	}
	if m.CreatedAtMs != 0 {
		p.CreatedAtMs = int64p(m.CreatedAtMs)
	}
	if m.UpdatedAtMs != 0 {
		p.UpdatedAtMs = int64p(m.UpdatedAtMs)
	}
	if m.CustomOutboundJSON != "" {
		p.CustomOutboundJson = strp(m.CustomOutboundJSON)
	}
	if m.CustomConfigJSON != "" {
		p.CustomConfigJson = strp(m.CustomConfigJSON)
	}
	tr := &lavrayv1.TestResult{}
	if m.TestResult.LatencyMs != nil {
		tr.LatencyMs = m.TestResult.LatencyMs
	}
	if m.TestResult.TestedAtMs != nil {
		tr.TestedAtMs = m.TestResult.TestedAtMs
	}
	if m.TestResult.LastError != "" {
		tr.LastError = strp(m.TestResult.LastError)
	}
	tr.FullReport = parseFullReport(m.TestResult.FullReport)
	p.TestResult = tr
	p.Traffic = &lavrayv1.TrafficCounters{
		UplinkBytes:   m.Traffic.UplinkBytes,
		DownlinkBytes: m.Traffic.DownlinkBytes,
	}
	detailToProto(m, p)
	return p
}

func detailToProto(m *model.Profile, p *lavrayv1.Profile) {
	switch d := m.Detail.(type) {
	case *model.SocksHTTPDetail:
		proto := lavrayv1.SocksHttpDetail_PROTO_UNSPECIFIED
		switch d.Proto {
		case "socks4":
			proto = lavrayv1.SocksHttpDetail_PROTO_SOCKS4
		case "socks5":
			proto = lavrayv1.SocksHttpDetail_PROTO_SOCKS5
		case "http":
			proto = lavrayv1.SocksHttpDetail_PROTO_HTTP
		}
		pd := &lavrayv1.SocksHttpDetail{Proto: proto}
		if d.Username != "" {
			pd.Username = strp(d.Username)
		}
		if d.Password != "" {
			pd.Password = strp(d.Password)
		}
		p.Detail = &lavrayv1.Profile_SocksHttp{SocksHttp: pd}
	case *model.ShadowsocksDetail:
		pd := &lavrayv1.ShadowsocksDetail{Method: d.Method}
		if d.Password != "" {
			pd.Password = strp(d.Password)
		}
		if d.Plugin != "" {
			pd.Plugin = strp(d.Plugin)
		}
		switch d.UDPOverTCP {
		case 1:
			pd.UdpOverTcp = lavrayv1.ShadowsocksDetail_UOT_V1
		case 2:
			pd.UdpOverTcp = lavrayv1.ShadowsocksDetail_UOT_V2
		}
		p.Detail = &lavrayv1.Profile_Shadowsocks{Shadowsocks: pd}
	case *model.VMessDetail:
		p.Detail = &lavrayv1.Profile_Vmess{Vmess: &lavrayv1.VmessDetail{
			Uuid: d.UUID, AlterId: int32(d.AlterID), Security: d.Security,
		}}
	case *model.TrojanVlessDetail:
		pd := &lavrayv1.TrojanVlessDetail{Password: d.Password}
		if d.Flow != "" {
			pd.Flow = strp(d.Flow)
		}
		p.Detail = &lavrayv1.Profile_TrojanVless{TrojanVless: pd}
	case *model.NaiveDetail:
		proto := lavrayv1.NaiveDetail_PROTO_UNSPECIFIED
		switch d.Proto {
		case "https":
			proto = lavrayv1.NaiveDetail_PROTO_HTTPS
		case "http":
			proto = lavrayv1.NaiveDetail_PROTO_HTTP
		}
		pd := &lavrayv1.NaiveDetail{Proto: proto}
		if d.Username != "" {
			pd.Username = strp(d.Username)
		}
		if d.Password != "" {
			pd.Password = strp(d.Password)
		}
		if d.ExtraHeaders != "" {
			pd.ExtraHeaders = strp(d.ExtraHeaders)
		}
		if d.SNI != "" {
			pd.Sni = strp(d.SNI)
		}
		if d.Certificate != "" {
			pd.Certificate = strp(d.Certificate)
		}
		if d.InsecureConcurrency != 0 {
			pd.InsecureConcurrency = int32p(int32(d.InsecureConcurrency))
		}
		if d.DisableLogs {
			pd.DisableLogs = boolp(true)
		}
		p.Detail = &lavrayv1.Profile_Naive{Naive: pd}
	case *model.Hysteria2Detail:
		pd := &lavrayv1.Hysteria2Detail{}
		if d.Password != "" {
			pd.Password = strp(d.Password)
		}
		if d.ObfsPassword != "" {
			pd.ObfsPassword = strp(d.ObfsPassword)
		}
		if d.UploadMbps != 0 {
			pd.UploadMbps = int32p(int32(d.UploadMbps))
		}
		if d.DownloadMbps != 0 {
			pd.DownloadMbps = int32p(int32(d.DownloadMbps))
		}
		if d.ServerPorts != "" {
			pd.ServerPorts = strp(d.ServerPorts)
		}
		if d.HopInterval != "" {
			pd.HopInterval = strp(d.HopInterval)
		}
		if d.SNI != "" {
			pd.Sni = strp(d.SNI)
		}
		if d.CA != "" {
			pd.Ca = strp(d.CA)
		}
		if d.AllowInsecure {
			pd.AllowInsecure = boolp(true)
		}
		if d.DisableSNI {
			pd.DisableSni = boolp(true)
		}
		p.Detail = &lavrayv1.Profile_Hysteria2{Hysteria2: pd}
	case *model.TUICDetail:
		pd := &lavrayv1.TuicDetail{Uuid: d.UUID}
		cc := lavrayv1.TuicDetail_CONGESTION_CONTROL_UNSPECIFIED
		switch d.CongestionControl {
		case "bbr":
			cc = lavrayv1.TuicDetail_CONGESTION_CONTROL_BBR
		case "cubic":
			cc = lavrayv1.TuicDetail_CONGESTION_CONTROL_CUBIC
		case "new_reno":
			cc = lavrayv1.TuicDetail_CONGESTION_CONTROL_NEW_RENO
		}
		pd.CongestionControl = cc
		urm := lavrayv1.TuicDetail_UDP_RELAY_MODE_UNSPECIFIED
		switch d.UDPRelayMode {
		case "native":
			urm = lavrayv1.TuicDetail_UDP_RELAY_MODE_NATIVE
		case "quic":
			urm = lavrayv1.TuicDetail_UDP_RELAY_MODE_QUIC
		}
		pd.UdpRelayMode = urm
		if d.Password != "" {
			pd.Password = strp(d.Password)
		}
		if d.ZeroRttHandshake {
			pd.ZeroRttHandshake = boolp(true)
		}
		if d.Heartbeat != "" {
			pd.Heartbeat = strp(d.Heartbeat)
		}
		if d.SNI != "" {
			pd.Sni = strp(d.SNI)
		}
		if d.ALPN != "" {
			pd.Alpn = strp(d.ALPN)
		}
		if d.CA != "" {
			pd.Ca = strp(d.CA)
		}
		if d.AllowInsecure {
			pd.AllowInsecure = boolp(true)
		}
		if d.DisableSNI {
			pd.DisableSni = boolp(true)
		}
		if d.UDPOverStream {
			pd.UdpOverStream = boolp(true)
		}
		p.Detail = &lavrayv1.Profile_Tuic{Tuic: pd}
	case *model.CustomDetail:
		core := lavrayv1.CustomDetail_CORE_UNSPECIFIED
		switch d.Core {
		case "internal":
			core = lavrayv1.CustomDetail_CORE_INTERNAL
		case "internal-full":
			core = lavrayv1.CustomDetail_CORE_INTERNAL_FULL
		case "external":
			core = lavrayv1.CustomDetail_CORE_EXTERNAL
		}
		pd := &lavrayv1.CustomDetail{Core: core}
		if d.ExternalCoreName != "" {
			pd.ExternalCoreName = strp(d.ExternalCoreName)
		}
		if len(d.Command) > 0 {
			pd.Command = strp(strings.Join(d.Command, " "))
		}
		if d.ConfigSuffix != "" {
			pd.ConfigSuffix = strp(d.ConfigSuffix)
		}
		if d.ConfigTemplate != "" {
			pd.ConfigTemplate = strp(d.ConfigTemplate)
		}
		if d.MappingPort != 0 {
			pd.MappingPort = int32p(int32(d.MappingPort))
		}
		if d.SocksPort != 0 {
			pd.SocksPort = int32p(int32(d.SocksPort))
		}
		if d.OutboundJSON != "" {
			pd.OutboundJson = strp(d.OutboundJSON)
		}
		if d.FullConfigJSON != "" {
			pd.FullConfigJson = strp(d.FullConfigJSON)
		}
		p.Detail = &lavrayv1.Profile_Custom{Custom: pd}
	case *model.ChainDetail:
		p.Detail = &lavrayv1.Profile_Chain{Chain: &lavrayv1.ChainDetail{ProfileIds: d.ProfileIDs}}
	}
}

// ---- Group / SubscriptionInfo ----

func groupToModel(g *lavrayv1.Group) *model.Group {
	if g == nil {
		return nil
	}
	kind := model.GroupBasic
	if g.GetKind() == lavrayv1.Group_KIND_SUBSCRIPTION {
		kind = model.GroupSubscription
	}
	m := &model.Group{
		ID:              g.GetId(),
		Name:            g.GetName(),
		Kind:            kind,
		SubscriptionURL: optStr(g.SubscriptionUrl),
		Archive:         g.GetArchive(),
		SkipAutoUpdate:  g.GetSkipAutoUpdate(),
		UserAgent:       optStr(g.UserAgent),
		HTTPHeaders:     g.GetHttpHeaders(),
		HTTPBasicUser:   optStr(g.HttpBasicUser),
		LastWorkingURL:  optStr(g.LastWorkingUrl),
		UIState:         optStr(g.UiStateJson),
	}
	if g.FrontProxyId != nil {
		v := *g.FrontProxyId
		m.FrontProxyID = &v
	}
	if g.LastUpdateMs != nil {
		m.LastUpdateMs = *g.LastUpdateMs
	}
	if g.UpdateIntervalMin != nil {
		v := *g.UpdateIntervalMin
		m.UpdateIntervalMin = &v
	}
	if si := g.GetSubscriptionInfo(); si != nil {
		info := &model.SubscriptionInfo{Title: optStr(si.Title)}
		if si.UploadBytes != nil {
			v := *si.UploadBytes
			info.UploadBytes = &v
		}
		if si.DownloadBytes != nil {
			v := *si.DownloadBytes
			info.DownloadBytes = &v
		}
		if si.TotalBytes != nil {
			v := *si.TotalBytes
			info.TotalBytes = &v
		}
		if si.ExpireMs != nil {
			v := *si.ExpireMs
			info.ExpireMs = &v
		}
		if si.UpdateIntervalHours != nil {
			v := *si.UpdateIntervalHours
			info.UpdateIntervalHours = &v
		}
		if si.WebPageUrl != nil {
			info.WebPageURL = optStr(si.WebPageUrl)
		}
		if si.SupportUrl != nil {
			info.SupportURL = optStr(si.SupportUrl)
		}
		if si.Announce != nil {
			info.Announce = optStr(si.Announce)
		}
		if si.Routing != nil {
			info.Routing = optStr(si.Routing)
		}
		m.SubscriptionInfo = info
	}
	return m
}

func groupToProto(m *model.Group) *lavrayv1.Group {
	if m == nil {
		return nil
	}
	g := &lavrayv1.Group{
		Id:           m.ID,
		Name:         m.Name,
		Kind:         lavrayv1.Group_KIND_BASIC,
		ProfileOrder: m.ProfileOrder,
		HttpHeaders:  m.HTTPHeaders,
	}
	if m.Kind == model.GroupSubscription {
		g.Kind = lavrayv1.Group_KIND_SUBSCRIPTION
	}
	if m.SubscriptionURL != "" {
		g.SubscriptionUrl = strp(m.SubscriptionURL)
	}
	if m.Archive {
		g.Archive = true
	}
	if m.SkipAutoUpdate {
		g.SkipAutoUpdate = true
	}
	if m.FrontProxyID != nil {
		g.FrontProxyId = int64p(*m.FrontProxyID)
	}
	if m.SubscriptionInfo != nil {
		g.SubscriptionInfo = subscriptionInfoToProto(m.SubscriptionInfo)
	}
	if m.LastUpdateMs != 0 {
		g.LastUpdateMs = int64p(m.LastUpdateMs)
	}
	if m.UpdateIntervalMin != nil {
		g.UpdateIntervalMin = int32p(*m.UpdateIntervalMin)
	}
	if m.UserAgent != "" {
		g.UserAgent = strp(m.UserAgent)
	}
	if m.HTTPBasicUser != "" {
		g.HttpBasicUser = strp(m.HTTPBasicUser)
	}
	if m.LastWorkingURL != "" {
		g.LastWorkingUrl = strp(m.LastWorkingURL)
	}
	if m.UIState != "" {
		g.UiStateJson = strp(m.UIState)
	}
	return g
}

// subscriptionInfoToProto — модель подписки → proto (используется и в
// Group, и в SubscriptionProgress.user_info, A15).
func subscriptionInfoToProto(m *model.SubscriptionInfo) *lavrayv1.SubscriptionInfo {
	if m == nil {
		return nil
	}
	si := &lavrayv1.SubscriptionInfo{Title: strpIf(m.Title)}
	if m.UploadBytes != nil {
		si.UploadBytes = int64p(*m.UploadBytes)
	}
	if m.DownloadBytes != nil {
		si.DownloadBytes = int64p(*m.DownloadBytes)
	}
	if m.TotalBytes != nil {
		si.TotalBytes = int64p(*m.TotalBytes)
	}
	if m.ExpireMs != nil {
		si.ExpireMs = int64p(*m.ExpireMs)
	}
	if m.UpdateIntervalHours != nil {
		si.UpdateIntervalHours = int32p(*m.UpdateIntervalHours)
	}
	if m.WebPageURL != "" {
		si.WebPageUrl = strp(m.WebPageURL)
	}
	if m.SupportURL != "" {
		si.SupportUrl = strp(m.SupportURL)
	}
	if m.Announce != "" {
		si.Announce = strp(m.Announce)
	}
	if m.Routing != "" {
		si.Routing = strp(m.Routing)
	}
	return si
}

// testResultToProto — модель результата теста → proto (LatencyResult).
func testResultToProto(m model.TestResult) *lavrayv1.TestResult {
	tr := &lavrayv1.TestResult{}
	if m.LatencyMs != nil {
		tr.LatencyMs = m.LatencyMs
	}
	if m.TestedAtMs != nil {
		tr.TestedAtMs = m.TestedAtMs
	}
	if m.LastError != "" {
		tr.LastError = strp(m.LastError)
	}
	tr.FullReport = parseFullReport(m.FullReport)
	return tr
}
