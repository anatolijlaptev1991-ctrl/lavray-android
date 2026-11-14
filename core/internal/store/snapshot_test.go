// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// TestSnapshotExcludesUpdateKeys — S: update.* и кэши meta не попадают в
// копию ни строками, ни остатками в свободных страницах файла.
func TestSnapshotExcludesUpdateKeys(t *testing.T) {
	s := openTemp(t)
	seed(t, s, "A")
	for k, v := range map[string]string{
		"update.serial_stable": `7`,
		"update.last_url":      `"https://updates.example/UPDATE-URL-MARKER"`,
		"update.channel":       `"beta"`,
	} {
		if err := s.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetMeta("cache.geo", "CACHE-VALUE-MARKER"); err != nil {
		t.Fatal(err)
	}

	snap := snapshotOf(t, s)
	db := rawOpen(t, snap)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key LIKE 'update.%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("update.* в копии: %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM meta WHERE key LIKE 'cache.%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("cache.* в копии: %d, %v", n, err)
	}
	// остальное на месте
	for _, q := range []string{
		`SELECT COUNT(*) FROM settings WHERE key = 'inbound.auth.pass'`,
		`SELECT COUNT(*) FROM meta WHERE key = '` + MetaCustomPresets + `'`,
		`SELECT COUNT(*) FROM meta WHERE key LIKE 'store.last_id.%'`,
		`SELECT COUNT(*) FROM sessions`,
	} {
		if err := db.QueryRow(q).Scan(&n); err != nil || n == 0 {
			t.Fatalf("%s: %d, %v", q, n, err)
		}
	}
	// после DELETE файл пересобран VACUUM: следов исключённого в байтах нет
	_ = db.Close()
	raw, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"UPDATE-URL-MARKER", "update.serial_stable", "CACHE-VALUE-MARKER"} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Errorf("в файле копии остался %q", marker)
		}
	}
	// у самого хранилища исключённое не тронуто
	if v, ok, _ := s.GetSetting("update.serial_stable"); !ok || v != "7" {
		t.Errorf("Snapshot изменил живую БД: update.serial_stable = %q, %v", v, ok)
	}
}

func TestSnapshotNoSidecarFiles(t *testing.T) {
	s := openTemp(t)
	seed(t, s, "A")
	snap := snapshotOf(t, s)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(snap + suffix); err == nil {
			t.Errorf("рядом с копией остался %s", suffix)
		}
	}
	// файл — самодостаточная БД: первые байты SQLite, режим журнала не WAL
	raw, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) {
		t.Fatalf("не SQLite: %q", raw[:16])
	}
	if raw[18] == 2 || raw[19] == 2 {
		t.Error("копия в режиме WAL: её нельзя открыть без -wal")
	}
}

func TestSnapshotRefusesExistingTarget(t *testing.T) {
	s := openTemp(t)
	dst := filepath.Join(mkTempDir(t), "exists.db")
	if err := os.WriteFile(dst, []byte("занято"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(context.Background(), dst); err == nil {
		t.Fatal("Snapshot перезаписал существующий файл")
	}
	if got, _ := os.ReadFile(dst); string(got) != "занято" {
		t.Errorf("файл изменён: %q", got)
	}
}

func TestSnapshotCancelledContext(t *testing.T) {
	s := openTemp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dst := filepath.Join(mkTempDir(t), "cancelled.db")
	if err := s.Snapshot(ctx, dst); err == nil {
		t.Fatal("Snapshot с отменённым контекстом не вернул ошибку")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("после ошибки остался недописанный файл копии")
	}
}

// TestSnapshotConsistentUnderWrites — C: писатель кладёт пары профилей одной
// транзакцией; в любой копии, снятой во время записи, пар целое число, а база
// целостна.
func TestSnapshotConsistentUnderWrites(t *testing.T) {
	s := openTemp(t)
	g := &model.Group{Name: "pairs", Kind: model.GroupBasic}
	if err := s.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			err := s.InTx(func(tx *Store) error {
				for j := 0; j < 2; j++ {
					p := newProfile(g.ID, model.TypeTrojan, "p", "pair.example", 443)
					if err := tx.CreateProfile(p); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Errorf("писатель: %v", err)
				return
			}
		}
	}()
	for i := 0; i < 8; i++ {
		snap := snapshotOf(t, s)
		db := rawOpen(t, snap)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE group_id = ?`, g.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n%2 != 0 {
			t.Errorf("копия %d: нечётное число профилей %d — снимок разорвал транзакцию", i, n)
		}
		var res string
		if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
			t.Errorf("копия %d: integrity_check = %q, %v", i, res, err)
		}
		_ = db.Close()
	}
	close(stop)
	wg.Wait()
}

func TestInTxViewRejectsSnapshotAndRestore(t *testing.T) {
	s := openTemp(t)
	err := s.InTx(func(tx *Store) error {
		if e := tx.Snapshot(context.Background(), filepath.Join(os.TempDir(), "x.db")); e == nil {
			t.Error("Snapshot на InTx-представлении не отвергнут")
		}
		if e := tx.Restore(context.Background(), "x.db"); e == nil {
			t.Error("Restore на InTx-представлении не отвергнут")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
