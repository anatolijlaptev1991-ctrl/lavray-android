// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	farFuture = time.Now().Add(2 * 365 * 24 * time.Hour)
	longPast  = time.Now().Add(-48 * time.Hour)
)

func TestRotationChainNToN3(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2")
	r3 := newRootDef(t, 3, farFuture, "k3")
	src := newMem()
	src.put("2.root.json", r2.rotatedFrom(t, r1))
	src.put("3.root.json", r3.rotatedFrom(t, r2))
	got, steps, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now())
	if err != nil || steps != 2 || got.Root().Version != 3 {
		t.Fatalf("v=%v steps=%d err=%v", got, steps, err)
	}
}

func TestExpiredEmbeddedRootRotatesToValid(t *testing.T) {
	r1 := newRootDef(t, 1, longPast, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2")
	src := newMem()
	src.put("2.root.json", r2.rotatedFrom(t, r1))
	got, steps, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now())
	if err != nil || steps != 1 || got.Root().Version != 2 {
		t.Fatalf("просроченный вшитый корень должен ротироваться: %v %d %v", got, steps, err)
	}
}

func TestFinalRootExpiredFails(t *testing.T) {
	r1 := newRootDef(t, 1, longPast, "k1")
	if _, _, err := AdvanceRoot(context.Background(), []Source{newMem()}, mustTrust(t, r1.selfSigned(t)), time.Now()); !errors.Is(err, ErrExpired) {
		t.Fatalf("итоговый просроченный корень: %v", err)
	}
	r2 := newRootDef(t, 2, longPast.Add(time.Hour), "k2")
	src := newMem()
	src.put("2.root.json", r2.rotatedFrom(t, r1))
	if _, _, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now()); !errors.Is(err, ErrExpired) {
		t.Fatalf("просрочен и итоговый v2: %v", err)
	}
}

func TestRotationStopsAt404(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	got, steps, err := AdvanceRoot(context.Background(), []Source{newMem(), newMem()}, mustTrust(t, r1.selfSigned(t)), time.Now())
	if err != nil || steps != 0 || got.Root().Version != 1 {
		t.Fatalf("нет N+1: %v %d %v", got, steps, err)
	}
}

func TestRotationSkipRejected(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r3 := newRootDef(t, 3, farFuture, "k3")
	src := newMem()
	src.put("2.root.json", r3.rotatedFrom(t, r1)) // под именем v2 лежит v3
	if _, _, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now()); !errors.Is(err, ErrRootRotation) {
		t.Fatalf("пропуск версии: %v", err)
	}
}

func TestRotationRequiresNewKeys(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2")
	src := newMem()
	// подписан только СТАРЫМ ключом: компрометация k1 не должна переписать доверие
	src.put("2.root.json", envelope(t, r2.signed(t), r1.rootKeys))
	if _, _, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now()); !errors.Is(err, ErrRootRotation) {
		t.Fatalf("корень без подписи новых ключей: %v", err)
	}
	// подписан только НОВЫМ ключом: порог старого корня не достигнут
	src.put("2.root.json", envelope(t, r2.signed(t), r2.rootKeys))
	if _, _, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now()); !errors.Is(err, ErrRootRotation) {
		t.Fatalf("корень без подписи порога старых ключей: %v", err)
	}
}

func TestRevokedKeyRejectedAfterTwoRotations(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2") // k1 отозван, ключи timestamp/manifest новые
	r3 := newRootDef(t, 3, farFuture, "k3")
	src := newMem()
	src.put("2.root.json", r2.rotatedFrom(t, r1))
	src.put("3.root.json", r3.rotatedFrom(t, r2))
	tr, _, err := AdvanceRoot(context.Background(), []Source{src}, mustTrust(t, r1.selfSigned(t)), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	oldTS := map[string]signer{"t1": r1.ts}
	stale := envelope(t, []byte(`{"type":"timestamp"}`), oldTS)
	if _, err := tr.Verify(stale, RoleTimestamp); err == nil {
		t.Fatal("timestamp, подписанный ключом из корня v1, принят после двух ротаций")
	}
	if _, err := tr.Verify(envelope(t, []byte(`{"type":"timestamp"}`), map[string]signer{"t1": r3.ts}), RoleTimestamp); err != nil {
		t.Fatalf("ключ из итогового корня должен работать: %v", err)
	}
	// попытка «воскресить» отозванный k1: v4, подписанный только k1 и новым ключом
	r4 := newRootDef(t, 4, farFuture, "k4")
	src.put("4.root.json", r4.rotatedFrom(t, r1))
	if _, _, err := AdvanceRoot(context.Background(), []Source{src}, tr, time.Now()); !errors.Is(err, ErrRootRotation) {
		t.Fatalf("корень, подписанный отозванным ключом: %v", err)
	}
}

func TestRotationLoopCapped(t *testing.T) {
	prev := newRootDef(t, 1, farFuture, "k1")
	start := mustTrust(t, prev.selfSigned(t))
	src := newMem()
	for v := int64(2); v <= MaxRootSteps+6; v++ {
		next := newRootDef(t, v, farFuture, fmt.Sprintf("k%d", v))
		src.put(RootName(v), next.rotatedFrom(t, prev))
		prev = next
	}
	got, steps, err := AdvanceRoot(context.Background(), []Source{src}, start, time.Now())
	if err != nil || steps != MaxRootSteps || got.Root().Version != 1+MaxRootSteps {
		t.Fatalf("цепочка должна обрываться на %d шагах: v=%d steps=%d err=%v", MaxRootSteps, got.Root().Version, steps, err)
	}
}

func TestRootPersistedNotDowngraded(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2")
	r3 := newRootDef(t, 3, farFuture, "k3")
	store := &fakeRootStore{}
	chain := mustTrust(t, r1.selfSigned(t))
	for _, doc := range [][]byte{r2.rotatedFrom(t, r1), r3.rotatedFrom(t, r2)} {
		var err error
		if chain, err = chain.VerifyRootUpdate(doc); err != nil {
			t.Fatal(err)
		}
	}
	if err := PersistRoot(store, chain); err != nil || store.saves != 2 {
		t.Fatalf("сохранение цепочки v2, v3: %v saves=%d", err, store.saves)
	}
	if err := PersistRoot(store, chain); err != nil || store.saves != 2 {
		t.Fatalf("повторное сохранение идемпотентно: saves=%d", store.saves)
	}
	got, err := ResolveTrust(r1.selfSigned(t), store)
	if err != nil || got.Root().Version != 3 {
		t.Fatalf("вшитый v1 + цепочка в хранилище = v3: %v %v", got, err)
	}
	// более новый вшитый корень (новая сборка) начинает цепочку заново
	r4 := newRootDef(t, 4, farFuture, "k4")
	got, err = ResolveTrust(r4.rotatedFrom(t, r3), store)
	if err != nil || got.Root().Version != 4 {
		t.Fatalf("новый вшитый корень: %v %v", got, err)
	}
	if _, err = ResolveTrust(nil, store); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("нет вшитого корня: %v", err)
	}
}

