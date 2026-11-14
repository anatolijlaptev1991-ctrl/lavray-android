// SPDX-License-Identifier: GPL-3.0-only

// Package subs — каркас обновления подписок LAVRay (W3): fetch с лимитами и
// заголовками, разбор (internal/subs/parser), дифф-флоу со снапшотом/откатом
// (FR-UI-258) и защитой от деструктивных обновлений (LNB-29).
//
// Сетевые параметры (план §4.1): раздельные таймауты connect 3 c / overall
// 30 c; 2 ретрая; лимит тела 64 МиБ (LNB-12); UA LAVRay/<v> или per-sub
// (FR-UI-262/263); HWID-заголовки ТОЛЬКО при явном opt-in (FR-UI-265);
// обновление через туннель по умолчанию задаёт вызывающая сторона (передавая
// http.Client с нужным Transport, LNB-33); «последний рабочий источник» —
// FR-UI-263 (успешный URL запоминается, при сбое основного пробуется первым).
//
// sub_insecure: при включении в лог пишется явный маркер (LNB-19).
package subs

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs/parser"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

// DefaultUserAgent — UA согласования формата (FR-UI-264). Версия — единая
// version.Version, а не собственный плейсхолдер пакета.
var DefaultUserAgent = "LAVRay/" + version.Version

// Параметры сети по умолчанию (план §4.1).
const (
	DefaultConnectTimeout = 3 * time.Second
	DefaultOverallTimeout = 30 * time.Second
	DefaultRetries        = 2
	// DefaultMaxBodyBytes — лимит тела ответа подписки (64 МиБ, LNB-12).
	DefaultMaxBodyBytes = parser.MaxContentBytes
)

// HWID — значения заголовков x-hwid/… (FR-UI-265, opt-in). ID — случайный
// на установку (CSPRNG) и ПОСТОЯННЫЙ: его создаёт NewHWID один раз, а хранит
// вызывающая сторона (api — настройка sub.hwid_id, D-02); os-поля заполняет
// вызывающая сторона или дефолты.
type HWID struct {
	ID          string
	DeviceOS    string
	VerOS       string
	DeviceModel string
}

// NewHWID генерирует набор HWID со случайным идентификатором установки.
// Вызывать нужно один раз и сохранить ID: новый ID на каждое обновление
// подписки провайдер считает новым устройством (D-02).
func NewHWID() HWID {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// деградация качества недопустима для идентификатора — паники нет,
		// fallback на временный уникальный
		return HWIDWithID(fmt.Sprintf("tmp-%d", time.Now().UnixNano()))
	}
	return HWIDWithID(hex.EncodeToString(b))
}

// HWIDWithID — набор HWID для уже сохранённого идентификатора установки.
func HWIDWithID(id string) HWID {
	return HWID{ID: id, DeviceOS: runtime.GOOS}
}

// Options — параметры подписчика (нулевые значения заменяются дефолтами).
type Options struct {
	UserAgent       string        // per-sub UA (FR-UI-263); "" → DefaultUserAgent
	ConnectTimeout  time.Duration // 0 → 3 c
	OverallTimeout  time.Duration // 0 → 30 c
	Retries         int           // <0 → 0; ретраи на сетевые ошибки
	MaxBodyBytes    int64         // 0 → 64 МиБ
	AllowPrivateURL bool          // «разрешить локальные адреса» (LNB-05)
	InsecureTLS     bool          // sub_insecure — с маркером в логе (LNB-19)
	HWIDEnabled     bool          // FR-UI-265: только явный opt-in
	ClearBefore     bool          // FR-UI-124 (sub_clear): очистить группу перед применением ответа
	HWID            HWID
	// Log — приёмник предупреждений (маркер insecure и пр.); nil → нет.
	Log func(format string, args ...any)
	// Resolver — резолвер валидатора URL (nil → системный; для тестов).
	Resolver interface {
		LookupIP(host string) ([]net.IP, error)
	}
}

func (o Options) withDefaults() Options {
	if o.UserAgent == "" {
		o.UserAgent = DefaultUserAgent
	}
	if o.ConnectTimeout <= 0 {
		o.ConnectTimeout = DefaultConnectTimeout
	}
	if o.OverallTimeout <= 0 {
		o.OverallTimeout = DefaultOverallTimeout
	}
	// SB1 (аудит round 2): ноль — это «не задано», а не «без ретраев».
	// Контракт §4.1: 2 ретрая с докачкой; отключение — явным отрицательным.
	if o.Retries == 0 {
		o.Retries = DefaultRetries
	}
	if o.Retries < 0 {
		o.Retries = 0
	}
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = DefaultMaxBodyBytes
	}
	// Запасной путь для прямых пользователей пакета: ID живёт столько же, сколько
	// этот Subscriber. Приложение сюда не попадает — api всегда передаёт
	// сохранённый sub.hwid_id.
	if o.HWIDEnabled && o.HWID.ID == "" {
		o.HWID = NewHWID()
	}
	return o
}

