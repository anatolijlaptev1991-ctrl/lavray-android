// SPDX-License-Identifier: GPL-3.0-only

// Package updates — TUF-lite клиент обновлений LAVRay (UPDATE-SYSTEM v2.0,
// core-port-plan §9 W8): четыре подписанных документа root/timestamp/
// manifest/sources, верификация ed25519 по ролям корня, монотонный serial
// по каналу, ротация root порогом СТАРЫХ ключей, отзыв ключа — удалением из
// keys (SECURITY.md §4).
//
// Доверие даёт подпись, а не хост (принцип 6): allowlist источников — только
// из подписанного sources.json; в коде URL-пиннинга нет, bootstrap-список
// передаётся конфигурацией сборки.
//
// Фаза 1 (решение владельца 2026-10-23): root содержит один ключ k1 с
// threshold 1; формат и верификация порога полностью поддерживают целевое
// состояние 2-из-3 (k1/k2/k3 threshold 2) — покрыто тестами ротации/отзыва.
package updates

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ============================================================================
// Конверт {"signed": …, "signatures":[{keyid,sig}…]} (§3)
// ============================================================================

// MetaSignature — одна подпись: keyid из root.json + подпись ed25519 над
// БАЙТАМИ объекта signed (канонизированный JSON передаётся как есть).
type MetaSignature struct {
	Keyid string `json:"keyid"`
	Sig   string `json:"sig"` // base64 (std) ed25519, 64 байта
}

// Envelope — конверт подписанного документа.
type Envelope struct {
	Signed     json.RawMessage `json:"signed"`
	Signatures []MetaSignature `json:"signatures"`
}

// ParseEnvelope — строгий разбор конверта: только поля signed/signatures,
// подписи непустые и декодируются в 64 байта ed25519.
func ParseEnvelope(raw []byte) (*Envelope, error) {
	var env Envelope
	if err := decodeStrict(raw, &env); err != nil {
		return nil, fmt.Errorf("конверт: %w", err)
	}
	if len(env.Signed) == 0 {
		return nil, fmt.Errorf("%w: конверт без поля signed", ErrMissingField)
	}
	if len(env.Signatures) == 0 {
		return nil, fmt.Errorf("%w: конверт без signatures", ErrMissingField)
	}
	for i, s := range env.Signatures {
		if s.Keyid == "" {
			return nil, fmt.Errorf("%w: signatures[%d].keyid пуст", ErrBadValue, i)
		}
		sig, err := base64.StdEncoding.DecodeString(s.Sig)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return nil, fmt.Errorf("%w: signatures[%d].sig не является ed25519-подписью (base64, %d байт)", ErrBadValue, i, ed25519.SignatureSize)
		}
	}
	return &env, nil
}

// ============================================================================
// root.json (§3.1)
// ============================================================================

// PublicKey — публичный ключ из root.json.
type PublicKey struct {
	Alg string `json:"alg"` // "ed25519"
	Pub string `json:"pub"` // base64 (std), 32 байта
}

// Role — роль и её порог подписей.
type Role struct {
	Keyids    []string `json:"keyids"`
	Threshold int      `json:"threshold"`
}

// Root — подписанная часть root.json.
type Root struct {
	Type    string               `json:"type"`
	Version int64                `json:"version"`
	Expires time.Time            `json:"expires"`
	Keys    map[string]PublicKey `json:"keys"`
	Roles   map[string]Role      `json:"roles"`
}

// Роли корня (§3.1): root/timestamp/manifest/sources.
const (
	RoleRoot      = "root"
	RoleTimestamp = "timestamp"
	RoleManifest  = "manifest"
	RoleSources   = "sources"
	// RoleArtifact — подпись установщиков minisign (ключ a1); без неё клиент
	// отказывает (fail-closed).
	RoleArtifact = "artifact"
)

