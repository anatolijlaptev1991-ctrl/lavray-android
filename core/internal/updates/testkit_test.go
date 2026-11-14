// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"testing"
	"time"
)

// memSource — Source и ArtifactSource в памяти.
type memSource struct {
	mu    sync.Mutex
	files map[string][]byte
	delay time.Duration // искусственная задержка Fetch (медленный источник)
	down  bool          // источник недоступен
	hits  map[string]int
}

func (m *memSource) Fetch(ctx context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	m.hit(name)
	down, delay := m.down, m.delay
	b, ok := m.files[name]
	m.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	switch {
	case down:
		return nil, fmt.Errorf("источник недоступен")
	case !ok:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return append([]byte(nil), b...), nil
}

func (m *memSource) Open(_ context.Context, name string, off int64) (*ArtifactStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hit("open:" + name)
	b, ok := m.files[name]
	if !ok || m.down {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return &ArtifactStream{Body: io.NopCloser(bytes.NewReader(b[off:])), Start: off, Total: int64(len(b))}, nil
}

func (m *memSource) hit(name string) {
	if m.hits == nil {
		m.hits = map[string]int{}
	}
	m.hits[name]++
}

func (m *memSource) put(name string, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = b
}

type signer struct{ priv ed25519.PrivateKey }

func newSigner(t *testing.T) signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return signer{priv: priv}
}

func b64pub(t *testing.T, s signer) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(s.priv.Public().(ed25519.PublicKey))
}

// relOpts — параметры публикации релиза в память-источник.
type relOpts struct {
	product, channel, version, minFrom string
	assetKey                           string // по умолчанию windows-x64
	serial, tsVersion                  int64
	tsExpires, mfExpires               time.Time
	mandatory                          bool
	data                               []byte
	noApp                              bool
	fileChannel                        string // имя файла манифеста (если отличается от channel)
}

// publish кладёт в источник артефакт, manifest-<channel>.json и timestamp.json,
// подписанные ключами корня d; возвращает актив манифеста.
func (d rootDef) publish(t *testing.T, m *memSource, o relOpts) Asset {
	t.Helper()
	if o.assetKey == "" {
		o.assetKey = "windows-x64"
	}
	if o.product == "" {
		o.product = "lavray-windows"
	}
	if o.channel == "" {
		o.channel = ChannelStable
	}
	if o.tsExpires.IsZero() {
		o.tsExpires = time.Now().Add(7 * 24 * time.Hour).Add(-time.Hour)
	}
	if o.mfExpires.IsZero() {
		o.mfExpires = time.Now().Add(30 * 24 * time.Hour)
	}
	if o.data == nil {
		o.data = []byte("installer-" + o.version)
	}
	sum := sha256.Sum256(o.data)
	claims := ArtifactClaims{o.product, o.assetKey, o.channel, o.version, o.serial}
	asset := map[string]any{"path": "v" + o.version + "/lavray-setup.exe", "sha256": hex.EncodeToString(sum[:]),
		"length": len(o.data), "minisig": string(d.art.sign(o.data, claims.String()))}
	mf := map[string]any{"type": "manifest", "product": o.product, "channel": o.channel, "serial": o.serial,
		"expires": o.mfExpires.UTC().Format(time.RFC3339), "mandatory": o.mandatory}
	if !o.noApp {
		app := map[string]any{"version": o.version, "assets": map[string]any{o.assetKey: asset}}
		if o.minFrom != "" {
			app["minUpgradeFrom"] = o.minFrom
		}
		mf["app"] = app
	}
	signed, _ := json.Marshal(mf)
	name := ManifestName(o.channel)
	if o.fileChannel != "" {
		name = ManifestName(o.fileChannel)
	}
	m.put(name, envelope(t, signed, map[string]signer{"r1": d.man}))
	m.put("v"+o.version+"/lavray-setup.exe", o.data)
	msum := sha256.Sum256(signed)
	tsDoc, _ := json.Marshal(map[string]any{"type": "timestamp", "version": o.tsVersion,
		"expires": o.tsExpires.UTC().Format(time.RFC3339),
		"meta":    map[string]any{name: map[string]any{"serial": o.serial, "sha256": hex.EncodeToString(msum[:]), "length": len(signed)}}})
	m.put("timestamp.json", envelope(t, tsDoc, map[string]signer{"t1": d.ts}))
	return Asset{Path: asset["path"].(string), Sha256: asset["sha256"].(string), Length: int64(len(o.data))}
}

