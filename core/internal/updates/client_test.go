// SPDX-License-Identifier: GPL-3.0-only

// client_test.go — цепочка проверки (UPDATE-SYSTEM §7): корень → sources →
// timestamp → manifest → решение по версии; негативные сценарии.
package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"
)

type env struct {
	def rootDef
	src *memSource
	cl  *Client
}

func newEnv(t *testing.T, appVersion string) *env {
	t.Helper()
	d := newRootDef(t, 1, farFuture, "k1")
	src := newMem()
	return &env{def: d, src: src, cl: &Client{
		Trust: mustTrust(t, d.selfSigned(t)), Product: "lavray-windows", AppVersion: appVersion,
		Platform: "windows-x64", BootstrapSources: []Source{src}, State: &State{},
		PhaseTimeout: 5 * time.Second, Grace: 50 * time.Millisecond,
	}}
}

func (e *env) check(t *testing.T) (*Result, error) {
	t.Helper()
	return e.cl.Check(context.Background())
}

func TestCheckHappyPathNewerAndRepeat(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 4, tsVersion: 100})
	for i := 0; i < 2; i++ { // повтор того же релиза — не откат метаданных
		res, err := e.check(t)
		if err != nil || !res.Availability.Available || res.Availability.Version != "0.6.0" {
			t.Fatalf("проверка #%d: %+v %v", i, res, err)
		}
	}
	st := e.cl.State
	if st.SerialStable != 4 || st.TimestampVersion != 100 || st.RootVersion != 1 || st.LastCheckedAt == 0 {
		t.Fatalf("состояние после проверки: %+v", *st)
	}
}

func TestDowngradeSerialRejected(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.cl.State.SerialStable = 5
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 3, tsVersion: 100})
	if _, err := e.check(t); !errors.Is(err, ErrSerialRollback) {
		t.Fatalf("откат serial: %v", err)
	}
	if e.cl.State.LastCheckedAt != 0 {
		t.Fatal("неудачная проверка не должна менять last_checked_at")
	}
}

func TestWrongProductAndChannelRejected(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1, product: "lavray-linux"})
	if _, err := e.check(t); !errors.Is(err, ErrWrongProduct) {
		t.Fatalf("чужой product: %v", err)
	}
	e = newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1, channel: ChannelBeta, fileChannel: ChannelStable})
	if _, err := e.check(t); !errors.Is(err, ErrWrongChannel) {
		t.Fatalf("beta-манифест под именем stable: %v", err)
	}
}

func TestTimestampExpired(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	e := newEnv(t, "0.5.0")
	e.cl.Now = func() time.Time { return now }
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1, tsExpires: now})
	if _, err := e.check(t); !errors.Is(err, ErrExpired) {
		t.Fatalf("timestamp с expires == now просрочен (граница): %v", err)
	}
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1, tsExpires: now.Add(time.Second)})
	if _, err := e.check(t); err != nil {
		t.Fatalf("expires на секунду позже — действителен: %v", err)
	}
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1, tsExpires: now.Add(9 * 24 * time.Hour)})
	if _, err := e.check(t); !errors.Is(err, ErrTimestampHorizon) {
		t.Fatalf("окно свежести > 7 суток: %v", err)
	}
}

func TestForeignSignatureRejected(t *testing.T) {
	e := newEnv(t, "0.5.0")
	other := newRootDef(t, 1, farFuture, "k1") // чужие ключи
	other.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1})
	if _, err := e.check(t); !errors.Is(err, ErrInsufficientSignatures) {
		t.Fatalf("метаданные чужими ключами: %v", err)
	}
}

func TestArtifactRoleMissingFailsClosed(t *testing.T) {
	d := newRootDef(t, 1, farFuture, "k1")
	d.art = nil
	src := newMem()
	cl := &Client{Trust: mustTrust(t, d.selfSigned(t)), BootstrapSources: []Source{src}}
	if _, err := cl.Check(context.Background()); !errors.Is(err, ErrArtifactRoleMissing) {
		t.Fatalf("корень без роли artifact: %v", err)
	}
	if _, err := (&Client{}).Check(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("клиент без корня: %v", err)
	}
}

func TestLargestValidTimestampWins(t *testing.T) {
	e := newEnv(t, "0.5.0")
	a, b := newMem(), newMem()
	e.def.publish(t, a, relOpts{version: "0.6.0", serial: 2, tsVersion: 5})
	e.def.publish(t, b, relOpts{version: "0.6.0", serial: 2, tsVersion: 9})
	e.cl.BootstrapSources = []Source{a, b}
	res, err := e.check(t)
	if err != nil || res.Timestamp.Version != 9 || res.Metadata != Source(b) {
		t.Fatalf("побеждает наибольшая версия: %+v %v", res, err)
	}
	if e.cl.State.TimestampVersion != 9 {
		t.Fatalf("сохранённая версия timestamp: %d", e.cl.State.TimestampVersion)
	}
}

