// SPDX-License-Identifier: GPL-3.0-only

// routing_settings_service.go — DnsService и SettingsService (W6). Наборы
// маршрутов (RoutingService) — routing_service.go и preset_service.go;
// SubscribeSettings — W7 (unimplemented.go).
package api

import (
	"context"
	"encoding/json"

	"sort"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// instStateIdle — единственное состояние, когда настройки применяются
// без перезапуска (FR-UI-062).
const instStateIdle = inst.StateIdle

// ---- DnsService --------------------------------------------------------------
// DNS-настройки живут в settings-хранилище с ключами dns.* (модель W1);
// SetDns применяет «живые» поля без перезапуска (FR-UI-148).

// domainStrategyStr/Of — мост между proto-энумом и строками sing-box.
func domainStrategyStr(s lavrayv1.DomainStrategy) string {
	switch s {
	case lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV4:
		return "prefer_ipv4"
	case lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV6:
		return "prefer_ipv6"
	case lavrayv1.DomainStrategy_DOMAIN_STRATEGY_IPV4_ONLY:
		return "ipv4_only"
	case lavrayv1.DomainStrategy_DOMAIN_STRATEGY_IPV6_ONLY:
		return "ipv6_only"
	}
	return ""
}

func domainStrategyOf(v string) (lavrayv1.DomainStrategy, bool) {
	switch v {
	case "prefer_ipv4":
		return lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV4, true
	case "prefer_ipv6":
		return lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV6, true
	case "ipv4_only":
		return lavrayv1.DomainStrategy_DOMAIN_STRATEGY_IPV4_ONLY, true
	case "ipv6_only":
		return lavrayv1.DomainStrategy_DOMAIN_STRATEGY_IPV6_ONLY, true
	}
	return lavrayv1.DomainStrategy_DOMAIN_STRATEGY_UNSPECIFIED, false
}

func (s *Server) GetDns(ctx context.Context, req *connect.Request[lavrayv1.GetDnsRequest]) (*connect.Response[lavrayv1.DnsSettings], error) {
	out := s.currentDNS()
	return connect.NewResponse(out), nil
}

// currentDNS — снимок действующего DNS: переопределение активного пресета
// поверх DNS самого пресета; choice — выбранная карточка ("auto" без выбора).
func (s *Server) currentDNS() *lavrayv1.DnsSettings {
	out := &lavrayv1.DnsSettings{Mode: lavrayv1.DnsSettings_MODE_SIMPLE, Choice: strp("auto")}
	dns := presets.DNSBlock{Mode: "simple", RemoteDNS: "https://dns.google/dns-query", DirectDNS: "local"}
	if p, err := s.activePreset(); err == nil && p != nil {
		dns = p.DNS
	}
	if dns.Mode == "advanced" {
		out.Mode = lavrayv1.DnsSettings_MODE_ADVANCED
	}
	if ov, ok := s.activeDNSChoice(); ok {
		out.Choice = strp(ov.Choice)
	}
	out.RemoteDns = strp(dns.RemoteDNS)
	out.DirectDns = strp(dns.DirectDNS)
	out.RoutingEnabled = dns.RoutingEnabled
	if dns.AdvancedJSON != "" {
		out.AdvancedJson = strp(dns.AdvancedJSON)
	}
	if st, ok := domainStrategyOf(dns.RemoteQueryStrategy); ok {
		out.RemoteQueryStrategy = &st
	}
	if v := s.settingStr("core.underlying_dns"); v != "" {
		out.UnderlyingDns = strp(v)
	}
	out.FakeDnsEnabled = boolp(s.settingBool("tun.fake_dns"))
	return out
}

// SetDns записывает выбор карточки для АКТИВНОГО пресета (T-31): «Авто»
// сбрасывает переопределение, остальные карточки задают серверы и маршрут
// запросов (туннель / напрямую); свой адрес проверяется.
func (s *Server) SetDns(ctx context.Context, req *connect.Request[lavrayv1.SetDnsRequest]) (*connect.Response[lavrayv1.SetDnsResponse], error) {
	m := req.Msg.Dns
	if m == nil {
		return nil, invalidArg("dns обязателен")
	}
	if m.Mode == lavrayv1.DnsSettings_MODE_ADVANCED && optStr(m.AdvancedJson) != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(*m.AdvancedJson), &obj); err != nil {
			return nil, invalidArg("advanced_json: невалидный JSON: %v", err)
		}
		if !dnsObjectHasDirect(obj) {
			return nil, invalidArg("advanced_json: нужен сервер с tag \"dns-direct\" " +
				"(на него ссылается route.default_domain_resolver, LNB-08)")
		}
	}
	ov, err := choiceFromRequest(m)
	if err != nil {
		return nil, err
	}
	if err := s.saveDNSChoice(ov); err != nil {
		return nil, mapErr(err)
	}
	needsRestart := s.inst.Status().State != instStateIdle
	return connect.NewResponse(&lavrayv1.SetDnsResponse{
		NeedsProfileRestart: needsRestart,
		Applied:             s.currentDNS(),
	}), nil
}