// S: подложенный в хранилище самоподписанный корень (хоть v2, хоть v7) не
// принимается: цепочка проверяется от вшитого корня.
func TestForgedSelfSignedRootInStoreIgnored(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	evil2 := newRootDef(t, 2, farFuture, "evil")
	evil7 := newRootDef(t, 7, farFuture, "evil")
	store := &fakeRootStore{docs: map[int64][]byte{2: evil2.selfSigned(t), 7: evil7.selfSigned(t)}}
	got, err := ResolveTrust(r1.selfSigned(t), store)
	if err != nil || got.Root().Version != 1 {
		t.Fatalf("самоподписанная подделка принята: v%d %v", got.Root().Version, err)
	}
	// корень v2 подписан не теми ключами (только новым) — тоже не цепляется
	r2 := newRootDef(t, 2, farFuture, "k2")
	store.docs[2] = envelope(t, r2.signed(t), r2.rootKeys)
	if got, _ = ResolveTrust(r1.selfSigned(t), store); got.Root().Version != 1 {
		t.Fatal("корень без подписи порога прежнего принят")
	}
}

func TestStoredChainStopsAtFirstBrokenLink(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2")
	r3 := newRootDef(t, 3, farFuture, "k3")
	r4 := newRootDef(t, 4, farFuture, "k4")
	bad3 := r3.rotatedFrom(t, r1) // v3 подписан ключами v1, а не v2
	store := &fakeRootStore{docs: map[int64][]byte{
		2: r2.rotatedFrom(t, r1), 3: bad3, 4: r4.rotatedFrom(t, r3)}}
	got, err := ResolveTrust(r1.selfSigned(t), store)
	if err != nil || got.Root().Version != 2 {
		t.Fatalf("цепочка обрывается на битом звене: v%d %v", got.Root().Version, err)
	}
	store.docs[5] = []byte("мусор")
	if _, err := ResolveTrust(r1.selfSigned(t), store); err != nil {
		t.Fatalf("мусор в хранилище не ошибка: %v", err)
	}
}

func TestDirRootStoreRoundTripAndGarbage(t *testing.T) {
	dir := t.TempDir()
	st := DirRootStore{Dir: filepath.Join(dir, "roots")}
	if docs, err := st.LoadRoots(); err != nil || len(docs) != 0 {
		t.Fatalf("нет каталога: %v %v", docs, err)
	}
	r1 := newRootDef(t, 1, farFuture, "k1")
	r2 := newRootDef(t, 2, farFuture, "k2")
	doc := r2.rotatedFrom(t, r1)
	if err := st.SaveRoot(2, doc); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(st.Dir, "junk.txt"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(st.Dir, "9.root.json"), bytes.Repeat([]byte("a"), maxRootFileBytes+1), 0o600)
	_ = os.Mkdir(filepath.Join(st.Dir, "3.root.json"), 0o700)
	docs, err := st.LoadRoots()
	if err != nil || len(docs) != 1 || !bytes.Equal(docs[0], doc) {
		t.Fatalf("читается только корректный файл: %d %v", len(docs), err)
	}
	if got, _ := ResolveTrust(r1.selfSigned(t), st); got.Root().Version != 2 {
		t.Fatal("цепочка из каталога")
	}
}

func TestRootWithoutArtifactRole(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	r1.art = nil
	tr := mustTrust(t, r1.selfSigned(t))
	if _, err := tr.Root().ArtifactKeys(); !errors.Is(err, ErrArtifactRoleMissing) {
		t.Fatalf("корень без роли artifact: %v", err)
	}
}

func TestRootRejectsMixedKeyAlgs(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	for name, role := range map[string]string{"timestamp": "a1", "artifact": "t1"} {
		var doc map[string]any
		if err := json.Unmarshal(r1.signed(t), &doc); err != nil {
			t.Fatal(err)
		}
		roles := doc["roles"].(map[string]any)
		roles[name] = map[string]any{"keyids": []string{role}, "threshold": 1}
		raw, _ := json.Marshal(doc)
		if _, err := ParseRoot(raw); !errors.Is(err, ErrBadValue) {
			t.Errorf("роль %s с ключом %s другого алгоритма принята: %v", name, role, err)
		}
	}
}
