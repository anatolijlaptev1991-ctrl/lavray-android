// SPDX-License-Identifier: GPL-3.0-only

package model

// GroupKind — вид группы (proto Group.Kind).
type GroupKind int

const (
	GroupBasic        GroupKind = 1
	GroupSubscription GroupKind = 2
)

// SubscriptionInfo — информация из ответа подписки (FR-UI-118/262).
// Нулевые/отсутствующие поля UI трактует как «Без лимита»/«Бессрочно».
type SubscriptionInfo struct {
	UploadBytes         *int64 `json:"upload_bytes,omitempty"`
	DownloadBytes       *int64 `json:"download_bytes,omitempty"`
	TotalBytes          *int64 `json:"total_bytes,omitempty"`
	ExpireMs            *int64 `json:"expire_ms,omitempty"`
	UpdateIntervalHours *int32 `json:"update_interval_hours,omitempty"`
	Title               string `json:"title,omitempty"`
	WebPageURL          string `json:"web_page_url,omitempty"`
	SupportURL          string `json:"support_url,omitempty"`
	Announce            string `json:"announce,omitempty"`
	Routing             string `json:"routing,omitempty"`
}

// Group — группа профилей (порт db/Group.hpp; FR-UI-102–113) + per-sub
// настройки (FR-UI-263).
type Group struct {
	ID   int64     `json:"id"`
	Name string    `json:"name"`
	Kind GroupKind `json:"kind"` // смена kind после создания запрещена (FR-UI-109)

	SubscriptionURL string `json:"subscription_url,omitempty"` // обязателен для подписки
	Archive         bool   `json:"archive,omitempty"`          // FR-UI-110
	SkipAutoUpdate  bool   `json:"skip_auto_update,omitempty"`
	FrontProxyID    *int64 `json:"front_proxy_id,omitempty"` // FR-UI-111

	// ProfileOrder — ВЫЧИСЛЯЕМОЕ поле: канонический порядок хранится в
	// profiles.sort_key (core-port-plan §5.2, Р2-29); отдаётся для
	// отрисовки, при записи игнорируется.
	ProfileOrder []int64 `json:"profile_order,omitempty"`

	SubscriptionInfo *SubscriptionInfo `json:"subscription_info,omitempty"`
	LastUpdateMs     int64             `json:"last_update_ms,omitempty"` // FR-UI-127; 0 = никогда

	// per-sub настройки (FR-UI-263, MUST).
	UpdateIntervalMin *int32            `json:"update_interval_min,omitempty"` // 0/nil = глобальный
	UserAgent         string            `json:"user_agent,omitempty"`
	HTTPHeaders       map[string]string `json:"http_headers,omitempty"`
	HTTPBasicUser     string            `json:"http_basic_user,omitempty"`
	LastWorkingURL    string            `json:"last_working_url,omitempty"`

	UIState string `json:"ui_state,omitempty"` // opaque (column_width и пр.)

	// Служебные поля хранилища (не из контракта).
	TabOrder    int64 `json:"tab_order,omitempty"`
	CreatedAtMs int64 `json:"created_at_ms,omitempty"`
	UpdatedAtMs int64 `json:"updated_at_ms,omitempty"`
}