// ParseRoot — строгий разбор подписанной части root.json + структурная
// валидация: ключи ed25519, роли полны, порог достижим, keyid существуют.
func ParseRoot(signed json.RawMessage) (*Root, error) {
	if err := requireKeys(signed, "type", "version", "expires", "keys", "roles"); err != nil {
		return nil, err
	}
	var r Root
	if err := decodeStrict(signed, &r); err != nil {
		return nil, fmt.Errorf("root: %w", err)
	}
	if r.Type != RoleRoot {
		return nil, fmt.Errorf("%w: root.json имеет type=%q", ErrWrongType, r.Type)
	}
	if r.Version < 1 {
		return nil, fmt.Errorf("%w: root.version=%d < 1", ErrBadValue, r.Version)
	}
	if r.Expires.IsZero() {
		return nil, fmt.Errorf("%w: root.expires отсутствует", ErrMissingField)
	}
	if len(r.Keys) == 0 {
		return nil, fmt.Errorf("%w: root.keys пуст", ErrBadValue)
	}
	if err := validateRootKeys(&r); err != nil {
		return nil, err
	}
	if err := validateRootRoles(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ============================================================================
// timestamp.json (§3.2)
// ============================================================================

// TimestampEntry — запись о манифесте канала в timestamp.json.
type TimestampEntry struct {
	Serial int64  `json:"serial"`
	Sha256 string `json:"sha256"` // hex
	Length int64  `json:"length"`
}

// Timestamp — подписанная часть timestamp.json.
type Timestamp struct {
	Type    string                    `json:"type"`
	Version int64                     `json:"version"`
	Expires time.Time                 `json:"expires"`
	Meta    map[string]TimestampEntry `json:"meta"` // "manifest-<channel>.json" → запись
}

// ParseTimestamp — строгий разбор подписанной части timestamp.json.
func ParseTimestamp(signed json.RawMessage) (*Timestamp, error) {
	if err := requireKeys(signed, "type", "version", "expires", "meta"); err != nil {
		return nil, err
	}
	var ts Timestamp
	if err := decodeStrict(signed, &ts); err != nil {
		return nil, fmt.Errorf("timestamp: %w", err)
	}
	if ts.Type != RoleTimestamp {
		return nil, fmt.Errorf("%w: timestamp.json имеет type=%q", ErrWrongType, ts.Type)
	}
	if ts.Version < 1 {
		return nil, fmt.Errorf("%w: timestamp.version=%d < 1", ErrBadValue, ts.Version)
	}
	if ts.Expires.IsZero() {
		return nil, fmt.Errorf("%w: timestamp.expires отсутствует", ErrMissingField)
	}
	if len(ts.Meta) == 0 {
		return nil, fmt.Errorf("%w: timestamp.meta пуст", ErrBadValue)
	}
	for name, e := range ts.Meta {
		if !strings.HasPrefix(name, "manifest-") || !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("%w: timestamp.meta: неожиданный ключ %q (ожидается manifest-<channel>.json)", ErrBadValue, name)
		}
		if e.Serial < 1 {
			return nil, fmt.Errorf("%w: timestamp.meta[%s].serial=%d < 1", ErrBadValue, name, e.Serial)
		}
		if err := checkHex64(e.Sha256); err != nil {
			return nil, fmt.Errorf("timestamp.meta[%s].sha256: %w", name, err)
		}
		if e.Length < 1 {
			return nil, fmt.Errorf("%w: timestamp.meta[%s].length=%d < 1", ErrBadValue, name, e.Length)
		}
	}
	return &ts, nil
}

// ============================================================================
// manifest.json (§3.3)
// ============================================================================

// Asset — платформенный артефакт приложения/ядра.
type Asset struct {
	Path    string `json:"path"`    // относительный путь на источнике
	Sha256  string `json:"sha256"`  // hex
	Length  int64  `json:"length"`  // байт
	Minisig string `json:"minisig"` // minisign; trusted comment product;platform;channel;version;serial
}

// AppSection — секция app манифеста.
type AppSection struct {
	Version        string           `json:"version"`
	MinUpgradeFrom string           `json:"minUpgradeFrom,omitempty"`
	Assets         map[string]Asset `json:"assets"` // "windows-x64" → артефакт
}

// CoreSection — секция core (§7: отдельная доставка, своя схема версий).
type CoreSection struct {
	Version string `json:"version"` // "1.14.1-lav.N"
	MinApp  string `json:"minApp"`  // минимальная версия приложения
}

// PresetEntry — подписанный пресет (§8: rev, не semver).
type PresetEntry struct {
	Rev    int64  `json:"rev"`
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

// GeoSection — подписанный набор гео-данных (LNB-31).
type GeoSection struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Length int64  `json:"length"`
	// Rev — монотонная ревизия набора: клиент не применяет набор, чья ревизия не
	// выше установленной (стабильный канал после беты не откатывает гео).
	Rev         int64  `json:"rev"`
	GeneratedAt string `json:"generatedAt,omitempty"`
}

// Manifest — подписанная часть manifest.json. product/channel/serial/
// expires/mandatory — ВНУТРИ подписи (§2: downgrade/mix-and-match/подмена
// канала/DoS через mandatory).
type Manifest struct {
	Type      string                 `json:"type"`
	Product   string                 `json:"product"` // "lavray-windows" | …
	Channel   string                 `json:"channel"` // "stable" | "beta"
	Serial    int64                  `json:"serial"`  // монотонный по каналу
	Expires   time.Time              `json:"expires"`
	App       *AppSection            `json:"app,omitempty"`
	Core      *CoreSection           `json:"core,omitempty"`
	Presets   map[string]PresetEntry `json:"presets,omitempty"`
	Geo       *GeoSection            `json:"geo,omitempty"`
	Notes     map[string]string      `json:"notes,omitempty"`
	Mandatory *bool                  `json:"mandatory"`
}

// Каналы (§1 принцип 5).
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// ManifestName — имя файла манифеста канала в timestamp.meta и на источнике.
func ManifestName(channel string) string { return "manifest-" + channel + ".json" }

// ParseManifest — строгий разбор подписанной части manifest.json.
func ParseManifest(signed json.RawMessage) (*Manifest, error) {
	if err := requireKeys(signed, "type", "product", "channel", "serial", "expires", "mandatory"); err != nil {
		return nil, err
	}
	var m Manifest
	if err := decodeStrict(signed, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.Type != RoleManifest {
		return nil, fmt.Errorf("%w: manifest.json имеет type=%q", ErrWrongType, m.Type)
	}
	if m.Product == "" {
		return nil, fmt.Errorf("%w: manifest.product пуст", ErrBadValue)
	}
	if m.Channel != ChannelStable && m.Channel != ChannelBeta {
		return nil, fmt.Errorf("%w: manifest.channel=%q (ожидается stable|beta)", ErrBadValue, m.Channel)
	}
	if m.Serial < 1 {
		return nil, fmt.Errorf("%w: manifest.serial=%d < 1", ErrBadValue, m.Serial)
	}
	if m.Expires.IsZero() {
		return nil, fmt.Errorf("%w: manifest.expires отсутствует", ErrMissingField)
	}
	if m.Mandatory == nil {
		return nil, fmt.Errorf("%w: manifest.mandatory отсутствует", ErrMissingField)
	}
	if m.App != nil {
		if err := requireSectionKeys(signed, "app", "version", "assets"); err != nil {
			return nil, err
		}
		if m.App.Version == "" {
			return nil, fmt.Errorf("%w: app.version пуст", ErrBadValue)
		}
		if len(m.App.Assets) == 0 {
			return nil, fmt.Errorf("%w: app.assets пуст", ErrBadValue)
		}
		for plat, a := range m.App.Assets {
			if err := validateAsset("app.assets["+plat+"]", a); err != nil {
				return nil, err
			}
		}
	}
	if m.Core != nil {
		if err := requireSectionKeys(signed, "core", "version", "minApp"); err != nil {
			return nil, err
		}
		if m.Core.Version == "" || m.Core.MinApp == "" {
			return nil, fmt.Errorf("%w: core.version/minApp пусты", ErrBadValue)
		}
	}
	for name, p := range m.Presets {
		if p.Rev < 1 || p.Rev > MaxPresetRev {
			return nil, fmt.Errorf("%w: presets[%s].rev=%d вне 1…%d", ErrBadValue, name, p.Rev, MaxPresetRev)
		}
		if err := checkRelativePath(p.Path); err != nil {
			return nil, fmt.Errorf("presets[%s].path: %w", name, err)
		}
		if err := checkHex64(p.Sha256); err != nil {
			return nil, fmt.Errorf("presets[%s].sha256: %w", name, err)
		}
	}
	if m.Geo != nil {
		if err := m.Geo.validate(signed); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

func validateAsset(where string, a Asset) error {
	// path/sha256/length не бывают легитимно пустыми/нулевыми — их явное
	// присутствие в JSON проверяется непустыми значениями ниже.
	if a.Path == "" {
		return fmt.Errorf("%w: %s.path пуст", ErrMissingField, where)
	}
	if a.Sha256 == "" {
		return fmt.Errorf("%w: %s.sha256 пуст", ErrMissingField, where)
	}
	if err := checkRelativePath(a.Path); err != nil {
		return fmt.Errorf("%s.path: %w", where, err)
	}
	if err := checkHex64(a.Sha256); err != nil {
		return fmt.Errorf("%s.sha256: %w", where, err)
	}
	if a.Length < 1 {
		return fmt.Errorf("%w: %s.length=%d < 1", ErrBadValue, where, a.Length)
	}
	return nil
}

// ============================================================================
// sources.json (§3.4)
// ============================================================================

// Sources — подписанная часть sources.json: allowlist зеркал.
type Sources struct {
	Type      string    `json:"type"`
	Version   int64     `json:"version"`
	Expires   time.Time `json:"expires"`
	Artifacts []string  `json:"artifacts"` // базовые URL артефактов
	Metadata  []string  `json:"metadata"`  // базовые URL метаданных
}

// ParseSources — строгий разбор подписанной части sources.json.
// HTTPS-only (§2 MITM/DNS-подмена): http-источники отвергаются.
func ParseSources(signed json.RawMessage) (*Sources, error) {
	if err := requireKeys(signed, "type", "version", "expires", "artifacts", "metadata"); err != nil {
		return nil, err
	}
	var s Sources
	if err := decodeStrict(signed, &s); err != nil {
		return nil, fmt.Errorf("sources: %w", err)
	}
	if s.Type != RoleSources {
		return nil, fmt.Errorf("%w: sources.json имеет type=%q", ErrWrongType, s.Type)
	}
	if s.Version < 1 {
		return nil, fmt.Errorf("%w: sources.version=%d < 1", ErrBadValue, s.Version)
	}
	if s.Expires.IsZero() {
		return nil, fmt.Errorf("%w: sources.expires отсутствует", ErrMissingField)
	}
	if len(s.Artifacts) == 0 || len(s.Metadata) == 0 {
		return nil, fmt.Errorf("%w: sources.artifacts/metadata пусты", ErrBadValue)
	}
	for _, list := range []struct {
		name string
		urls []string
	}{
		{"artifacts", s.Artifacts}, {"metadata", s.Metadata},
	} {
		for _, raw := range list.urls {
			if err := checkSourceURL(raw); err != nil {
				return nil, fmt.Errorf("sources.%s: %w", list.name, err)
			}
		}
	}
	return &s, nil
}

// checkSourceURL — только https, без фрагментов/запросов, непустой хост.
func checkSourceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %q не разбирается", ErrBadURL, raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w: %q не https", ErrBadURL, raw)
	}
	if u.Host == "" || u.Fragment != "" || u.RawQuery != "" || u.User != nil {
		return fmt.Errorf("%w: %q (хост/фрагмент/запрос/учётные данные)", ErrBadURL, raw)
	}
	return nil
}

// ============================================================================
// Верификация подписей (ed25519, порог роли)
// ============================================================================

// VerifyEnvelope — проверка конверта против роли корня доверия: каждая
// подпись считается только если keyid есть в keys И входит в роль; для
// подсчёта ключ учитывается один раз; валидных ≥ threshold.
//
// Отзыв (SECURITY §4.1): ключ, удалённый из keys новой версией root, здесь
// просто не находится — его подписи перестают считаться без изменения кода.
func VerifyEnvelope(env *Envelope, role string, root *Root) error {
	rl, ok := root.Roles[role]
	if !ok {
		return fmt.Errorf("%w: %q", ErrBadRole, role)
	}
	inRole := map[string]bool{}
	for _, id := range rl.Keyids {
		inRole[id] = true
	}
	valid := map[string]bool{}
	for _, s := range env.Signatures {
		if valid[s.Keyid] || !inRole[s.Keyid] {
			continue
		}
		key, ok := root.Keys[s.Keyid]
		if !ok || key.Alg != algEd25519 {
			continue // отозванный/неизвестный ключ (и не-ed25519) не считается
		}
		pub, err := base64.StdEncoding.DecodeString(key.Pub)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			continue
		}
		sig, err := base64.StdEncoding.DecodeString(s.Sig)
		if err != nil || len(sig) != ed25519.SignatureSize {
			continue
		}
		if !ed25519.Verify(ed25519.PublicKey(pub), env.Signed, sig) {
			continue // неверная подпись этим ключом — не считается
		}
		valid[s.Keyid] = true
	}
	if len(valid) < rl.Threshold {
		return fmt.Errorf("%w: роль %s — валидных %d из %d (порог %d)", ErrInsufficientSignatures, role, len(valid), len(rl.Keyids), rl.Threshold)
	}
	return nil
}
