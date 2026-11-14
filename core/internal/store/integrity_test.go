// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// T-50 (A1-15, FR-UI-260): мусор вместо SQLite → ErrCorrupt с путём к файлу;
// файл не изменён, не переименован, не удалён.
func TestOpenGarbageFileIsCorruptAndUntouched(t *testing.T) {
	dir := mkTempDir(t)
	path := DBPath(dir)
	garbage := []byte("this is not a sqlite database, garbage garbage garbage garbage")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err == nil {
		_ = s.Close()
		t.Fatal("Open принял мусорный файл вместо базы данных")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ожидался ErrCorrupt, получено: %v", err)
	}
	var ce *CorruptError
	if !errors.As(err, &ce) || ce.Path != path || ce.Cause == nil {
		t.Fatalf("CorruptError с путём %s не получен: %#v", path, ce)
	}
	after, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("файл БД пропал после неудачного Open: %v", rerr)
	}
	if !bytes.Equal(after, garbage) {
		t.Fatal("Open изменил повреждённый файл")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), "corrupt") || strings.HasSuffix(e.Name(), ".bak") {
			t.Errorf("Open не должен переименовывать или копировать файл сам: %s", e.Name())
		}
	}
}

// Целый заголовок, но побитая страница: раньше Open проходил, а ошибки
// «database disk image is malformed» всплывали позже, в случайных RPC.
func TestOpenDamagedPageIsCorrupt(t *testing.T) {
	dir := mkTempDir(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := newGroup("g", model.GroupBasic)
	if err := s.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		if err := s.CreateProfile(newProfile(g.ID, model.TypeSOCKS, "n", "1.2.3.4", 1000+i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	path := DBPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const pageSize = 4096 // modernc.org/sqlite: страница по умолчанию
	if len(data) < 8*pageSize {
		t.Fatalf("база слишком мала для опыта: %d байт", len(data))
	}
	damaged := append([]byte(nil), data...)
	copy(damaged[2*pageSize:3*pageSize], bytes.Repeat([]byte{0xFF}, pageSize))
	if err := os.WriteFile(path, damaged, 0o600); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err == nil {
		_ = s2.Close()
		t.Fatal("Open принял базу с повреждённой страницей")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ожидался ErrCorrupt, получено: %v", err)
	}
	after, _ := os.ReadFile(path)
	// Первые 100 байт — заголовок: SQLite переключает режим журнала и счётчик
	// изменений; страницы данных обязаны остаться как были.
	if !bytes.Equal(after[100:], damaged[100:]) {
		t.Fatal("Open изменил страницы повреждённой базы")
	}
}

// T-50 (A1-15): БД более новой схемы отвергается и не изменяется.
func TestOpenNewerSchemaIsRejectedWithoutWriting(t *testing.T) {
	dir := mkTempDir(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	latest := migrations[len(migrations)-1].version
	future := latest + 97
	if _, err := s.DB().Exec(`INSERT INTO schema_version (version, applied_at_ms) VALUES (?, 0)`, future); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err == nil {
		_ = s2.Close()
		t.Fatal("Open принял базу из будущей версии схемы")
	}
	var tooNew *SchemaTooNewError
	if !errors.As(err, &tooNew) || tooNew.Found != future || tooNew.Supported != latest {
		t.Fatalf("ожидался SchemaTooNewError{Found:%d, Supported:%d}, получено: %v", future, latest, err)
	}
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("errors.Is(err, ErrSchemaTooNew) = false: %v", err)
	}

	raw, err := sql.Open("sqlite", DBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var maxVer, rows int64
	if err := raw.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_version`).Scan(&maxVer, &rows); err != nil {
		t.Fatal(err)
	}
	if maxVer != future || rows != int64(len(migrations))+1 {
		t.Fatalf("schema_version изменена: max=%d строк=%d (ожидалось %d и %d)", maxVer, rows, future, len(migrations)+1)
	}
}
