// SPDX-License-Identifier: GPL-3.0-only

package api

// agent.go — доступ ИИ-агентов (MCP): отдельный агентский токен и белый
// список процедур по режиму `agent.mode` (решение владельца 2026-10-29).
//
// Агентский токен оболочка передаёт ядру второй строкой stdin и пишет в
// agent-endpoint.json; основной токен ядра в файл не попадает. Токен даёт
// доступ только к процедурам из белого списка текущего режима:
//
//	off     — ничего (по умолчанию);
//	read    — состояние, список серверов и подписок (без адресов и секретов),
//	          трафик, публичный IP, хвост журнала;
//	control — плюс подключение, отключение, тест серверов, обновление подписок.
//
// Режим читается из настройки при каждом запросе, поэтому смена режима
// действует сразу, без перезапуска ядра. Запросы агента проверяются и после
// белого списка: подключение — только к сохранённому серверу в режиме по
// умолчанию, у теста и обновления подписки нельзя задать адрес и обходные
// флаги, поиск по списку серверов (оракул адресов) закрыт.

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// SettingAgentMode — настройка ядра с режимом доступа агентов.
const SettingAgentMode = "agent.mode"

// Режимы доступа агентов.
const (
	AgentModeOff     = "off"
	AgentModeRead    = "read"
	AgentModeControl = "control"
)

// MaxAgentLogLines — наибольший хвост журнала, который получает агент.
const MaxAgentLogLines = 200

// agentConfirmHeader — заголовок подтверждения деструктивных действий
// (UpdateSubscription); агенту подтверждать нельзя.
const agentConfirmHeader = "X-LAVRay-Confirm"

type agentCtxKey struct{}

// WithAgent помечает контекст запроса как пришедший с агентским токеном.
func WithAgent(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentCtxKey{}, true)
}

// IsAgent — запрос пришёл с агентским токеном.
func IsAgent(ctx context.Context) bool {
	v, _ := ctx.Value(agentCtxKey{}).(bool)
	return v
}

// SetAgentToken задаёт агентский токен этого запуска ядра. Пустой токен
// отключает агентский доступ целиком (fail-closed).
func (s *Server) SetAgentToken(token string) {
	s.agentMu.Lock()
	s.agentToken = token
	s.agentMu.Unlock()
}

// AuthorizeAgent сверяет токен из заголовка Authorization с агентским и, если
// он совпал, возвращает запрос с отметкой агента в контексте.
func (s *Server) AuthorizeAgent(r *http.Request, bearer string) (*http.Request, bool) {
	s.agentMu.RLock()
	token := s.agentToken
	s.agentMu.RUnlock()
	if token == "" || bearer == "" || subtle.ConstantTimeCompare([]byte(bearer), []byte(token)) != 1 {
		return r, false
	}
	return r.WithContext(WithAgent(r.Context())), true
}

// AgentMode — текущий режим доступа агентов; неизвестное значение = off.
func (s *Server) AgentMode() string {
	switch s.settingStr(SettingAgentMode) {
	case AgentModeRead:
		return AgentModeRead
	case AgentModeControl:
		return AgentModeControl
	}
	return AgentModeOff
}

// agentReadProcedures — процедуры режима read. Строятся из agentTable
// (agent_table.go). Экспорт ссылок и конфига, GetRuntimeConfig, настройки, DNS,
// правила, импорт, создание и удаление серверов и подписок в списки не входят
// ни в одном режиме.
var agentReadProcedures = agentProcedures(agentRead)

// agentControlProcedures — процедуры, которые режим control добавляет к read.
var agentControlProcedures = agentProcedures(agentControl)

// agentAllowed — можно ли агенту вызвать процедуру в режиме mode.
func agentAllowed(mode, procedure string) error {
	switch {
	case mode != AgentModeRead && mode != AgentModeControl:
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("доступ для агентов выключен: включите его в LAVRay → Настройки → Агенты (MCP)"))
	case agentReadProcedures[procedure]:
		return nil
	case agentControlProcedures[procedure]:
		if mode == AgentModeControl {
			return nil
		}
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("режим «только чтение»: управление включается в LAVRay → Настройки → Агенты (MCP)"))
	}
	return connect.NewError(connect.CodePermissionDenied,
		errors.New("эта операция недоступна агентам ни в одном режиме"))
}

// agentDenied — отказ по содержимому запроса.
func agentDenied(why string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New("запрос отклонён для агента: "+why))
}

