// SPDX-License-Identifier: GPL-3.0-only

package api

// connect_health.go — проверка связи после подключения (0.2.1). Старт sing-box
// не доказывает, что через сервер что-то проходит: сервер может рвать
// соединения (ALPN, неверный пароль, закрытый порт), а интерфейс при этом пишет
// «Подключено». После успешного Connect и RestartProfile ядро в фоне один раз
// запрашивает test.url через исходящее профиля (та же проба, что у URL-теста
// T-84, но через работающий инстанс, а не через mixed-порт). Неудача
// записывается в CoreStatus.health_error и журнал и уходит подписчикам событий;
// успех снимает предупреждение. Настройка connect.health_check (по умолчанию
// включена) отключает проверку.

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

const (
	// settingHealthCheck — включена ли проверка связи после подключения.
	settingHealthCheck = "connect.health_check"
	// defaultHealthURL — адрес проверки, если test.url пуст.
	defaultHealthURL = "https://www.gstatic.com/generate_204"
	// healthCheckTimeout — сколько ждём ответа test.url через исходящее.
	healthCheckTimeout = 8 * time.Second
	// healthRetryDelay — пауза перед повтором: сразу после старта sing-box
	// получает первое событие смены сетевого интерфейса и обрывает QUIC-клиентов
	// («network changed»); один повтор отличает такой сбой от настоящего.
	healthRetryDelay = time.Second
	// maxHealthReason — предел краткой причины, символов.
	maxHealthReason = 160
	// warnNoTraffic — машинный код предупреждения в потоке событий.
	warnNoTraffic = "no_traffic"
)

// connHealth — итог проверки связи текущего запуска.
type connHealth struct {
	mu      sync.Mutex
	gen     uint64             // номер запуска; результат устаревшей проверки отбрасывается
	cancel  context.CancelFunc // отмена бегущей проверки
	failure string             // краткая причина; "" — сбоя нет
	subs    map[chan struct{}]struct{}
}

// healthFailure — причина сбоя проверки связи ("" — сбоя нет).
func (s *Server) healthFailure() string {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	return s.health.failure
}

// subscribeHealth — уведомления об изменении итога проверки (SubscribeEvents).
func (s *Server) subscribeHealth() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.health.mu.Lock()
	if s.health.subs == nil {
		s.health.subs = map[chan struct{}]struct{}{}
	}
	s.health.subs[ch] = struct{}{}
	s.health.mu.Unlock()
	return ch, func() {
		s.health.mu.Lock()
		delete(s.health.subs, ch)
		s.health.mu.Unlock()
	}
}

