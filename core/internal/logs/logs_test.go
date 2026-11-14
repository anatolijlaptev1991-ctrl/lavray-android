// SPDX-License-Identifier: GPL-3.0-only

package logs

import (
	"strings"
	"testing"
	"time"
)

// LNB-01/28, TEST-PLAN набор 6: «секрета нет ни в одной строке» — прогон
// набора утечек через Mux (запись → ring + подписка) и через Redact напрямую.
func TestRedactSecretsAbsentEverywhere(t *testing.T) {
	const (
		secretPass   = "Sup3r-Secret-Passw0rd!"
		secretToken  = "tok-9f8e7d6c5b4a"
		secretUUID   = "b831381d-6324-4d53-ad4f-8cda05ad21b1"
		secretB64    = "eyJhZGQiOiJleGFtcGxlLmNvbSIsImlkIjoiYjgzMTM4MWQtNjMyNC00ZDUzLWFkNGYtOGNkYTA1YWQyMWIxIn0="
		secretEnv    = "hunter2-env-pass"
		secretBearer = "Bearer-xyz-abc-123"
	)
	inputs := []string{
		"подписка https://user:" + secretPass + "@sub.example.com/api?u=1",
		"обновление: GET https://sub.example.com/api?token=" + secretToken + "&x=2",
		"конфиг: {\"password\":\"" + secretPass + "\",\"uuid\":\"" + secretUUID + "\"}",
		"ссылка vmess://" + secretB64 + " импортирована",
		"env ядра: SUB_PASSWORD=" + secretEnv + " PATH=/usr/bin",
		"заголовок Authorization: Bearer " + secretBearer,
		"happ://ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 расшифрована",
	}

	m := NewMux(64)
	ch, cancel, _ := m.Subscribe(64, 0, nil)
	defer cancel()
	for i, in := range inputs {
		m.Logf(LevelInfo, SourceSubs, "строка %d: %s", i, in)
	}

	check := func(t *testing.T, where, s string) {
		t.Helper()
		for _, secret := range []string{secretPass, secretToken, secretUUID, secretB64, secretEnv, secretBearer, "Bearer "} {
			if secret == "Bearer " {
				continue
			}
			if strings.Contains(s, secret) {
				t.Fatalf("%s: секрет %q утёк в строку %q", where, secret, s)
			}
		}
	}
	for _, l := range m.Tail(0) {
		check(t, "ring", l.Message)
	}
	timeout := time.After(2 * time.Second)
	for i := 0; i < len(inputs); i++ {
		select {
		case l := <-ch:
			check(t, "подписка", l.Message)
		case <-timeout:
			t.Fatalf("подписка: пришло %d строк из %d", i, len(inputs))
		}
	}
	// Прямой Redact — те же инварианты.
	for _, in := range inputs {
		check(t, "Redact", Redact(in))
	}
}

func TestRedactKeepsUsefulInfo(t *testing.T) {
	cases := []struct{ in, contains string }{
		{"GET https://sub.example.com/api?token=x → 200", "sub.example.com/api"},
		{"GET https://sub.example.com/api?token=x → 200", "[REDACTED]"},
		{"профиль example.com:443 сохранён", "example.com:443"},
		{"порт 2080 занят", "2080"},
	}
	for _, c := range cases {
		out := Redact(c.in)
		if !strings.Contains(out, c.contains) {
			t.Errorf("Redact(%q) = %q — потеряно %q", c.in, out, c.contains)
		}
	}
	// повторное применение идемпотентно (redact(redact(x)) == redact(x))
	once := Redact("vmess://abc?token=zz и https://u:p@h/x")
	if twice := Redact(once); twice != once {
		t.Errorf("Redact не идемпотентен: %q vs %q", once, twice)
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[31mERROR\x1b[0m что-то \x1b[1;32mслучилось\x1b[m"
	want := "ERROR что-то случилось"
	if got := StripANSI(in); got != want {
		t.Errorf("StripANSI = %q, хочу %q", got, want)
	}
}

func TestMuxRingAndTail(t *testing.T) {
	m := NewMux(4)
	for i := 0; i < 10; i++ {
		m.Logf(LevelInfo, SourceCore, "строка %d", i)
	}
	tail := m.Tail(3)
	if len(tail) != 3 {
		t.Fatalf("Tail(3) = %d строк, хочу 3", len(tail))
	}
	if tail[0].Message != "строка 7" || tail[2].Message != "строка 9" {
		t.Errorf("Tail(3) = %v (последние строки 7..9)", tail)
	}
	all := m.Tail(0)
	if len(all) != 4 {
		t.Errorf("ring должен держать 4 строки, в нём %d", len(all))
	}
}

func TestMuxFilterAndSlowSubscriber(t *testing.T) {
	m := NewMux(8)
	ch, cancel, _ := m.Subscribe(4, 0, func(l Line) bool { return l.Source == SourceSubs })
	defer cancel()
	m.Logf(LevelInfo, SourceCore, "не проходит фильтр")
	m.Logf(LevelWarn, SourceSubs, "проходит 1")
	m.Logf(LevelInfo, SourceSubs, "проходит 2")
	// медленный подписчик: буфер 4 не переполнится здесь; проверяем что
	// доставка не блокирует писателя — 100 сообщений подряд без зависания
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			m.Logf(LevelInfo, SourceSubs, "шторм %d", i)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Logf заблокировался на подписчике")
	}
	// из шторма дошло не больше буфера — и это не ошибка
	for read := 0; read < 4; read++ {
		select {
		case <-ch:
		default:
			t.Fatalf("в канале меньше 4 сообщений после шторма (прочитано %d)", read)
		}
	}
}

// L4 аудита №1: ANSI-последовательность внутри секрета не должна обходить
// редакцию. Раньше StripANSI шёл ПОСЛЕ Redact, и цветовой код разрывал ключ.
func TestRedactSurvivesAnsiInsideSecret(t *testing.T) {
	m := NewMux(10)
	m.Logf("info", "core", "PASS\x1b[31mWORD=hunter2 tok\x1b[0men=abcdef")
	tail := m.Tail(1)
	if len(tail) != 1 {
		t.Fatalf("tail=%d", len(tail))
	}
	msg := tail[0].Message
	if strings.Contains(msg, "hunter2") || strings.Contains(msg, "abcdef") {
		t.Fatalf("секрет утёк в журнал: %q", msg)
	}
}
