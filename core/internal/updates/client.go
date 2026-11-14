// SPDX-License-Identifier: GPL-3.0-only

// client.go — клиент обновлений поверх Trust (UPDATE-SYSTEM v2.0 §5). Цепочка
// проверки: refreshRoot (ротация до 404) → sources → timestamp (параллельно,
// побеждает наибольший) → manifest (пиннится timestamp) → Decide (по версии
// приложения). Serial манифеста защищает только от отката метаданных.
// Негативные сценарии §7 — sentinel-ошибки (errors.Is).
package updates

import (
	"context"
	"net/http"
	"time"
)

// Result — успешная проверка (нет обновления → Availability.Available=false;
// манифест nil — в канале ничего не опубликовано).
type Result struct {
	Trust        *Trust
	Timestamp    *Timestamp
	Manifest     *Manifest
	Sources      *Sources
	Channel      string
	Availability Availability
	AssetKey     string // ключ актива платформы
	// Metadata — источник, отдавший победивший timestamp; Mirrors — источники
	// артефактов (подписанный список, затем bootstrap).
	Metadata Source
	Mirrors  []ArtifactSource
}

// State — состояние клиента (сериализуется в настройки update.*).
type State struct {
	RootVersion      int64
	SerialStable     int64
	SerialBeta       int64
	TimestampVersion int64
	LastCheckedAt    int64 // мс; обновляется только успешной проверкой
	LastWorkingURL   string
	// ResetTimestamp/ResetSerials — после ротации сменился набор ключей роли
	// timestamp/manifest: хранилище записывает счётчики как есть, а не берёт
	// максимум со старыми (иначе утёкший ключ, подписавший MaxInt64, навсегда
	// запер бы обновления).
	ResetTimestamp bool
	ResetSerials   bool
}

// Client — TUF-lite клиент. Trust — вшитый или сохранённый корень сборки.
type Client struct {
	Trust     *Trust
	RootStore RootStore // куда сохранять принятый корень (nil — не сохранять)
	State     *State
	// Product — продукт этой сборки ("lavray-windows" | …): чужие манифесты отвергаются (§7).
	Product string
	// AppVersion — установленная версия: по ней решается «есть ли обновление».
	AppVersion string
	// Platform — ключ актива ("windows-x64"); пусто → по GOOS/GOARCH.
	Platform string
	// Channel — "stable"/"beta"; пусто = stable.
	Channel string
	// BootstrapSources — источники до первого подписанного sources.json.
	BootstrapSources []Source
	// HTTP — клиент для источников из подписанного sources.json (туннель, затем напрямую).
	HTTP *http.Client
	// Now — инъекция времени; PhaseTimeout/Grace — пределы параллельной фазы timestamp.
	Now          func() time.Time
	PhaseTimeout time.Duration
	Grace        time.Duration
	// OnWarn получает некритичные замечания (например, не удалось сохранить корень).
	OnWarn func(string)
}

// Check выполняет полный цикл проверки. Любая ошибка оставляет State нетронутым
// (last_checked_at меняется только успешной проверкой).
func (c *Client) Check(ctx context.Context) (*Result, error) {
	if c.Trust == nil {
		return nil, ErrNotConfigured
	}
	now := c.now()
	if err := c.refreshRoot(ctx, now); err != nil {
		return nil, err
	}
	srcs, signed := c.fetchSources(ctx, now)
	tsr, err := FetchTimestamp(ctx, c.Trust, srcs, TimestampPolicy{
		Saved: c.state().TimestampVersion, Now: now, Timeout: c.PhaseTimeout, Grace: c.Grace})
	if err != nil {
		return nil, err
	}
	channel := c.channel()
	mf, msrc, err := FetchManifest(ctx, c.Trust, preferred(srcs, tsr.Source), tsr.Timestamp, ManifestQuery{
		Channel: channel, Product: c.Product, SavedSerial: c.savedSerial(channel), Now: now})
	if err != nil {
		return nil, err
	}
	res := &Result{Trust: c.Trust, Timestamp: tsr.Timestamp, Manifest: mf, Sources: signed,
		Channel: channel, AssetKey: c.assetKey(), Metadata: tsr.Source,
		Mirrors: mirrors(signed, c.BootstrapSources, c.HTTP)}
	res.Availability = Decide(mf, c.AppVersion, res.AssetKey)
	c.record(res, msrc, now)
	return res, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) channel() string {
	if c.Channel == ChannelBeta {
		return ChannelBeta
	}
	return ChannelStable
}

func (c *Client) assetKey() string {
	if c.Platform != "" {
		return c.Platform
	}
	return CurrentAssetKey()
}

func (c *Client) state() *State {
	if c.State == nil {
		c.State = &State{}
	}
	return c.State
}

func (c *Client) savedSerial(channel string) int64 {
	if channel == ChannelBeta {
		return c.state().SerialBeta
	}
	return c.state().SerialStable
}

// record фиксирует успешную проверку. Serial и версия timestamp — максимум из
// сохранённого и принятого (деградировавшее зеркало не откатывает счётчик).
func (c *Client) record(res *Result, msrc Source, now time.Time) {
	st := c.state()
	st.LastCheckedAt = now.UnixMilli()
	st.RootVersion = c.Trust.Root().Version
	st.LastWorkingURL = sourceBase(res.Metadata)
	if res.Timestamp.Version > st.TimestampVersion {
		st.TimestampVersion = res.Timestamp.Version
	}
	if res.Manifest == nil {
		return
	}
	if res.Channel == ChannelBeta {
		st.SerialBeta = max(st.SerialBeta, res.Manifest.Serial)
	} else {
		st.SerialStable = max(st.SerialStable, res.Manifest.Serial)
	}
	if msrc != nil {
		st.LastWorkingURL = sourceBase(msrc)
	}
}

func (c *Client) warn(msg string) {
	if c.OnWarn != nil {
		c.OnWarn(msg)
	}
}

func sourceBase(s Source) string {
	if h, ok := s.(*HTTPSource); ok {
		return h.Base
	}
	return ""
}
