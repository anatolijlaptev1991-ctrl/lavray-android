// SPDX-License-Identifier: GPL-3.0-only

// updater_wiring.go — подключение подсистемы обновлений к ядру: конфигурация
// сборки (Deps.Updater), состояние сервера (загрузка, проверенный установщик,
// фаза) и сборка клиента internal/updates с хранилищем корня в настройках.
package api

import (
	"sync"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

// UpdaterConf — конфигурация обновлений сборки. Пустой RootJSON (или nil
// конфигурация) — «обновления не настроены»: CheckUpdates честно отвечает
// FAILED_PRECONDITION с причиной not_configured, а не «обновлений нет».
type UpdaterConf struct {
	RootJSON []byte // вшитый root.json (только публичные ключи)
	Product  string // "lavray-windows" | …
	// BootstrapMetadata — базовые URL источников до первого подписанного
	// sources.json (GitHub Pages, зеркало jsDelivr).
	BootstrapMetadata []string
	// BootstrapSources — готовые источники (тесты); идут после URL-источников.
	BootstrapSources []updates.Source
	AppVersion       string // "" → version.Version (флаг компоновщика)
	Platform         string // "" → по GOOS/GOARCH ("windows-x64")
}

// stagedUpdate — проверенный установщик в staging и замыкание повторной
// проверки подписи (применение перепроверяет файл с диска).
type stagedUpdate struct {
	updates.Staged
	verify updates.ArtifactVerifier
}

// updaterState — изменяемое состояние подсистемы на Server.
type updaterState struct {
	conf *UpdaterConf
	mu   sync.Mutex

	downloading bool
	phase       lavrayv1.ComponentState_Phase
	phaseErr    string
	staged      *stagedUpdate

	// Компоненты-данные (update_comp_state.go): наборы и гео, замок применения.
	presets, geo compSlot
	applying     bool
	hookApply    func() // только тесты: вызывается под замком применения
}

func newUpdaterState(c *UpdaterConf) updaterState {
	return updaterState{conf: c, phase: lavrayv1.ComponentState_PHASE_IDLE}
}

func (u *updaterState) appVersion() string {
	if u.conf != nil && u.conf.AppVersion != "" {
		return u.conf.AppVersion
	}
	return version.Version
}

// configured: в сборке есть корень доверия.
func (u *updaterState) configured() bool { return u.conf != nil && len(u.conf.RootJSON) > 0 }

func (u *updaterState) setPhase(p lavrayv1.ComponentState_Phase, errText string) {
	u.mu.Lock()
	u.phase, u.phaseErr = p, errText
	u.mu.Unlock()
}

// beginDownload занимает «слот» загрузки; false — загрузка уже идёт. Фаза
// DOWNLOADING выставляется только у запрошенных компонентов.
func (u *updaterState) beginDownload(t dlTargets) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.downloading {
		return false
	}
	u.downloading = true
	downloading := lavrayv1.ComponentState_PHASE_DOWNLOADING
	if t.app {
		u.phase, u.phaseErr = downloading, ""
	}
	if t.presets {
		u.presets.phase, u.presets.err = downloading, ""
	}
	if t.geo {
		u.geo.phase, u.geo.err = downloading, ""
	}
	return true
}

func (u *updaterState) endDownload() {
	u.mu.Lock()
	u.downloading = false
	u.mu.Unlock()
}

func (u *updaterState) setStaged(s *stagedUpdate) {
	u.mu.Lock()
	u.staged = s
	u.phase = lavrayv1.ComponentState_PHASE_DOWNLOADED
	u.mu.Unlock()
}

func (u *updaterState) stagedCopy() *stagedUpdate {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.staged
}

// updateClient собирает клиент на запрос: корень — вшитый либо принятый ранее
// (update.root_json), состояние — из настроек, источники — через туннель, затем напрямую.
func (s *Server) updateClient(channel lavrayv1.UpdateChannel) (*updates.Client, *updates.State, error) {
	if !s.upd.configured() {
		return nil, nil, updates.ErrNotConfigured
	}
	conf := s.upd.conf
	trust, err := updates.ResolveTrust(conf.RootJSON, s.rootStore())
	if err != nil {
		return nil, nil, err
	}
	st := s.loadUpdateState()
	hc := s.updateHTTPClient()
	srcs := make([]updates.Source, 0, len(conf.BootstrapMetadata)+len(conf.BootstrapSources))
	for _, base := range conf.BootstrapMetadata {
		srcs = append(srcs, &updates.HTTPSource{Base: base, Client: hc})
	}
	srcs = append(srcs, conf.BootstrapSources...)
	return &updates.Client{
		Trust: trust, RootStore: s.rootStore(), State: &st, Product: conf.Product,
		AppVersion: s.upd.appVersion(), Platform: conf.Platform, Channel: channelOf(channel),
		BootstrapSources: srcs, HTTP: hc,
		OnWarn: func(m string) { s.logs.Logf(logs.LevelWarn, "update", "%s", m) },
	}, &st, nil
}

func channelOf(c lavrayv1.UpdateChannel) string {
	if c == lavrayv1.UpdateChannel_UPDATE_CHANNEL_BETA {
		return updates.ChannelBeta
	}
	return updates.ChannelStable
}