// notifyHealth будит подписчиков (неблокирующе: достаточно одного сигнала).
func (s *Server) notifyHealth() {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	for ch := range s.health.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// resetHealth отменяет бегущую проверку и снимает предупреждение: вызывается
// при отключении, остановке ядра и перед новым запуском.
func (s *Server) resetHealth() {
	s.health.mu.Lock()
	s.health.gen++
	cancel := s.health.cancel
	s.health.cancel = nil
	changed := s.health.failure != ""
	s.health.failure = ""
	s.health.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if changed {
		s.notifyHealth()
	}
}

// startHealthCheck запускает проверку связи в фоне (один раз на запуск).
func (s *Server) startHealthCheck() {
	if !s.settingBool(settingHealthCheck) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.health.mu.Lock()
	s.health.gen++
	gen := s.health.gen
	prev := s.health.cancel
	s.health.cancel = cancel
	s.health.mu.Unlock()
	if prev != nil {
		prev()
	}
	go func() {
		defer cancel()
		s.runHealthCheck(ctx, gen)
	}()
}

// healthURL — адрес проверки: test.url, иначе значение по умолчанию.
func (s *Server) healthURL() string {
	if u := strings.TrimSpace(s.testURL()); u != "" {
		return u
	}
	return defaultHealthURL
}

// runHealthCheck делает запрос test.url через исходящее активного инстанса.
// Быстрый сбой (не таймаут) повторяется один раз после короткой паузы.
func (s *Server) runHealthCheck(ctx context.Context, gen uint64) {
	dialer := activeDialer{m: s.inst}
	target := s.healthURL()
	var (
		ms  int32
		err error
	)
	for attempt := 0; attempt < 2; attempt++ {
		ms, err = probeURL(ctx, dialer, target, s.healthTimeout)
		var timeout *probeTimeoutError
		if err == nil || ctx.Err() != nil || errors.As(err, &timeout) {
			break
		}
		if errors.Is(err, inst.ErrDialUnsupported) || errors.Is(err, inst.ErrNotRunning) {
			break
		}
		if attempt == 0 {
			select {
			case <-time.After(s.healthRetryDelay):
			case <-ctx.Done():
				return
			}
		}
	}
	if ctx.Err() != nil {
		return // отключились или запустили заново: результат не нужен
	}
	if errors.Is(err, inst.ErrDialUnsupported) || errors.Is(err, inst.ErrNotRunning) {
		return // проверять нечем (раннер без этой возможности) или инстанс уже остановлен
	}
	s.finishHealth(gen, ms, err)
}

// finishHealth записывает итог, если запуск не сменился, и сообщает о нём.
func (s *Server) finishHealth(gen uint64, ms int32, err error) {
	reason := ""
	if err != nil {
		reason = healthReason(err)
	}
	s.health.mu.Lock()
	if s.health.gen != gen {
		s.health.mu.Unlock()
		return
	}
	changed := s.health.failure != reason
	s.health.failure = reason
	s.health.mu.Unlock()
	if err != nil {
		s.logs.Logf(logs.LevelWarn, logs.SourceCore,
			"проверка связи после подключения не прошла: %s (адрес проверки — настройка test.url)", healthReasonText(reason))
	} else {
		s.logs.Logf(logs.LevelInfo, logs.SourceCore, "проверка связи после подключения прошла: %d мс", ms)
	}
	if changed {
		s.notifyHealth()
	}
}

// outboundPrefix — начало текста ошибки sing-box: «open connection to host:port
// using outbound/vmess[proxy]: …»; адреса и теги в причину не нужны.
var outboundPrefix = regexp.MustCompile(`^.*using outbound/\S+: `)

// dialPrefix — «dial tcp 203.0.113.5:443: …»: адрес сервера в предупреждение и
// журнал (его читает и агент) не попадает.
var dialPrefix = regexp.MustCompile(`^(?:dial|read|write) (?:tcp|udp)[46]? \S+(?: -> \S+)?: `)

// Коды типовых причин (CoreStatus.health_error): интерфейс переводит их на язык
// пользователя, остальные причины приходят текстом ошибки как есть.
const (
	healthRefused = "refused" // сервер отклонил соединение (порт закрыт)
	healthReset   = "reset"   // сервер оборвал соединение (EOF, сброс)
	healthNoHost  = "no_host" // имя сервера не нашлось
	healthTimeout = "timeout" // ответ не пришёл за отведённое время
)

// healthReason — краткая причина сбоя: код типового сбоя либо текст ошибки без
// адресов и служебных префиксов.
func healthReason(err error) string {
	var timeout *probeTimeoutError
	if errors.As(err, &timeout) {
		return healthTimeout
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	text := outboundPrefix.ReplaceAllString(err.Error(), "")
	text = dialPrefix.ReplaceAllString(strings.Join(strings.Fields(text), " "), "")
	low := strings.ToLower(text)
	switch {
	case text == "":
		return healthTimeout
	case strings.Contains(low, "actively refused"), strings.Contains(low, "connection refused"):
		return healthRefused
	case strings.Contains(low, "no such host"), strings.Contains(low, "nxdomain"):
		return healthNoHost
	case text == "EOF", strings.HasSuffix(text, ": EOF"), strings.Contains(low, "connection reset"),
		strings.Contains(low, "forcibly closed"), strings.Contains(low, "broken pipe"):
		return healthReset
	}
	if r := []rune(text); len(r) > maxHealthReason {
		text = string(r[:maxHealthReason]) + "…"
	}
	return text
}

// healthReasonText — причина словами для журнала и запасного текста события.
func healthReasonText(reason string) string {
	switch reason {
	case healthRefused:
		return "сервер отклонил соединение"
	case healthReset:
		return "сервер оборвал соединение"
	case healthNoHost:
		return "не удалось найти адрес сервера"
	case healthTimeout:
		return "сервер не ответил вовремя"
	}
	return reason
}

// healthWarningEvent — предупреждение для потока событий (nil — сбоя нет).
func (s *Server) healthWarningEvent() *lavrayv1.CoreEvent {
	reason := s.healthFailure()
	if reason == "" {
		return nil
	}
	return &lavrayv1.CoreEvent{
		TsMs: nowMs(),
		Kind: &lavrayv1.CoreEvent_WarningRaised{WarningRaised: &lavrayv1.CoreWarning{
			Code:    warnNoTraffic,
			Message: "Подключено, но через сервер ничего не проходит: " + healthReasonText(reason) + ". Выберите другой сервер или проверьте подписку.",
		}},
	}
}

// activeDialer — TestDialer поверх работающего инстанса: соединения идут через
// его исходящее "proxy". UDP не нужен проверке связи.
type activeDialer struct{ m *inst.Manager }

func (d activeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.m.DialProxy(ctx, network, address)
}

func (d activeDialer) ListenPacket(context.Context, string) (net.PacketConn, error) {
	return nil, inst.ErrDialUnsupported
}

func (d activeDialer) Close() error { return nil }

var _ inst.TestDialer = activeDialer{}
