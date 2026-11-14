// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

// S: конец цепочки ротации объявляется, только если КАЖДЫЙ источник ответил
// «нет файла»; молчащий источник мог бы скрывать новый корень (отзыв ключа).
func TestRotationFailsClosedWhenASourceIsSilent(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	start := mustTrust(t, r1.selfSigned(t))
	ctx := context.Background()
	ok, down, hung := newMem(), newMem(), newMem()
	down.down = true
	hung.delay = time.Minute
	pol := QueryPolicy{Timeout: 300 * time.Millisecond}

	if _, _, err := AdvanceRootWith(ctx, []Source{ok, down}, start, time.Now(), pol); !errors.Is(err, ErrNetwork) {
		t.Fatalf("один источник недоступен: ожидалась ErrNetwork, получено %v", err)
	}
	begin := time.Now()
	if _, _, err := AdvanceRootWith(ctx, []Source{ok, hung}, start, time.Now(), pol); !errors.Is(err, ErrNetwork) {
		t.Fatalf("один источник завис: ожидалась ErrNetwork, получено %v", err)
	}
	if time.Since(begin) > 3*time.Second {
		t.Fatalf("зависший источник держит проверку дольше предела фазы: %v", time.Since(begin))
	}
	if _, _, err := AdvanceRootWith(ctx, nil, start, time.Now(), pol); !errors.Is(err, ErrNetwork) {
		t.Fatalf("нет источников: %v", err)
	}
	// валидный корень от живого источника принимается, даже если соседний лежит
	r2 := newRootDef(t, 2, farFuture, "k2")
	ok.put("2.root.json", r2.rotatedFrom(t, r1))
	got, steps, err := AdvanceRootWith(ctx, []Source{down, ok}, start, time.Now(), pol)
	if err == nil && steps == 1 && got.Root().Version == 2 {
		// следующий шаг (v3) спросил бы down снова, поэтому здесь ожидается ошибка
		t.Fatal("после v2 цепочка не могла быть закончена при недоступном источнике")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("v2 принят, а конец цепочки не подтверждён: %v", err)
	}
}

func TestCheckFailsWhenRotationEndIsNotConfirmed(t *testing.T) {
	e := newEnv(t, "0.5.0")
	e.def.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 1})
	dead := newMem()
	dead.down = true
	e.cl.BootstrapSources = []Source{e.src, dead}
	e.cl.PhaseTimeout = time.Second
	if _, err := e.check(t); !errors.Is(err, ErrNetwork) {
		t.Fatalf("проверка при молчащем зеркале должна провалиться: %v", err)
	}
	if e.cl.State.LastCheckedAt != 0 {
		t.Fatal("провал не должен менять last_checked_at")
	}
}

// R7: после ротации, сменившей ключи timestamp и manifest, счётчики сбрасываются:
// утёкший ключ, подписавший MaxInt64, не запирает обновления навсегда.
func TestRotationResetsCountersOfRotatedRoles(t *testing.T) {
	e := newEnv(t, "0.5.0")
	r2 := newRootDef(t, 2, farFuture, "k2") // новые ключи timestamp и manifest
	e.src.put("2.root.json", r2.rotatedFrom(t, e.def))
	r2.publish(t, e.src, relOpts{version: "0.6.0", serial: 2, tsVersion: 5})
	st := e.cl.State
	st.RootVersion, st.TimestampVersion, st.SerialStable, st.SerialBeta = 1, math.MaxInt64, math.MaxInt64, math.MaxInt64
	res, err := e.check(t)
	if err != nil || !res.Availability.Available {
		t.Fatalf("после ротации ключей старые счётчики не должны блокировать: %v", err)
	}
	if st.TimestampVersion != 5 || st.SerialStable != 2 || st.SerialBeta != 0 || st.RootVersion != 2 {
		t.Fatalf("состояние после сброса: %+v", *st)
	}
	if !st.ResetTimestamp || !st.ResetSerials {
		t.Fatal("хранилищу нужно знать, что счётчики сброшены, а не взять максимум")
	}
}

// Ротация, не менявшая ключи timestamp и manifest, счётчики не трогает: защита
// от отката метаданных остаётся.
func TestRotationKeepsCountersWhenKeysUnchanged(t *testing.T) {
	e := newEnv(t, "0.5.0")
	r2 := newRootDef(t, 2, farFuture, "k2")
	r2.ts, r2.man, r2.art = e.def.ts, e.def.man, e.def.art
	e.src.put("2.root.json", r2.rotatedFrom(t, e.def))
	r2.publish(t, e.src, relOpts{version: "0.6.0", serial: 2, tsVersion: 5})
	st := e.cl.State
	st.RootVersion, st.TimestampVersion = 1, 100
	if _, err := e.check(t); !errors.Is(err, ErrSerialRollback) {
		t.Fatalf("ключи те же — откат timestamp 5 < 100 отвергается: %v", err)
	}
	if st.TimestampVersion != 100 || st.ResetTimestamp {
		t.Fatalf("счётчик не должен сбрасываться: %+v", *st)
	}
}

// Ротация, прошедшая в прошлом запуске, но не дошедшая до записи состояния,
// сбрасывает счётчики в следующем: признак — версия корня в состоянии.
func TestResetDetectedFromSavedRootVersion(t *testing.T) {
	e := newEnv(t, "0.5.0")
	r2 := newRootDef(t, 2, farFuture, "k2")
	store := &fakeRootStore{docs: map[int64][]byte{2: r2.rotatedFrom(t, e.def)}}
	e.cl.RootStore = store
	e.cl.Trust, _ = ResolveTrust(e.def.selfSigned(t), store) // цепочка уже на диске
	r2.publish(t, e.src, relOpts{version: "0.6.0", serial: 1, tsVersion: 2})
	e.cl.State.RootVersion, e.cl.State.TimestampVersion = 1, math.MaxInt64
	if _, err := e.check(t); err != nil {
		t.Fatalf("счётчики старого корня не должны переживать смену ключей: %v", err)
	}
}
