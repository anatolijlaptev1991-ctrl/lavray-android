// SPDX-License-Identifier: GPL-3.0-only

package api

// update_comp_hardening_test.go — правки по ревью безопасности §7.5: устаревшие
// данные не применяются, ошибки не несут путей, чтение staging ограничено,
// гео-набор не откатывается назад по ревизии.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

// geoRelease публикует релиз с гео-архивом ревизии rev.
func (f *updFix) geoRelease(serial, rev int64, extra string) {
	f.t.Helper()
	f.release(updatestest.Release{Version: "0.5.0", Serial: serial, TSVersion: serial, Geo: geoArchive(f.t, extra), GeoRev: rev})
}

// S: гео-набор более старой ревизии (стабильный канал после беты) не предлагается и не качается.
func TestGeoDowngradeIsNotOffered(t *testing.T) {
	f := geoFixture(t)
	f.geoRelease(1, 5, "from-beta")
	if _, err := f.downloadComps(compGeo); err != nil {
		t.Fatal(err)
	}
	if _, err := f.applyComps(compGeo); err != nil {
		t.Fatal(err)
	}
	f.geoRelease(2, 3, "older-stable") // манифест новее по serial, но гео старше по ревизии
	chk, err := f.check()
	if err != nil || offerFor(chk, compGeo) != nil {
		t.Fatalf("старая ревизия не предлагается: %v %v", offerFor(chk, compGeo), err)
	}
	if _, err := f.downloadComps(compGeo); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("нечего скачивать: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "from-beta.srs")); err != nil {
		t.Fatal("действующий набор не изменился:", err)
	}
	f.geoRelease(3, 6, "newer")
	if chk, err = f.check(); err != nil || offerFor(chk, compGeo) == nil {
		t.Fatalf("более новая ревизия предлагается: %v", err)
	}
}

// Устаревшие данные не применяются: serial скачанного манифеста ниже уже виденного,
// либо срок манифеста вышел. Долго живущее ядро шлюза не ставит вытесненное.
func TestApplyRefusesSupersededOrExpiredStagedData(t *testing.T) {
	t.Run("superseded_by_newer_serial", func(t *testing.T) {
		f := geoFixture(t)
		f.geoRelease(1, 1, "old")
		if _, err := f.downloadComps(compGeo); err != nil {
			t.Fatal(err)
		}
		f.geoRelease(2, 2, "new")
		if _, err := f.check(); err != nil { // последующая проверка видит serial 2
			t.Fatal(err)
		}
		evs, err := f.applyComps(compGeo)
		if err != nil || len(evs) != 1 || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "serial_rollback" {
			t.Fatalf("вытесненные данные: %v %v", evs, err)
		}
		if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "old.srs")); err == nil {
			t.Fatal("вытесненный набор применён")
		}
	})
	t.Run("expired_manifest", func(t *testing.T) {
		f := geoFixture(t)
		f.geoRelease(1, 1, "old")
		if _, err := f.downloadComps(compGeo); err != nil {
			t.Fatal(err)
		}
		f.srv.upd.mu.Lock()
		f.srv.upd.geo.staged.manifest.Expires = time.Now().Add(-time.Minute)
		f.srv.upd.mu.Unlock()
		evs, err := f.applyComps(compGeo)
		if err != nil || len(evs) != 1 || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "expired" {
			t.Fatalf("просроченный манифест: %v %v", evs, err)
		}
	})
}

// Ошибка занятого каталога не отдаёт путей пользователя; причина dir_busy на месте.
func TestDirBusyErrorHasNoPaths(t *testing.T) {
	secret := filepath.Join(os.TempDir(), "SECRET-user-dir", "geo")
	cause := &fs.PathError{Op: "rename", Path: secret, Err: errors.New("Access is denied")}
	f := newUpdFix(t, "0.5.0", nil)
	err := f.srv.dataFailure(compGeo, updates.NewDirBusyError(cause))
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeFailedPrecondition || ce.Meta().Get(UpdateReasonHeader) != ReasonDirBusy {
		t.Fatalf("ожидался FAILED_PRECONDITION dir_busy: %v", err)
	}
	if strings.Contains(ce.Message(), "SECRET") || strings.Contains(ce.Message(), os.TempDir()) {
		t.Fatalf("путь в сообщении: %q", ce.Message())
	}
	if st := f.compState(compGeo); strings.Contains(st.GetError(), "SECRET") {
		t.Fatalf("путь в ComponentState.error: %q", st.GetError())
	}
	// Подробности ушли в журнал.
	var found bool
	for _, l := range f.srv.logs.Tail(1000) {
		if strings.Contains(l.Message, "SECRET-user-dir") {
			found = true
		}
	}
	if !found {
		t.Fatal("подробности ошибки должны попасть в журнал")
	}
}

// Прочие сбои окружения при применении тоже отдаются без путей (INTERNAL с общим текстом).
func TestUnknownApplyErrorHasNoPaths(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	secret := filepath.Join(os.TempDir(), "SECRET-user-dir", "x.json")
	err := f.srv.dataFailure(compPresets, &fs.PathError{Op: "open", Path: secret, Err: errors.New("denied")})
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("неизвестный сбой: %v", err)
	}
}

// readStaged ограничен и по размеру, и по тексту ошибки.
func TestReadStagedIsBoundedAndPathless(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	dir := f.srv.compStagingDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)
	p := filepath.Join(dir, sha+"-big.json")
	if err := os.WriteFile(p, make([]byte, 2<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.readStaged(stagedFile{path: p, sha256: sha}, 1<<10); !errors.Is(err, updates.ErrTooLarge) {
		t.Fatalf("файл больше предела: %v", err)
	}
	if data, err := f.srv.readStaged(stagedFile{path: p, sha256: sha}, 2<<10); err != nil || len(data) != 2<<10 {
		t.Fatalf("ровно предел: %d %v", len(data), err)
	}
	missing := filepath.Join(dir, sha+"-gone.json")
	_, err := f.srv.readStaged(stagedFile{path: missing}, 1<<10)
	if !errors.Is(err, updates.ErrNotStaged) || strings.Contains(err.Error(), dir) {
		t.Fatalf("нет файла: %v", err)
	}
}

// Отказ применения не оставляет в состоянии компонента текста с путями.
func TestComponentErrorTextIsNotRaw(t *testing.T) {
	f := geoFixture(t)
	f.geoRelease(1, 1, "old")
	if _, err := f.downloadComps(compGeo); err != nil {
		t.Fatal(err)
	}
	// Файл в staging пропал: применение сообщает not_staged без путей.
	st := f.srv.upd.stagedOf(compGeo)
	_ = os.Remove(st.files[0].path)
	evs, err := f.applyComps(compGeo)
	if err != nil || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "not_staged" {
		t.Fatalf("нет файла в staging: %v %v", evs, err)
	}
	cs := f.compState(compGeo)
	if cs.Phase != lavrayv1.ComponentState_PHASE_FAILED || strings.ContainsAny(cs.GetError(), `\/:`) {
		t.Fatalf("состояние: %+v", cs)
	}
}