// rootDef — описание корня для тестов цепочки: ключи роли root (keyid → ключ),
// ключи timestamp/manifest/sources и (необязательно) artifact-minisign.
type rootDef struct {
	version   int64
	expires   time.Time
	rootKeys  map[string]signer
	threshold int
	ts, man   signer
	art       *testMinisignKey
}

func newRootDef(t *testing.T, version int64, expires time.Time, rootKeyIDs ...string) rootDef {
	t.Helper()
	keys := map[string]signer{}
	for _, id := range rootKeyIDs {
		keys[id] = newSigner(t)
	}
	art := newMinisignKey(t)
	return rootDef{version: version, expires: expires, rootKeys: keys, threshold: 1,
		ts: newSigner(t), man: newSigner(t), art: &art}
}

func (d rootDef) signed(t *testing.T) json.RawMessage {
	t.Helper()
	keys := map[string]any{
		"t1": map[string]any{"alg": "ed25519", "pub": b64pub(t, d.ts)},
		"r1": map[string]any{"alg": "ed25519", "pub": b64pub(t, d.man)},
	}
	ids := make([]string, 0, len(d.rootKeys))
	for id, s := range d.rootKeys {
		keys[id] = map[string]any{"alg": "ed25519", "pub": b64pub(t, s)}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	roles := map[string]any{
		"root":      map[string]any{"keyids": ids, "threshold": d.threshold},
		"timestamp": map[string]any{"keyids": []string{"t1"}, "threshold": 1},
		"manifest":  map[string]any{"keyids": []string{"r1"}, "threshold": 1},
		"sources":   map[string]any{"keyids": []string{"r1"}, "threshold": 1},
	}
	if d.art != nil {
		keys["a1"] = map[string]any{"alg": "minisign", "pub": base64.StdEncoding.EncodeToString(d.art.key.Blob())}
		roles["artifact"] = map[string]any{"keyids": []string{"a1"}, "threshold": 1}
	}
	doc := map[string]any{"type": "root", "version": d.version,
		"expires": d.expires.UTC().Format(time.RFC3339), "keys": keys, "roles": roles}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// envelope подписывает документ перечисленными ключами (keyid → ключ).
func envelope(t *testing.T, signed json.RawMessage, by map[string]signer) []byte {
	t.Helper()
	sigs := []any{}
	for id, s := range by {
		sigs = append(sigs, map[string]any{"keyid": id,
			"sig": base64.StdEncoding.EncodeToString(ed25519.Sign(s.priv, signed))})
	}
	b, err := json.Marshal(map[string]any{"signed": signed, "signatures": sigs})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// selfSigned — корень, подписанный своими ключами (вшитый в сборку).
func (d rootDef) selfSigned(t *testing.T) []byte { return envelope(t, d.signed(t), d.rootKeys) }

// rotatedFrom — корень d, подписанный ключами предыдущего корня и своими.
func (d rootDef) rotatedFrom(t *testing.T, prev rootDef) []byte {
	t.Helper()
	by := map[string]signer{}
	for id, s := range prev.rootKeys {
		by[id] = s
	}
	for id, s := range d.rootKeys {
		by[id] = s
	}
	return envelope(t, d.signed(t), by)
}

func mustTrust(t *testing.T, raw []byte) *Trust {
	t.Helper()
	tr, err := NewTrust(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func newMem() *memSource { return &memSource{files: map[string][]byte{}} }

// fakeRootStore — RootStore в памяти (считает записи).
type fakeRootStore struct {
	docs  map[int64][]byte
	saves int
}

func (f *fakeRootStore) LoadRoots() ([][]byte, error) {
	var out [][]byte
	for _, raw := range f.docs {
		out = append(out, raw)
	}
	return out, nil
}

func (f *fakeRootStore) SaveRoot(version int64, raw []byte) error {
	if f.docs == nil {
		f.docs = map[int64][]byte{}
	}
	f.docs[version] = append([]byte(nil), raw...)
	f.saves++
	return nil
}
