// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetupCodeCreatedAndChecked(t *testing.T) {
	dir := t.TempDir()
	sc, err := NewSetupCode(dir)
	if err != nil {
		t.Fatal(err)
	}
	code := sc.Code()
	if len(code) != setupCodeLen || strings.ContainsAny(code, "01OIL") {
		t.Fatalf("код %q: длина или неоднозначные символы", code)
	}
	if sc.Path() != filepath.Join(dir, SetupCodeFile) {
		t.Fatalf("путь %q", sc.Path())
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(sc.Path()); st.Mode().Perm() != 0o600 {
			t.Fatalf("права файла %v, ждали 0600", st.Mode().Perm())
		}
	}
	for _, ok := range []string{code, strings.ToLower(code), " " + code + " ", code[:5] + "-" + code[5:]} {
		if !sc.Check(ok) {
			t.Errorf("верный код %q не принят", ok)
		}
	}
	for _, bad := range []string{"", "x", code[:len(code)-1], code + "A", strings.Repeat("A", 4096)} {
		if sc.Check(bad) {
			t.Errorf("неверный код %q принят", bad)
		}
	}
}

// TestSetupCodeSurvivesRestartAndIsConsumed — код переживает перезапуск ядра,
// пока администратор не создан, и исчезает после (D).
func TestSetupCodeSurvivesRestartAndIsConsumed(t *testing.T) {
	dir := t.TempDir()
	first, _ := NewSetupCode(dir)
	again, err := NewSetupCode(dir)
	if err != nil || again.Code() != first.Code() {
		t.Fatalf("после перезапуска код сменился: %v", err)
	}
	again.Consume()
	if _, err := os.Stat(first.Path()); !os.IsNotExist(err) {
		t.Fatal("файл кода остался после Consume")
	}
	if again.Check(first.Code()) {
		t.Fatal("потреблённый код всё ещё принимается")
	}
	again.Consume() // повтор безопасен
}

func TestSetupCodeReplacesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SetupCodeFile), []byte("слишком коротко"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := NewSetupCode(dir)
	if err != nil || len(sc.Code()) != setupCodeLen {
		t.Fatalf("испорченный файл не заменён: %v %q", err, sc.Code())
	}
}