func TestTimestampRollbackRejected(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.cl.State.TimestampVersion = 10
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 2, tsVersion: 5})
	if _, err := e.check(t); !errors.Is(err, ErrSerialRollback) {
		t.Fatalf("откат timestamp: %v", err)
	}
	// второй источник со свежим timestamp спасает проверку: откатный источник игнорируется
	fresh := newMem()
	e.def.publish(t, fresh, relOpts{version: "0.6.0", serial: 2, tsVersion: 12})
	e.cl.BootstrapSources = []Source{e.src, fresh}
	if res, err := e.check(t); err != nil || res.Timestamp.Version != 12 {
		t.Fatalf("валидный источник побеждает откатный: %v", err)
	}
}

func TestAllSourcesFailIsCheckFailed(t *testing.T) {
	e := newEnv(t, "0.5.0")
	a, b := newMem(), newMem()
	a.down, b.down = true, true
	e.cl.BootstrapSources = []Source{a, b}
	if _, err := e.check(t); !errors.Is(err, ErrNetwork) {
		t.Fatalf("все источники недоступны: %v", err)
	}
	if e.cl.State.LastCheckedAt != 0 {
		t.Fatal("last_checked_at меняется только успешной проверкой")
	}
}

// X: медленный источник не задерживает выбор timestamp (опрос параллельный,
// после первого ответа остальным даётся короткая отсрочка).
func TestSlowSourceTimeout(t *testing.T) {
	e := newEnv(t, "0.5.0")
	slow, fast := newMem(), newMem()
	e.def.publish(t, slow, relOpts{version: "0.6.0", serial: 1, tsVersion: 1})
	e.def.publish(t, fast, relOpts{version: "0.6.0", serial: 1, tsVersion: 2})
	slow.delay = time.Minute
	start := time.Now()
	res, err := FetchTimestamp(context.Background(), e.cl.Trust, []Source{slow, fast},
		TimestampPolicy{Now: time.Now(), Timeout: 5 * time.Second, Grace: 100 * time.Millisecond})
	if err != nil || res.Timestamp.Version != 2 {
		t.Fatalf("медленный источник не должен блокировать: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("ожидание медленного источника: %v", time.Since(start))
	}
}

func TestStableUserDoesNotSeeBeta(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0-rc.1", serial: 9, tsVersion: 3, channel: ChannelBeta})
	res, err := e.check(t)
	if err != nil || res.Manifest != nil || res.Availability.Available {
		t.Fatalf("stable не видит beta (и это не ошибка): %+v %v", res, err)
	}
	e.cl.Channel = ChannelBeta
	res, err = e.check(t)
	if err != nil || !res.Availability.Available || res.Availability.Version != "0.6.0-rc.1" {
		t.Fatalf("beta видит rc: %+v %v", res, err)
	}
	if e.cl.State.SerialBeta != 9 || e.cl.State.SerialStable != 0 {
		t.Fatalf("serial по каналам: %+v", *e.cl.State)
	}
}

func TestCheckPersistsRotatedRoot(t *testing.T) {
	e := newEnv(t, "0.5.0")
	r2 := newRootDef(t, 2, farFuture, "k2")
	e.src.put("2.root.json", r2.rotatedFrom(t, e.def))
	r2.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1})
	store := &fakeRootStore{}
	e.cl.RootStore = store
	if _, err := e.check(t); err != nil {
		t.Fatalf("после ротации метаданные подписаны новыми ключами: %v", err)
	}
	if store.saves != 1 || e.cl.State.RootVersion != 2 {
		t.Fatalf("принятый root.json сохраняется: saves=%d root=%d", store.saves, e.cl.State.RootVersion)
	}
}

func TestThresholdEnforcedOnRotation(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1", "k2")
	r1.threshold = 2
	r2 := newRootDef(t, 2, farFuture, "k3")
	src := newMem()
	start := mustTrust(t, r1.selfSigned(t))
	one := map[string]signer{"k1": r1.rootKeys["k1"], "k3": r2.rootKeys["k3"]}
	src.put("2.root.json", envelope(t, r2.signed(t), one))
	if _, _, err := AdvanceRoot(context.Background(), []Source{src}, start, time.Now()); !errors.Is(err, ErrRootRotation) {
		t.Fatalf("одной подписи из двух недостаточно: %v", err)
	}
	both := map[string]signer{"k1": r1.rootKeys["k1"], "k2": r1.rootKeys["k2"], "k3": r2.rootKeys["k3"]}
	src.put("2.root.json", envelope(t, r2.signed(t), both))
	if got, _, err := AdvanceRoot(context.Background(), []Source{src}, start, time.Now()); err != nil || got.Root().Version != 2 {
		t.Fatalf("порог 2 из 2: %v", err)
	}
}

