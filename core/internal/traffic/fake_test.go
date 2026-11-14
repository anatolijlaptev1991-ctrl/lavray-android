// SPDX-License-Identifier: GPL-3.0-only

package traffic

import (
	"errors"
	"sync"
	"testing"
)

func TestProcessNameKeepsOnlyFileName(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\anatoly\AppData\Local\Chrome\chrome.exe`: "chrome.exe",
		"/usr/bin/curl":    "curl",
		"firefox.exe":      "firefox.exe",
		`C:\dir\sub\`:      "",
		"":                 "",
		`  C:\a b\c.exe  `: "c.exe",
		`\\server\share\x`: "x",
	} {
		if got := ProcessName(in); got != want {
			t.Errorf("ProcessName(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestFakeSourceLifecycle(t *testing.T) {
	f := NewFakeSource()
	events, cancel := f.Subscribe()
	f.Open(ConnInfo{ID: "a", Outbound: "proxy"})
	f.Open(ConnInfo{ID: "b", Outbound: "direct"})
	f.AddBytes("a", 10, 20)
	if up, down := f.Total(); up != 10 || down != 20 {
		t.Fatalf("Total = %d/%d", up, down)
	}
	if err := f.Close("a"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("повторное закрытие: %v", err)
	}
	if len(f.Active()) != 1 || len(f.Closed()) != 1 || f.Closed()[0].EndMs == 0 {
		t.Fatalf("списки: active=%v closed=%v", f.Active(), f.Closed())
	}
	if up, _ := f.Total(); up != 10 {
		t.Fatal("закрытое соединение пропало из Total")
	}
	if got := f.CloseCalls(); len(got) != 2 {
		t.Fatalf("вызовы Close: %v", got)
	}
	want := []Event{{EventOpened, "a"}, {EventOpened, "b"}, {EventClosed, "a"}}
	for _, w := range want {
		if got := <-events; got != w {
			t.Fatalf("событие %+v, ожидалось %+v", got, w)
		}
	}
	if f.Subscribers() != 1 {
		t.Fatal("подписка потеряна")
	}
	cancel()
	cancel() // повторная отмена безопасна
	if f.Subscribers() != 0 {
		t.Fatal("отмена не сняла подписку")
	}
	if _, open := <-events; open {
		t.Fatal("канал после отмены не закрыт")
	}
}

// Параллельные записи и чтения (проверяется под -race в CI).
func TestFakeSourceConcurrent(t *testing.T) {
	f := NewFakeSource()
	_, cancel := f.Subscribe()
	defer cancel()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := string(rune('a' + w))
			f.Open(ConnInfo{ID: id})
			for i := 0; i < 100; i++ {
				f.AddBytes(id, 1, 1)
				_ = f.Active()
				_, _ = f.Total()
			}
			_ = f.Close(id)
		}(w)
	}
	wg.Wait()
	if up, down := f.Total(); up != 800 || down != 800 {
		t.Fatalf("Total = %d/%d, ожидалось 800/800", up, down)
	}
}