func (o Options) logf(format string, args ...any) {
	if o.Log != nil {
		o.Log(format, args...)
	}
}

// Subscriber — обновляльщик подписок. http.Client инъектится (тесты, туннель:
// sub_use_proxy — клиент с прокси-транспортом собирает вызывающая сторона).
type Subscriber struct {
	opts   Options
	client *http.Client
	guard  *URLGuard
}

// NewSubscriber собирает подписчика поверх готового http.Client
// (nil → собственный клиент с дефолтными таймаутами).
func NewSubscriber(client *http.Client, opts Options) *Subscriber {
	opts = opts.withDefaults()
	guard := &URLGuard{Resolver: opts.Resolver, AllowPrivate: opts.AllowPrivateURL}
	// Пиннинг IP при dial (W3-2, анти-DNS-rebinding TOCTOU): соединение
	// открывается только на адрес, заново резолвнутый и проверенный тем же
	// валидатором (см. dialPinned). network игнорируется — семейство
	// определяет проверенный IP.
	dial := func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialPinned(ctx, addr, guard, opts.ConnectTimeout)
	}
	// L3 аудита №1: клиент вызывающей стороны НИКОГДА не мутируется.
	// Раньше при проксированном/произвольном транспорте мы меняли
	// CheckRedirect и TLSClientConfig общего *http.Client — следующий
	// подписчик (в т.ч. без insecure) наследовал InsecureSkipVerify, то есть
	// MITM для всех подписок. Всегда работаем на своей копии.
	if client == nil {
		client = &http.Client{Transport: &http.Transport{DialContext: dial}}
	} else {
		shallow := *client
		client = &shallow
	}
	if tr, ok := client.Transport.(*http.Transport); ok && !transportProxied(tr) {
		// Транспорт без прокси: клонируем (не мутируем транспорт вызывающей
		// стороны) и включаем пиннинг.
		c := tr.Clone()
		c.DialContext = dial
		nc := *client
		nc.Transport = c
		client = &nc
	} else if client.Transport == nil {
		nc := *client
		nc.Transport = &http.Transport{DialContext: dial}
		client = &nc
	}
	// Транспорт с прокси (туннель sub_use_proxy, LNB-33) и произвольный
	// RoundTripper пиннингом не накрываются: их DialContext набирает адрес
	// ПРОКСИ, а не цели подписки (проверка приватности там ломала бы
	// localhost-туннель); цель по-прежнему проверяется валидатором до
	// запроса и на каждом редиректе.
	// Редиректы — своим контролем: каждая цель через валидатор, ≤5 (LNB-05).
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return fmt.Errorf("слишком много редиректов (>%d)", MaxRedirects)
		}
		return guard.Validate(req.URL.String())
	}
	if opts.InsecureTLS {
		// LNB-19: явный маркер игнорирования TLS в логе запроса.
		opts.logf("ПОДПИСКА: проверка сертификата TLS ОТКЛЮЧЕНА (sub_insecure) для %s", "<url скрыт>")
		if tr, ok := client.Transport.(*http.Transport); ok {
			tr = tr.Clone()
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			client.Transport = tr
		} else if client.Transport == nil {
			client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		}
	}
	return &Subscriber{opts: opts, client: client, guard: guard}
}

// transportProxied — использует ли транспорт прокси для запросов подписки
// (если да, DialContext набирает адрес прокси, и пиннинг неприменим).
func transportProxied(tr *http.Transport) bool {
	if tr.Proxy == nil {
		return false
	}
	u, err := url.Parse("http://subscription.invalid/")
	if err != nil {
		return true
	}
	p, err := tr.Proxy(&http.Request{URL: u})
	return err == nil && p != nil
}