// dnsObjectHasDirect — advanced-DNS обязан содержать tag dns-direct.
func dnsObjectHasDirect(obj map[string]any) bool {
	servers, ok := obj["servers"].([]any)
	if !ok {
		return false
	}
	for _, srv := range servers {
		if m, ok := srv.(map[string]any); ok {
			if tag, _ := m["tag"].(string); tag == "dns-direct" {
				return true
			}
		}
	}
	return false
}

// ValidateDnsJson — FR-UI-149 «Format/Validate» в редакторе DNS-объекта.
func (s *Server) ValidateDnsJson(ctx context.Context, req *connect.Request[lavrayv1.ValidateDnsJsonRequest]) (*connect.Response[lavrayv1.ValidateDnsJsonResponse], error) {
	raw := strings.TrimSpace(req.Msg.Json)
	if raw == "" {
		return connect.NewResponse(&lavrayv1.ValidateDnsJsonResponse{
			Valid: false, Errors: []string{"json: пустой ввод"},
		}), nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return connect.NewResponse(&lavrayv1.ValidateDnsJsonResponse{
			Valid: false, Errors: []string{"json: " + err.Error()},
		}), nil
	}
	var errs []string
	if _, ok := obj["servers"]; !ok {
		errs = append(errs, "servers: обязательное поле")
	}
	if !dnsObjectHasDirect(obj) {
		errs = append(errs, "servers: нужен сервер с tag \"dns-direct\"")
	}
	resp := &lavrayv1.ValidateDnsJsonResponse{Valid: len(errs) == 0, Errors: errs}
	if b, err := json.MarshalIndent(obj, "", "  "); err == nil {
		resp.Formatted = strp(string(b))
	}
	return connect.NewResponse(resp), nil
}

// ---- SettingsService ----------------------------------------------------------

func settingTypeToProto(t model.SettingType) lavrayv1.SettingType {
	switch t {
	case model.SettingString:
		return lavrayv1.SettingType_SETTING_TYPE_STRING
	case model.SettingBool:
		return lavrayv1.SettingType_SETTING_TYPE_BOOL
	case model.SettingInt:
		return lavrayv1.SettingType_SETTING_TYPE_INT
	case model.SettingStringList:
		return lavrayv1.SettingType_SETTING_TYPE_STRING_LIST
	case model.SettingJSON:
		return lavrayv1.SettingType_SETTING_TYPE_JSON
	default:
		return lavrayv1.SettingType_SETTING_TYPE_STRING
	}
}

func (s *Server) DescribeSettings(ctx context.Context, req *connect.Request[lavrayv1.DescribeSettingsRequest]) (*connect.Response[lavrayv1.DescribeSettingsResponse], error) {
	out := &lavrayv1.DescribeSettingsResponse{}
	for _, d := range model.SettingDescriptors() {
		if d.Internal {
			continue // служебный ключ ядра (sub.hwid_id) UI не показываем
		}
		sd := &lavrayv1.SettingDescriptor{
			Key:                    d.Key,
			Type:                   settingTypeToProto(d.Type),
			GroupId:                d.GroupID,
			RequiresProfileRestart: d.RequiresProfileRestart,
			RequiresAppRestart:     d.RequiresAppRestart,
			ShellOwned:             d.ShellOwned,
			DefaultValueJson:       strpIf(d.DefaultValue),
			DeprecatedBy:           strpIf(d.DeprecatedBy),
		}
		out.Descriptors = append(out.Descriptors, sd)
	}
	return connect.NewResponse(out), nil
}

