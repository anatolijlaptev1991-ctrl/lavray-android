// SPDX-License-Identifier: GPL-3.0-only

package gateway

// service.go — шлюз как целое: правила nft, политики устройств и watchdog.
// Service не знает про proto и про инстанс sing-box: слой api сообщает ему,
// когда шлюзовый инстанс поднят (Activate) и упал/остановлен (Trip, Deactivate),
// и пробрасывает его статус в Beat. Политики применяются к уже установленным
// правилам с откатом: сначала nft, потом запись в настройки.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// defaultWatchdogMin — порог связи по умолчанию (§5), минут.
const defaultWatchdogMin = 5

// Status — состояние шлюза для GetGatewayStatus.
type Status struct {
	Active          bool   // правила nft установлены
	WatchdogTripped bool   // правила сняты watchdog'ом, ждём связи
	IPv6            bool   // ip6-правила включены явно
	LastError       string // последняя ошибка применения, одной строкой
}

// Service — шлюз роутера.
type Service struct {
	cfg  Config
	repo *PolicyRepo
	wd   *Watchdog
	now  func() time.Time

	mu       sync.Mutex   // сериализует Activate/Deactivate/SetPolicy
	enabled  bool         // шлюзовый инстанс работает
	polMu    sync.RWMutex // кэш политик; берётся внутри watchdog (Policies)
	policies []DevicePolicy
	errMu    sync.Mutex // последняя ошибка; берётся внутри watchdog (recordingApplier)
	lastErr  string
}

// NewService загружает политики и готовит watchdog. now == nil — time.Now.
func NewService(cfg Config, repo *PolicyRepo, applier Applier, now func() time.Time) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	if cfg.WatchdogMin <= 0 {
		cfg.WatchdogMin = defaultWatchdogMin
	}
	if cfg.SelfUser == "" {
		cfg.SelfUser = defaultSelfUser
	}
	list, err := repo.List()
	if err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, repo: repo, now: now, policies: list}
	s.wd = &Watchdog{
		Threshold: time.Duration(cfg.WatchdogMin) * time.Minute,
		Applier:   &recordingApplier{inner: applier, svc: s},
		Config:    cfg,
		Policies:  s.Policies,
	}
	return s, nil
}

// SetNotify задаёт обратный вызов смены состояния (true — правила сняты
// watchdog'ом, интернет у LAN идёт напрямую; false — правила вернулись).
func (s *Service) SetNotify(fn func(failover bool)) { s.wd.OnFailover = fn }

// Policies — копия текущего списка политик.
func (s *Service) Policies() []DevicePolicy {
	s.polMu.RLock()
	defer s.polMu.RUnlock()
	return append([]DevicePolicy(nil), s.policies...)
}

// Status — снимок состояния.
func (s *Service) Status() Status {
	s.errMu.Lock()
	last := s.lastErr
	s.errMu.Unlock()
	_, up := s.wd.State()
	return Status{Active: up, WatchdogTripped: s.wd.Tripped(), IPv6: s.cfg.IPv6, LastError: last}
}

// Enabled — шлюзовый инстанс запущен (правила нужны).
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// Activate устанавливает правила и вооружает watchdog. Ошибка применения
// сохраняется в статусе; watchdog остаётся вооружённым и повторит попытку на
// следующем Beat.
func (s *Service) Activate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
	return s.wd.StartArmed(s.now())
}

// Deactivate снимает правила и разоружает watchdog (отключение, выход).
func (s *Service) Deactivate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = false
	return s.wd.Disarm()
}

// Trip немедленно снимает правила, когда инстанс упал; Beat вернёт их.
func (s *Service) Trip() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.wd.Trip()
	return err
}

// ClearOrphans снимает правила, оставшиеся от прошлого запуска (при старте
// ядра; страж восстановления после краха, §5). Повтор безопасен.
func (s *Service) ClearOrphans() error {
	if err := s.wd.Applier.Remove(); err != nil {
		return fmt.Errorf("gateway: снятие осиротевших правил: %w", err)
	}
	return nil
}

// Beat сообщает watchdog'у, что связь есть, а Tick проверяет порог. Вызываются,
// пока шлюз включён; после Deactivate не делают ничего.
func (s *Service) Beat() {
	if s.Enabled() {
		s.wd.Beat(s.now())
	}
}

// Tick — проверка порога watchdog.
func (s *Service) Tick() {
	if s.Enabled() {
		s.wd.Tick(s.now())
	}
}

// setErr запоминает ошибку (nil — стирает).
func (s *Service) setErr(err error) {
	s.errMu.Lock()
	s.lastErr = oneLine(err)
	s.errMu.Unlock()
}

// recordingApplier запоминает результат последнего применения или снятия
// правил, в том числе вызванного самим watchdog'ом (Beat, Tick).
type recordingApplier struct {
	inner Applier
	svc   *Service
}

func (r *recordingApplier) Apply(script string) error {
	err := r.inner.Apply(script)
	r.svc.setErr(err)
	return err
}

func (r *recordingApplier) Remove() error {
	err := r.inner.Remove()
	r.svc.setErr(err)
	return err
}

// oneLine — ошибка одной строкой не длиннее 300 символов ("" — нет ошибки).
func oneLine(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return msg
}
