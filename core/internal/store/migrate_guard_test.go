// SPDX-License-Identifier: GPL-3.0-only

package store

// migrate_guard_test.go — миграции только добавляются. sha256 текста каждой
// миграции сверяется со снимком testdata/migrations.sha256; уже выпущенную
// миграцию править нельзя: у пользователей она применена, правка до них не
// доедет (см. L19 в migrate.go) и схемы разойдутся.
//
// Как добавить миграцию: допишите migration{version: N+1, ...} в конец
// migrations, запустите этот тест — он напечатает готовую строку
// `N+1 <sha256>`; допишите её в конец testdata/migrations.sha256. Существующие
// строки не меняются. Если тест падает на старой версии, не правьте снимок, а
// верните миграцию и сделайте новую версию с нужным изменением.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

const migrationsSnapshot = "testdata/migrations.sha256"

// migrationHash — sha256 текста миграции. Каждая инструкция входит с нулевым
// байтом после неё, чтобы перенос границы между инструкциями менял хэш. Текст
// берётся таким, каким его получит SQLite (константы вроде DefaultGroupID уже
// подставлены).
func migrationHash(m migration) string {
	h := sha256.New()
	for _, stmt := range m.stmts {
		h.Write([]byte(stmt))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// readMigrationSnapshot читает «<версия> <sha256>» построчно; пустые строки и
// строки с # пропускаются.
func readMigrationSnapshot(t *testing.T) map[int64]string {
	t.Helper()
	f, err := os.Open(migrationsSnapshot)
	if err != nil {
		t.Fatalf("снимок миграций: %v", err)
	}
	defer func() { _ = f.Close() }()
	out := map[int64]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s:%d: ждём «<версия> <sha256>», получили %q", migrationsSnapshot, n, line)
		}
		version, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			t.Fatalf("%s:%d: версия %q: %v", migrationsSnapshot, n, fields[0], err)
		}
		if _, dup := out[version]; dup {
			t.Fatalf("%s:%d: версия %d указана дважды", migrationsSnapshot, n, version)
		}
		out[version] = fields[1]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigrationsAreAppendOnly(t *testing.T) {
	snapshot := readMigrationSnapshot(t)
	inCode := map[int64]bool{}
	for i, m := range migrations {
		if m.version != int64(i+1) {
			t.Errorf("migrations[%d].version = %d, ждём %d: версии идут подряд с 1", i, m.version, i+1)
		}
		inCode[m.version] = true
		got := migrationHash(m)
		want, ok := snapshot[m.version]
		switch {
		case !ok:
			t.Errorf("у миграции %d нет строки в %s; допишите в конец файла:\n%d %s",
				m.version, migrationsSnapshot, m.version, got)
		case want != got:
			t.Errorf("миграция %d изменена (sha256 %s, в снимке %s): миграции только добавляются, "+
				"верните текст и сделайте новую версию", m.version, got, want)
		}
	}
	for version := range snapshot {
		if !inCode[version] {
			t.Errorf("в %s есть версия %d, а миграции с таким номером нет: миграции не удаляют", migrationsSnapshot, version)
		}
	}
}