// GetSettings отдаёт сохранённые значения, а для ключей без записи — значение
// по умолчанию из дескриптора (T-34): свежая установка показывает порт 2080, а
// не пустое поле, которое раньше превращалось в 0.
func (s *Server) GetSettings(ctx context.Context, req *connect.Request[lavrayv1.GetSettingsRequest]) (*connect.Response[lavrayv1.GetSettingsResponse], error) {
	all, err := s.st.Settings()
	if err != nil {
		return nil, mapErr(err)
	}
	for _, d := range model.SettingDescriptors() {
		if _, stored := all[d.Key]; !stored && d.DefaultValue != "" {
			all[d.Key] = d.DefaultValue
		}
	}
	want := map[string]bool{}
	for _, k := range req.Msg.Keys {
		want[k] = true
	}
	out := &lavrayv1.GetSettingsResponse{Values: map[string]*lavrayv1.SettingValue{}}
	for k, raw := range all {
		if len(want) > 0 && !want[k] {
			continue
		}
		if d, known := descriptorMap[k]; known && d.Internal {
			continue // служебный ключ ядра не отдаётся UI даже по явному запросу
		}
		// A4 (аудит №4): тип значения — из дескриптора. Сырой JSON в
		// stringValue приводил к тому, что клиент видел "\"192.168.1.5\""
		// и молча сбрасывал int/bool в дефолты.
		out.Values[k] = typedSettingValue(k, raw)
	}
	return connect.NewResponse(out), nil
}

// SetSettings сначала проверяет ВСЕ значения (тип, диапазон, адрес — см.
// settings_validate.go) и только потом пишет: невалидный запрос не меняет ничего.
// Ответ сообщает, нужен ли перезапуск профиля или приложения.
func (s *Server) SetSettings(ctx context.Context, req *connect.Request[lavrayv1.SetSettingsRequest]) (*connect.Response[lavrayv1.SetSettingsResponse], error) {
	keys := make([]string, 0, len(req.Msg.Values))
	for k := range req.Msg.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var writes []settingWrite
	resp := &lavrayv1.SetSettingsResponse{}
	for _, k := range keys {
		v := req.Msg.Values[k]
		d, known := descriptorMap[k]
		if known && d.Internal {
			continue // служебный ключ ядра UI менять не может (как неизвестный: не в accepted)
		}
		if !known && !strings.HasPrefix(k, "dns.") {
			continue // неизвестный ключ не применяется и не попадает в accepted (LNB-30-класс: не молча — список accepted честный)
		}
		if known {
			if err := validateSetting(k, d, v); err != nil {
				return nil, err
			}
		}
		// F6 (аудит №7): значения хранятся СТРОГО как JSON — геттеры
		// (settingStr/Bool/Int) делают json.Unmarshal, поэтому "1"/"0" и
		// сырые строки молча превращались в дефолты (TUN/порты не применялись).
		var raw string
		switch kind := v.GetValue().(type) {
		case *lavrayv1.SettingValue_StringValue:
			b, err := json.Marshal(kind.StringValue)
			if err != nil {
				return nil, mapErr(err)
			}
			raw = string(b)
		case *lavrayv1.SettingValue_BoolValue:
			raw = strconv.FormatBool(kind.BoolValue)
		case *lavrayv1.SettingValue_IntValue:
			raw = strconv.FormatInt(kind.IntValue, 10)
		case *lavrayv1.SettingValue_DoubleValue:
			raw = strconv.FormatFloat(kind.DoubleValue, 'g', -1, 64)
		case *lavrayv1.SettingValue_JsonValue:
			raw = kind.JsonValue
		case *lavrayv1.SettingValue_StringListValue:
			b, err := json.Marshal(kind.StringListValue.GetItems())
			if err != nil {
				return nil, mapErr(err)
			}
			raw = string(b)
		default:
			continue
		}
		writes = append(writes, settingWrite{k, raw})
		resp.AcceptedKeys = append(resp.AcceptedKeys, k)
		if d.RequiresProfileRestart && s.inst.Status().State != instStateIdle {
			resp.NeedsProfileRestart = true
		}
		if d.RequiresAppRestart {
			resp.NeedsAppRestart = true
		}
	}
	if err := s.writeSettings(writes); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(resp), nil
}