// agentCheckRequest ограничивает параметры разрешённых процедур. Сообщение
// может быть исправлено на месте (хвост журнала сокращается до предела).
func agentCheckRequest(msg any) error {
	switch m := msg.(type) {
	case *lavrayv1.ConnectRequest:
		switch {
		case m.InlineProfile != nil:
			return agentDenied("подключение по произвольному профилю не разрешено, нужен сохранённый server_id")
		case m.ProfileId == nil:
			return agentDenied("не задан server_id")
		case m.GetValidateOnly():
			return agentDenied("validate_only не разрешён")
		case m.GetMode() != lavrayv1.ConnectMode_CONNECT_MODE_UNSPECIFIED:
			return agentDenied("режим подключения выбирает пользователь в приложении")
		}
	case *lavrayv1.TestLatencyRequest:
		switch {
		case m.Mode == lavrayv1.TestLatencyRequest_MODE_FULL_TEST:
			return agentDenied("полный тест не разрешён (только latency и url)")
		case m.Url != nil, m.FullSpeedUrl != nil:
			return agentDenied("адрес проверки задаётся в настройках приложения")
		case m.FullLatency != nil, m.FullUdpLatency != nil, m.FullSpeed != nil, m.FullInOutIp != nil:
			return agentDenied("параметры полного теста не разрешены")
		}
	case *lavrayv1.UpdateSubscriptionRequest:
		if m.UseProxy != nil || m.InsecureTls != nil || m.UserAgent != nil || m.ClearBefore != nil ||
			m.AllowPrivateUrls != nil || m.ConfirmDestructive != nil {
			return agentDenied("параметры обновления подписки задаёт приложение; подтверждение деструктивного результата — только в приложении")
		}
	case *lavrayv1.ListProfilesRequest:
		if m.Search != nil && strings.TrimSpace(*m.Search) != "" {
			return agentDenied("поиск по списку серверов не разрешён (он сравнивает адреса)")
		}
	case *lavrayv1.GetTailRequest:
		if m.Lines <= 0 || m.Lines > MaxAgentLogLines {
			m.Lines = MaxAgentLogLines
		}
	}
	return nil
}

// agentRedactResponse убирает из ответа всё, что нельзя показывать агенту:
// адреса, порты, учётные данные, ссылки подписок, заметки и тексты ошибок.
// Ответы строятся заново на каждый вызов, поэтому изменение на месте безопасно.
// Применяется и к кадрам стримов (agentStream.Send).
func agentRedactResponse(msg any) {
	switch m := msg.(type) {
	case *lavrayv1.ListProfilesResponse:
		for i, p := range m.Profiles {
			m.Profiles[i] = agentProfile(p)
		}
	case *lavrayv1.ListGroupsResponse:
		for i, g := range m.Groups {
			m.Groups[i] = agentGroup(g)
		}
	case *lavrayv1.ListSubscriptionsResponse:
		for i, g := range m.Groups {
			m.Groups[i] = agentGroup(g)
		}
	case *lavrayv1.CoreStatus:
		agentStatus(m)
	case *lavrayv1.ConnectResponse:
		agentStatus(m.Status)
		m.Warnings = nil // содержат адрес и порт входящего подключения
	case *lavrayv1.DisconnectResponse:
		m.FinalStats = nil
	case *lavrayv1.SubscriptionProgress:
		// В detail фазы REQUESTING лежит ссылка подписки; остальное — только текст ошибки.
		m.UserInfo = nil
		if m.Phase == lavrayv1.SubscriptionProgress_PHASE_FAILED || m.Phase == lavrayv1.SubscriptionProgress_PHASE_CONFIRM_REQUIRED {
			if d := m.GetDetail(); d != "" {
				t := agentSanitizeText(d)
				m.Detail = &t
			}
		} else {
			m.Detail = nil
		}
	case *lavrayv1.LatencyResult:
		m.Result = agentTestResult(m.Result)
	case *lavrayv1.GetTailResponse:
		// Строки журнала называют посещаемые сайты и адреса серверов: история
		// посещений не должна уходить агенту (и дальше — провайдеру модели).
		for _, l := range m.Lines {
			if l != nil {
				l.Message = agentSanitizeTextLimit(l.Message, maxAgentLogRunes)
			}
		}
	}
}

// agentStatus — статус без параметров входящего подключения и интерфейса TUN.
func agentStatus(st *lavrayv1.CoreStatus) {
	if st == nil {
		return
	}
	st.Inbound = nil
	st.Tun = nil
	st.RoutingPresetId = nil
	if e := st.GetLastError(); e != "" {
		t := agentSanitizeText(e)
		st.LastError = &t
	}
}

// reAddress — адреса в свободном тексте: IPv4, IPv6, имя хоста с точкой, с
// портом или без.
var reAddress = regexp.MustCompile(`(?i)(\[[0-9a-f:.]+\]|\b\d{1,3}(?:\.\d{1,3}){3}\b|\b[0-9a-f]{1,4}(?::[0-9a-f]{0,4}){3,7}\b|\b(?:[a-z0-9_-]+\.)+[a-z]{2,}\b)(?::\d{1,5})?`)

