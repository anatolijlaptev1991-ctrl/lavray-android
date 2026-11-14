// SPDX-License-Identifier: GPL-3.0-only

package updatestest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// Env — набор одноразовых ключей и подписанный ими корень версии 1 (порог 1).
// Ключи живут только в памяти теста; на диск и в дерево не попадают.
type Env struct {
	Root, TS, Man ed25519.PrivateKey
	Art           ed25519.PrivateKey
	ArtID         [8]byte
	RootJSON      []byte
	Product       string
}

// New генерирует ключи и корень (срок действия — два года).
func New(t testing.TB) *Env {
	t.Helper()
	e := &Env{Product: "lavray-windows"}
	for _, k := range []*ed25519.PrivateKey{&e.Root, &e.TS, &e.Man, &e.Art} {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		*k = priv
	}
	if _, err := rand.Read(e.ArtID[:]); err != nil {
		t.Fatal(err)
	}
	e.RootJSON = e.signRoot(t)
	return e
}

func pub(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

func (e *Env) signRoot(t testing.TB) []byte {
	t.Helper()
	artKey := updates.MinisignKey{KeyID: e.ArtID, Pub: e.Art.Public().(ed25519.PublicKey)}
	role := func(id string) map[string]any { return map[string]any{"keyids": []string{id}, "threshold": 1} }
	signed, err := json.Marshal(map[string]any{
		"type": "root", "version": 1, "expires": time.Now().Add(2 * 365 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"keys": map[string]any{
			"k1": map[string]any{"alg": "ed25519", "pub": pub(e.Root)},
			"t1": map[string]any{"alg": "ed25519", "pub": pub(e.TS)},
			"r1": map[string]any{"alg": "ed25519", "pub": pub(e.Man)},
			"a1": map[string]any{"alg": "minisign", "pub": base64.StdEncoding.EncodeToString(artKey.Blob())},
		},
		"roles": map[string]any{"root": role("k1"), "timestamp": role("t1"), "manifest": role("r1"),
			"sources": role("r1"), "artifact": role("a1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return Envelope(t, signed, "k1", e.Root)
}

// Envelope — конверт {"signed", "signatures"} с одной подписью.
func Envelope(t testing.TB, signed []byte, keyID string, k ed25519.PrivateKey) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"signed": json.RawMessage(signed), "signatures": []any{
		map[string]any{"keyid": keyID, "sig": base64.StdEncoding.EncodeToString(ed25519.Sign(k, signed))}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Release — публикуемый релиз (нулевые поля заполняются разумными значениями).
type Release struct {
	Version, Channel, MinUpgradeFrom string
	Serial, TSVersion                int64
	Data                             []byte
	Mandatory                        bool
	Notes                            map[string]string
	Platform                         string // по умолчанию windows-x64
	TSExpires                        time.Time
	// Presets — обновлённые встроенные наборы (id → ревизия и файл); Geo — zip-архив
	// гео-набора (nil — секции geo в манифесте нет).
	Presets map[string]PresetArtifact
	Geo     []byte
	// GeoRev — ревизия гео-набора (0 — берётся serial релиза): монотонна, клиент не откатывается назад.
	GeoRev int64
	// MutateManifest правит собранный манифест до подписи (например, убрать
	// обязательное поле): подпись остаётся настоящей, документ — неверного вида.
	MutateManifest func(mf map[string]any)
}

// PresetArtifact — файл набора ревизии Rev, публикуемый в секции presets манифеста.
type PresetArtifact struct {
	Rev  int
	Data []byte
}

// Publish кладёт в src подписанные timestamp, manifest и установщик; возвращает
// путь установщика на источнике.
func (e *Env) Publish(t testing.TB, src *Source, r Release) string {
	t.Helper()
	if r.Channel == "" {
		r.Channel = updates.ChannelStable
	}
	if r.Platform == "" {
		r.Platform = "windows-x64"
	}
	if r.Data == nil {
		r.Data = []byte("installer-" + r.Version)
	}
	if r.TSExpires.IsZero() {
		r.TSExpires = time.Now().Add(6 * 24 * time.Hour)
	}
	sum := sha256.Sum256(r.Data)
	claims := updates.ArtifactClaims{Product: e.Product, Platform: r.Platform, Channel: r.Channel, Version: r.Version, Serial: r.Serial}
	path := "v" + r.Version + "/lavray-setup.exe"
	sig := updates.SignMinisigPrehashed(e.Art, e.ArtID, updates.DigestOf(r.Data), claims.String())
	app := map[string]any{"version": r.Version, "assets": map[string]any{r.Platform: map[string]any{
		"path": path, "sha256": hex.EncodeToString(sum[:]), "length": len(r.Data), "minisig": string(sig)}}}
	if r.MinUpgradeFrom != "" {
		app["minUpgradeFrom"] = r.MinUpgradeFrom
	}
	mf := map[string]any{"type": "manifest", "product": e.Product, "channel": r.Channel, "serial": r.Serial,
		"expires": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339), "mandatory": r.Mandatory, "app": app}
	if r.Notes != nil {
		mf["notes"] = r.Notes
	}
	e.publishComponents(src, mf, r)
	if r.MutateManifest != nil {
		r.MutateManifest(mf)
	}
	signed, _ := json.Marshal(mf)
	name := updates.ManifestName(r.Channel)
	src.Put(name, Envelope(t, signed, "r1", e.Man))
	src.Put(path, r.Data)
	msum := sha256.Sum256(signed)
	ts, _ := json.Marshal(map[string]any{"type": "timestamp", "version": r.TSVersion,
		"expires": r.TSExpires.UTC().Format(time.RFC3339),
		"meta":    map[string]any{name: map[string]any{"serial": r.Serial, "sha256": hex.EncodeToString(msum[:]), "length": len(signed)}}})
	src.Put("timestamp.json", Envelope(t, ts, "t1", e.TS))
	return path
}

// publishComponents кладёт на источник файлы наборов и гео-архив и добавляет
// секции presets и geo в манифест mf (sha256 и длина — как у настоящего релиза).
func (e *Env) publishComponents(src *Source, mf map[string]any, r Release) {
	if len(r.Presets) > 0 {
		entries := map[string]any{}
		for id, a := range r.Presets {
			path := fmt.Sprintf("presets/%s-%d.json", id, a.Rev)
			sum := sha256.Sum256(a.Data)
			src.Put(path, a.Data)
			entries[id] = map[string]any{"rev": a.Rev, "path": path, "sha256": hex.EncodeToString(sum[:])}
		}
		mf["presets"] = entries
	}
	if r.Geo != nil {
		sum := sha256.Sum256(r.Geo)
		src.Put("geo/set.zip", r.Geo)
		rev := r.GeoRev
		if rev == 0 {
			rev = r.Serial
		}
		mf["geo"] = map[string]any{"path": "geo/set.zip", "sha256": hex.EncodeToString(sum[:]),
			"length": len(r.Geo), "generatedAt": "2026-10-20", "rev": rev}
	}
}