// ---- SubscriptionService (минимум W6; Update-стримы — W7) ----------------------

func (s *Server) ListSubscriptions(ctx context.Context, req *connect.Request[lavrayv1.ListSubscriptionsRequest]) (*connect.Response[lavrayv1.ListSubscriptionsResponse], error) {
	groups, err := s.st.ListGroups()
	if err != nil {
		return nil, mapErr(err)
	}
	out := &lavrayv1.ListSubscriptionsResponse{}
	for _, g := range groups {
		if g.Kind == model.GroupSubscription {
			out.Groups = append(out.Groups, groupToProto(g))
		}
	}
	return connect.NewResponse(out), nil
}

func (s *Server) CreateSubscription(ctx context.Context, req *connect.Request[lavrayv1.CreateSubscriptionRequest]) (*connect.Response[lavrayv1.Group], error) {
	msg := req.Msg
	if strings.TrimSpace(msg.Url) == "" {
		return nil, invalidArg("url обязателен")
	}
	subURL, err := resolveSubscriptionURL(msg.Url) // T-19: happ://crypt… → ссылка подписки
	if err != nil {
		return nil, err
	}
	// T-51: локальный/приватный адрес — только с явным разрешением (LNB-05). Проверяем
	// ссылку, которую группа сохранит (после разбора happ://), а не сырой ввод.
	if err := validateNewSubscriptionURL(subURL); err != nil {
		return nil, invalidArg("%v", err)
	}
	g := &model.Group{
		Name:            msg.Name,
		Kind:            model.GroupSubscription,
		SubscriptionURL: subURL,
	}
	if g.Name == "" {
		g.Name = hostOf(subURL)
	}
	if err := s.st.CreateGroup(g); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(groupToProto(g)), nil
}

func hostOf(raw string) string {
	h := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if i := strings.IndexByte(h, '/'); i > 0 {
		h = h[:i]
	}
	return h
}

// typedSettingValue — JSON-хранилище → типизированный oneof по дескриптору.
func typedSettingValue(key, raw string) *lavrayv1.SettingValue {
	typ := lavrayv1.SettingType_SETTING_TYPE_STRING
	for _, d := range model.SettingDescriptors() {
		if d.Key == key {
			typ = settingTypeToProto(d.Type)
			break
		}
	}
	switch typ {
	case lavrayv1.SettingType_SETTING_TYPE_BOOL:
		var b bool
		if json.Unmarshal([]byte(raw), &b) == nil {
			return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_BoolValue{BoolValue: b}}
		}
	case lavrayv1.SettingType_SETTING_TYPE_INT:
		var n int64
		if json.Unmarshal([]byte(raw), &n) == nil {
			return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_IntValue{IntValue: n}}
		}
	case lavrayv1.SettingType_SETTING_TYPE_DOUBLE:
		var f float64
		if json.Unmarshal([]byte(raw), &f) == nil {
			return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_DoubleValue{DoubleValue: f}}
		}
	case lavrayv1.SettingType_SETTING_TYPE_STRING_LIST:
		var list []string
		if json.Unmarshal([]byte(raw), &list) == nil {
			return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_StringListValue{StringListValue: &lavrayv1.StringList{Items: list}}}
		}
	case lavrayv1.SettingType_SETTING_TYPE_JSON:
		return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_JsonValue{JsonValue: raw}}
	}
	var sv string
	if json.Unmarshal([]byte(raw), &sv) == nil {
		return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_StringValue{StringValue: sv}}
	}
	return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_StringValue{StringValue: raw}}
}