// dialPinned — соединение с пиннингом IP (W3-2, SSRF DNS-rebinding TOCTOU):
// urlguard.Validate резолвит и проверяет хост ДО запроса, но http.Transport
// по умолчанию резолвит хост ПОВТОРНО при dial — за это время DNS мог
// «переехать» на приватный адрес. Здесь резолв выполняется заново (тем же
// резолвером, что у валидатора), КАЖДЫЙ адрес проходит проверку
// приватности, и соединение открывается ТОЛЬКО на проверенный IP
// (dial на ip:port — имя хоста в адрес соединения не попадает). Имя домена
// сохраняется в TLS SNI: transport строит TLS поверх возвращённого
// соединения сам, ServerName берётся из URL запроса, а не из адреса dial.
func dialPinned(ctx context.Context, addr string, guard *URLGuard, timeout time.Duration) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("подписка: dial %q: %v", addr, err)
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		ips, err = guard.lookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("подписка: dial: хост %s не резолвится: %v", host, err)
		}
	}
	checked := make([]string, 0, len(ips))
	for _, ip := range ips {
		if err := guard.CheckIP(ip); err != nil {
			return nil, fmt.Errorf("подписка: dial %s: %v", host, err)
		}
		checked = append(checked, net.JoinHostPort(ip.String(), port))
	}
	if len(checked) == 0 {
		return nil, fmt.Errorf("подписка: dial: хост %s не имеет адресов", host)
	}
	d := &net.Dialer{Timeout: timeout}
	var lastErr error
	for _, target := range checked {
		conn, err := d.DialContext(ctx, "tcp", target)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("подписка: dial %s: %v", host, lastErr)
}

// Response — успешный ответ подписки (тело + заголовки FR-UI-262).
type Response struct {
	Body            []byte
	Info            *model.SubscriptionInfo
	UsedURL         string
	Header          http.Header
	FromLastWorking bool
}

