// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// rotateWith выпускает корень v2 из v1, изменённый функцией mutate (корневой
// ключ k1 остаётся, поэтому ротация валидна).
func rotateWith(t *testing.T, prev rootDef, mutate func(doc map[string]any)) *Trust {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(prev.signed(t), &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = 2
	mutate(doc)
	raw, _ := json.Marshal(doc)
	next, err := mustTrust(t, prev.selfSigned(t)).VerifyRootUpdate(envelope(t, raw, prev.rootKeys))
	if err != nil {
		t.Fatalf("корень v2: %v", err)
	}
	return next
}

func keyOf(t *testing.T, s signer) map[string]any {
	t.Helper()
	return map[string]any{"alg": "ed25519", "pub": b64pub(t, s)}
}

func role(keyids ...string) map[string]any {
	return map[string]any{"keyids": keyids, "threshold": 1}
}

// Сброс счётчиков нужен, только когда ключ роли УДАЛЁН. Добавленный или
// переименованный ключ при действующем старом счётчики не трогает.
func TestKeysRemovedSinceOnlyWhenAKeyOfTheRoleDisappears(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	extra := newSigner(t)
	cases := map[string]struct {
		mutate func(doc map[string]any)
		want   bool
	}{
		"добавлен второй ключ timestamp": {func(d map[string]any) {
			d["keys"].(map[string]any)["t2"] = keyOf(t, extra)
			d["roles"].(map[string]any)["timestamp"] = role("t1", "t2")
		}, false},
		"тот же ключ под новым именем": {func(d map[string]any) {
			keys := d["keys"].(map[string]any)
			keys["t9"] = keys["t1"]
			delete(keys, "t1")
			d["roles"].(map[string]any)["timestamp"] = role("t9")
		}, false},
		"ключ заменён другим": {func(d map[string]any) {
			d["keys"].(map[string]any)["t1"] = keyOf(t, extra)
		}, true},
		"в роли два ключа, ничего не убрано": {func(d map[string]any) {
			d["keys"].(map[string]any)["t2"] = keyOf(t, extra)
			d["roles"].(map[string]any)["timestamp"] = role("t1", "t2")
		}, false},
	}
	for name, c := range cases {
		next := rotateWith(t, r1, c.mutate)
		if got := next.KeysRemovedSince(1, RoleTimestamp); got != c.want {
			t.Errorf("%s: KeysRemovedSince=%v, ожидалось %v", name, got, c.want)
		}
		if next.KeysRemovedSince(1, RoleManifest) {
			t.Errorf("%s: роль manifest не менялась", name)
		}
	}
	// из двух ключей остался один: удаление есть
	two := rotateWith(t, r1, cases["добавлен второй ключ timestamp"].mutate)
	var doc map[string]any
	_ = json.Unmarshal(r1.signed(t), &doc)
	doc["version"] = 3
	doc["keys"].(map[string]any)["t2"] = keyOf(t, extra)
	doc["roles"].(map[string]any)["timestamp"] = role("t2")
	raw, _ := json.Marshal(doc)
	three, err := two.VerifyRootUpdate(envelope(t, raw, r1.rootKeys))
	if err != nil {
		t.Fatal(err)
	}
	if !three.KeysRemovedSince(2, RoleTimestamp) {
		t.Error("ключ t1 убран из роли: счётчики сбрасываются")
	}
	if !three.KeysRemovedSince(1, RoleTimestamp) {
		t.Error("относительно v1 тоже убран")
	}
	if !three.KeysRemovedSince(77, RoleTimestamp) {
		t.Error("версии корня нет в истории — считаем, что ключ удалён")
	}
}

// Клиент: добавили второй ключ timestamp — откат версии timestamp по-прежнему отвергается.
func TestAddedKeyDoesNotResetCounters(t *testing.T) {
	e := newEnv(t, "0.5.0")
	extra := newSigner(t)
	next := rotateWith(t, e.def, func(d map[string]any) {
		d["keys"].(map[string]any)["t2"] = keyOf(t, extra)
		d["roles"].(map[string]any)["timestamp"] = role("t1", "t2")
	})
	e.src.put("2.root.json", next.Chain()[0])
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 2, tsVersion: 5})
	st := e.cl.State
	st.RootVersion, st.TimestampVersion, st.SerialStable = 1, 100, math.MaxInt64-1
	if _, err := e.check(t); !errors.Is(err, ErrSerialRollback) {
		t.Fatalf("старый ключ действует — защита от отката сохраняется: %v", err)
	}
	if st.TimestampVersion != 100 || st.ResetTimestamp || st.ResetSerials {
		t.Fatalf("счётчики не должны сбрасываться: %+v", *st)
	}
}
