// SPDX-License-Identifier: GPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner запоминает скрипты, которые получил бы `nft -f -`. nft в тестах
// не запускается.
type fakeRunner struct {
	scripts []string
	err     error
	block   bool // ждать ctx (проверка предела по времени)
}

func (f *fakeRunner) Run(ctx context.Context, script string) error {
	f.scripts = append(f.scripts, script)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

func TestNftApplierApplyAndRemove(t *testing.T) {
	r := &fakeRunner{}
	a := &NftApplier{Runner: r}
	script := NftRenderer{}.Render(baseCfg(), nil, "")
	if err := a.Apply(script); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(); err != nil {
		t.Fatal(err)
	}
	if len(r.scripts) != 2 || r.scripts[0] != script {
		t.Fatalf("Apply должен передать скрипт как есть: %d вызовов", len(r.scripts))
	}
	rm := r.scripts[1]
	if !strings.Contains(rm, "delete table inet lavray") || !strings.HasPrefix(rm, "table inet lavray\n") {
		t.Fatalf("скрипт снятия: %q", rm)
	}
}

func TestNftApplierPropagatesErrors(t *testing.T) {
	boom := errors.New("Error: syntax error")
	a := &NftApplier{Runner: &fakeRunner{err: boom}}
	if err := a.Apply("x"); !errors.Is(err, boom) {
		t.Fatalf("Apply: %v", err)
	}
	if err := a.Remove(); !errors.Is(err, boom) {
		t.Fatalf("Remove: %v", err)
	}
}

// TestNftApplierTimeout — зависший nft не блокирует шлюз навсегда (X).
func TestNftApplierTimeout(t *testing.T) {
	a := &NftApplier{Runner: &fakeRunner{block: true}, Timeout: 20 * time.Millisecond}
	start := time.Now()
	err := a.Apply("x")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v за %v", err, time.Since(start))
	}
}

// TestExecRunnerMissingBinary — отсутствие nft даёт ошибку, а не панику. Имя
// заведомо несуществующее: настоящий nft тест не запускает.
func TestExecRunnerMissingBinary(t *testing.T) {
	err := ExecRunner{Bin: "lavray-no-such-nft-binary"}.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "nft:") {
		t.Fatalf("err=%v", err)
	}
}

// TestActiveFileForFw4 — применённый набор лежит в файле, который подхватит
// include fw4; снятие и сбой nft убирают файл.
func TestActiveFileForFw4(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state", "active", "lavray.nft")
	r := &fakeRunner{}
	a := &NftApplier{Runner: r, ActiveFile: file}

	if err := a.Apply("table inet lavray {}\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "table inet lavray {}\n" {
		t.Fatalf("файл правил: %q err=%v", got, err)
	}
	if err := a.Apply("table inet lavray { # v2\n}\n"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); !strings.Contains(string(got), "v2") {
		t.Fatalf("файл не заменён: %q", got)
	}
	if err := a.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("после Remove файл остался: %v", err)
	}
	if err := a.Remove(); err != nil {
		t.Fatalf("повторный Remove: %v", err)
	}

	r.err = errors.New("nft: syntax error")
	if err := a.Apply("плохой скрипт"); err == nil {
		t.Fatal("сбой nft не возвращён")
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("после сбоя nft файл для fw4 остался")
	}
}

// TestActiveFileWriteFailureBlocksApply — не записали файл: nft не вызывается.
func TestActiveFileWriteFailureBlocksApply(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	a := &NftApplier{Runner: r, ActiveFile: filepath.Join(blocker, "active", "lavray.nft")} // родитель — файл
	if err := a.Apply("x"); err == nil {
		t.Fatal("ошибка записи файла проглочена")
	}
	if len(r.scripts) != 0 {
		t.Fatal("nft вызван, хотя файл для fw4 не записан")
	}
}