// Fetch скачивает содержимое подписки. Порядок источников (FR-UI-263):
// основной URL, при неудаче — последний рабочий (если был и отличается).
// Успешный URL запоминается вызывающим кодом в group.last_working_url.
func (s *Subscriber) Fetch(ctx context.Context, group *model.Group) (*Response, error) {
	if group == nil || group.SubscriptionURL == "" {
		return nil, errors.New("подписка: у группы нет URL подписки")
	}
	urls := []string{group.SubscriptionURL}
	if group.LastWorkingURL != "" && group.LastWorkingURL != group.SubscriptionURL {
		urls = append(urls, group.LastWorkingURL)
	}
	var firstErr error
	for i, u := range urls {
		resp, err := s.fetchOne(ctx, u, group)
		if err == nil {
			resp.FromLastWorking = i > 0
			return resp, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

// fetchOne — один URL с ретраями (2 попытки сверх первой на сетевые ошибки).
func (s *Subscriber) fetchOne(ctx context.Context, rawURL string, group *model.Group) (*Response, error) {
	if err := s.guard.Validate(rawURL); err != nil {
		return nil, err // схемы/приватные адреса — без ретраев
	}
	attempts := s.opts.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// бекоф перед повтором (LNB-19)
			select {
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		resp, retriable, err := s.doRequest(ctx, rawURL, group)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retriable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("подписка: %v (после %d попыток)", lastErr, attempts)
}

func (s *Subscriber) doRequest(ctx context.Context, rawURL string, group *model.Group) (*Response, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.OverallTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("запрос подписки: %w", err)
	}
	// UA: per-sub (FR-UI-263) > опция подписчика > LAVRay/<v> (FR-UI-264).
	ua := s.opts.UserAgent
	if group.UserAgent != "" {
		ua = group.UserAgent
	}
	req.Header.Set("User-Agent", ua)
	// per-sub заголовки (FR-UI-263; без секретов — Basic-пароль передаёт
	// вызывающая сторона через заголовок Authorization в HTTPHeaders;
	// в модели W1 для Basic есть только имя пользователя).
	for k, v := range group.HTTPHeaders {
		if strings.EqualFold(k, "User-Agent") {
			continue // UA уже задан
		}
		req.Header.Set(k, v)
	}
	// HWID — только при явном opt-in (FR-UI-265).
	if s.opts.HWIDEnabled {
		req.Header.Set("x-hwid", s.opts.HWID.ID)
		if s.opts.HWID.DeviceOS != "" {
			req.Header.Set("x-device-os", s.opts.HWID.DeviceOS)
		}
		if s.opts.HWID.VerOS != "" {
			req.Header.Set("x-ver-os", s.opts.HWID.VerOS)
		}
		if s.opts.HWID.DeviceModel != "" {
			req.Header.Set("x-device-model", s.opts.HWID.DeviceModel)
		}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, true, err // сетевые ошибки — ретраибельны
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, false, fmt.Errorf("подписка: HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, s.opts.MaxBodyBytes+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(body)) > s.opts.MaxBodyBytes {
		return nil, false, fmt.Errorf("подписка: тело больше лимита %d байт", s.opts.MaxBodyBytes) // LNB-12
	}

	info := s.parseHeaders(resp.Header)
	out := &Response{Body: body, UsedURL: resp.Request.URL.String(), Header: resp.Header, Info: info}
	return out, false, nil
}

// parseHeaders — HTTP-заголовки ответа → SubscriptionInfo (FR-UI-262).
func (s *Subscriber) parseHeaders(h http.Header) *model.SubscriptionInfo {
	info := &model.SubscriptionInfo{}
	any := false
	apply := func(key, value string) {
		if value == "" {
			return
		}
		if err := parser.ApplySubscriptionHeader(info, key, value); err != nil {
			s.opts.logf("подписка: заголовок %s: %v", key, err)
			return
		}
		any = true
	}
	apply("subscription-userinfo", h.Get("Subscription-Userinfo"))
	apply("profile-title", h.Get("Profile-Title"))
	apply("profile-update-interval", h.Get("Profile-Update-Interval"))
	apply("profile-web-page-url", h.Get("Profile-Web-Page-Url"))
	apply("support-url", h.Get("Support-Url"))
	apply("announce", h.Get("Announce"))
	apply("routing", h.Get("Routing"))
	if title := parser.TitleFromDisposition(h.Get("Content-Disposition")); title != "" && info.Title == "" {
		info.Title = title
		any = true
	}
	if !any {
		return nil
	}
	return info
}

// MergeInfo объединяет заголовки ответа и тела (тело не перетирает HTTP).
func MergeInfo(dst, src *model.SubscriptionInfo) {
	if src == nil {
		return
	}
	if src.UploadBytes != nil {
		dst.UploadBytes = src.UploadBytes
	}
	if src.DownloadBytes != nil {
		dst.DownloadBytes = src.DownloadBytes
	}
	if src.TotalBytes != nil {
		dst.TotalBytes = src.TotalBytes
	}
	if src.ExpireMs != nil {
		dst.ExpireMs = src.ExpireMs
	}
	if src.UpdateIntervalHours != nil {
		dst.UpdateIntervalHours = src.UpdateIntervalHours
	}
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.WebPageURL == "" {
		dst.WebPageURL = src.WebPageURL
	}
	if dst.SupportURL == "" {
		dst.SupportURL = src.SupportURL
	}
	if dst.Announce == "" {
		dst.Announce = src.Announce
	}
	if dst.Routing == "" {
		dst.Routing = src.Routing
	}
}

// Update — полный цикл обновления группы-подписки (W3, FR-UI-258):
// fetch (с «последним рабочим источником») → parse (промежуточная копия) →
// снапшот → дифф → Apply (пустой результат/удаление >50 % — подтверждение,
// LNB-29). HTML-заглушка/пустой ответ отвергаются парсером до применения.
func (s *Subscriber) Update(ctx context.Context, st StoreAPI, group *model.Group, confirmed bool) (*UpdateResult, error) {
	if group == nil {
		return nil, errors.New("подписка: группа не задана")
	}
	if group.Kind != model.GroupSubscription {
		return nil, errors.New("подписка: группа не является подпиской")
	}
	if group.Archive {
		return nil, errors.New("подписка: группа в архиве — обновление пропущено")
	}
	resp, err := s.Fetch(ctx, group)
	if err != nil {
		return nil, err
	}
	parsed, err := parser.Parse(resp.Body, parser.Options{})
	if err != nil {
		// Заглушка/пусто/не распознано — группа НЕ меняется (LNB-29).
		return nil, fmt.Errorf("подписка %d: разбор: %w", group.ID, err)
	}
	info := resp.Info
	if info == nil {
		info = parsed.Info
	} else if parsed.Info != nil {
		MergeInfo(info, parsed.Info) // HTTP-заголовки приоритетнее строк тела
	}

	snap, err := TakeSnapshot(st, group.ID)
	if err != nil {
		return nil, err
	}
	result, err := Apply(st, snap, ApplyParams{
		Next:      parsed.Profiles,
		Confirmed: confirmed,
		Clear:     s.opts.ClearBefore,
		UsedURL:   resp.UsedURL,
		Info:      info,
	})
	if err != nil {
		return nil, err
	}
	result.Info = info
	// Строки, которые не разобрались, не должны пропадать молча: сервер из
	// ответа провайдера просто не появлялся в списке, и причины никто не видел.
	// Текст строки в журнал не пишется (там секреты), только номер и причина.
	result.SkippedLines = len(parsed.Errors)
	for _, le := range parsed.Errors {
		s.opts.logf("подписка %d: строка %d не разобрана и пропущена: %s", group.ID, le.Line, le.Err)
	}
	for _, w := range parsed.Warnings {
		s.opts.logf("подписка %d: %s", group.ID, w)
	}
	return result, nil
}