func TestClientDownloadStagesVerifiedInstaller(t *testing.T) {
	e := newEnv(t, "0.5.0")
	data := testData(150_000)
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1, data: data})
	res, err := e.check(t)
	if err != nil {
		t.Fatal(err)
	}
	res.Mirrors = []ArtifactSource{e.src}
	dir := t.TempDir()
	var last int64
	st, err := e.cl.Download(context.Background(), res, dir, func(done, _ int64) { last = done })
	if err != nil || st.Version != "0.6.0" || last != int64(len(data)) {
		t.Fatalf("Download: %+v %v last=%d", st, err, last)
	}
	if got, _ := os.ReadFile(st.Path); string(got) != string(data) {
		t.Fatal("содержимое staging")
	}
	// подмена trusted comment (другая версия) — установщик отвергается и не остаётся
	res.Manifest.App.Version = "9.9.9"
	if _, err := e.cl.Download(context.Background(), res, t.TempDir(), nil); !errors.Is(err, ErrMinisigTrustedComment) {
		t.Fatalf("версия не совпала с подписанным заявлением: %v", err)
	}
	// не доступно — скачивать нечего
	res.Availability.Available = false
	if _, err := e.cl.Download(context.Background(), res, t.TempDir(), nil); !errors.Is(err, ErrNoCheck) {
		t.Fatalf("нет обновления: %v", err)
	}
}

func TestSourcesJSONAddsMirrors(t *testing.T) {
	e := newEnv(t, "0.5.0")
	doc := []byte(`{"type":"sources","version":1,"expires":"` + farFuture.UTC().Format(time.RFC3339) +
		`","artifacts":["https://mirror-a.invalid/dl"],"metadata":["https://mirror-m.invalid/meta"]}`)
	e.src.put("sources.json", envelope(t, doc, map[string]signer{"r1": e.def.man}))
	srcs, signed := e.cl.fetchSources(context.Background(), time.Now())
	if signed == nil || len(srcs) != 2 || len(mirrors(signed, e.cl.BootstrapSources, nil)) != 2 {
		t.Fatalf("подписанный список добавляет зеркала: srcs=%d signed=%v", len(srcs), signed)
	}
	// список, подписанный чужим ключом, игнорируется
	e.src.put("sources.json", envelope(t, doc, map[string]signer{"r1": newSigner(t)}))
	if srcs, signed = e.cl.fetchSources(context.Background(), time.Now()); signed != nil || len(srcs) != 1 {
		t.Fatalf("чужой sources.json: srcs=%d", len(srcs))
	}
}

// Подменённый манифест (валидная подпись, но не тот документ, что пиннит timestamp)
// даёт ErrManifestMismatch — отдельную причину проверки, а не безымянный сбой.
func TestSwappedManifestIsManifestMismatch(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1})
	old := append([]byte(nil), e.src.files[ManifestName(ChannelStable)]...)
	e.def.publish(t, e.src, relOpts{version: "0.7.0", serial: 2, tsVersion: 2})
	e.src.put(ManifestName(ChannelStable), old) // подпись настоящая, документ другой
	_, err := e.check(t)
	if !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("ожидалась ErrManifestMismatch: %v", err)
	}
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("прежний признак ErrHashMismatch сохраняется: %v", err)
	}
	if e.cl.State.LastCheckedAt != 0 {
		t.Fatal("неудачная проверка не меняет last_checked_at")
	}
}

// Длина и serial манифеста против пина timestamp — тоже подмена манифеста.
func TestManifestPinLengthAndSerialMismatch(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 3, tsVersion: 1})
	raw := e.src.files[ManifestName(ChannelStable)]
	trust := mustTrust(t, e.def.selfSigned(t))
	env, err := trust.Verify(raw, RoleManifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(env.Signed)
	good := TimestampEntry{Serial: 3, Sha256: hex.EncodeToString(sum[:]), Length: int64(len(env.Signed))}
	q := ManifestQuery{Channel: ChannelStable, Product: "lavray-windows", Now: time.Now()}
	if _, err := checkManifest(trust, raw, good, q); err != nil {
		t.Fatalf("исходный пин: %v", err)
	}
	badLen, badSerial := good, good
	badLen.Length++
	badSerial.Serial++
	for name, entry := range map[string]TimestampEntry{"length": badLen, "serial": badSerial} {
		if _, err := checkManifest(trust, raw, entry, q); !errors.Is(err, ErrManifestMismatch) {
			t.Errorf("%s: ожидалась ErrManifestMismatch: %v", name, err)
		}
	}
}