// reAgentURL — любая ссылка со схемой: агенту ссылка не нужна целиком.
var reAgentURL = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]+`)

// maxAgentTextRunes — самый длинный текст ошибки, который получает агент.
const maxAgentTextRunes = 240

// maxAgentLogRunes — самая длинная строка журнала, которую получает агент.
const maxAgentLogRunes = 400

// agentSanitizeText скрывает в тексте ошибки секреты (общая маскировка
// журнала) и адреса серверов, затем обрезает его.
func agentSanitizeText(s string) string {
	return agentSanitizeTextLimit(s, maxAgentTextRunes)
}

// agentSanitizeTextLimit — agentSanitizeText с заданным пределом длины.
func agentSanitizeTextLimit(s string, limit int) string {
	s = reAgentURL.ReplaceAllString(strings.TrimSpace(s), "[url]")
	s = logs.Redact(s)
	s = reAddress.ReplaceAllString(s, "[addr]")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit]) + "…"
	}
	return s
}

// agentSanitizeError чистит текст ошибки хендлера от адресов и секретов:
// ошибки сети и sing-box называют адрес сервера. Код ошибки сохраняется.
func agentSanitizeError(err error) error {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return connect.NewError(connect.CodeInternal, errors.New(agentSanitizeText(err.Error())))
	}
	return connect.NewError(ce.Code(), errors.New(agentSanitizeText(ce.Message())))
}

// agentTestResult — результат теста без текста ошибки и отчёта полного теста.
func agentTestResult(tr *lavrayv1.TestResult) *lavrayv1.TestResult {
	if tr == nil {
		return nil
	}
	out := &lavrayv1.TestResult{LatencyMs: tr.LatencyMs, TestedAtMs: tr.TestedAtMs}
	// Текст ошибки теста содержит адрес сервера; остаётся только класс.
	if e := tr.GetLastError(); e != "" {
		class := "failed"
		if strings.HasPrefix(e, testNAPrefix) {
			class = "n/a"
		}
		out.LastError = &class
	}
	return out
}

// agentProfile — сервер в виде, пригодном для агента.
func agentProfile(p *lavrayv1.Profile) *lavrayv1.Profile {
	if p == nil {
		return nil
	}
	out := &lavrayv1.Profile{
		Id:       p.Id,
		GroupId:  p.GroupId,
		Type:     p.Type,
		Name:     p.Name,
		Favorite: p.Favorite,
		Geo:      p.Geo,
	}
	out.TestResult = agentTestResult(p.TestResult)
	return out
}

// agentGroup — группа или подписка без ссылки, заголовков и учётных данных.
func agentGroup(g *lavrayv1.Group) *lavrayv1.Group {
	if g == nil {
		return nil
	}
	out := &lavrayv1.Group{
		Id:             g.Id,
		Name:           g.Name,
		Kind:           g.Kind,
		Archive:        g.Archive,
		SkipAutoUpdate: g.SkipAutoUpdate,
		ProfileOrder:   g.ProfileOrder,
		LastUpdateMs:   g.LastUpdateMs,
	}
	if info := g.SubscriptionInfo; info != nil {
		out.SubscriptionInfo = &lavrayv1.SubscriptionInfo{
			UploadBytes:   info.UploadBytes,
			DownloadBytes: info.DownloadBytes,
			TotalBytes:    info.TotalBytes,
			ExpireMs:      info.ExpireMs,
		}
	}
	return out
}

// agentBefore проверяет процедуру вызова агента до хендлера (белый список
// режима). Для обычного (не агентского) контекста ничего не делает. Отказы
// пишутся в журнал: пользователь видит, что агент пытался сделать лишнее.
func (i *interceptor) agentBefore(ctx context.Context, procedure string, header http.Header) error {
	if !IsAgent(ctx) {
		return nil
	}
	mode := AgentModeOff
	if i.mode != nil {
		mode = i.mode()
	}
	header.Del(agentConfirmHeader)
	if err := agentAllowed(mode, procedure); err != nil {
		i.agentDeny(procedure, mode, err)
		return err
	}
	if agentControlProcedures[procedure] {
		i.agentLog(logs.LevelInfo, "агент (MCP): %s", procedure)
	}
	return nil
}

// agentMessage проверяет параметры уже разрешённой процедуры.
func (i *interceptor) agentMessage(procedure string, msg any) error {
	err := agentCheckRequest(msg)
	if err != nil {
		mode := AgentModeOff
		if i.mode != nil {
			mode = i.mode()
		}
		i.agentDeny(procedure, mode, err)
	}
	return err
}

func (i *interceptor) agentDeny(procedure, mode string, err error) {
	text := err.Error()
	if ce := new(connect.Error); errors.As(err, &ce) {
		text = ce.Message()
	}
	i.agentLog(logs.LevelWarn, "агент (MCP): отказ %s в режиме %s: %s", procedure, mode, text)
}

func (i *interceptor) agentLog(level, format string, args ...any) {
	if i.logs != nil {
		i.logs.Logf(level, logs.SourceAPI, format, args...)
	}
}

// agentStream проверяет сообщение запроса стримингового вызова после чтения.
type agentStream struct {
	connect.StreamingHandlerConn
	check func(msg any) error
}

func (c agentStream) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
		return err
	}
	return c.check(msg)
}

// Send очищает кадр ответа перед отправкой агенту.
func (c agentStream) Send(msg any) error {
	agentRedactResponse(msg)
	return c.StreamingHandlerConn.Send(msg)
}
