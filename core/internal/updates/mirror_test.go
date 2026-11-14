// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Отказ закрыть цепочку корней называет молчащее зеркало: пользователю понятно, к
// чему не удалось подключиться, а не просто «проверка не удалась».
func TestRotationFailureNamesTheSilentMirror(t *testing.T) {
	r1 := newRootDef(t, 1, farFuture, "k1")
	start := mustTrust(t, r1.selfSigned(t))
	dead := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(dead.URL, "http://")
	dead.Close() // порт больше не слушается

	srcs := []Source{newMem(), &HTTPSource{Base: dead.URL}, newMem()}
	_, _, err := AdvanceRootWith(context.Background(), srcs, start, time.Now(), QueryPolicy{Timeout: 2 * time.Second})
	var me *MirrorError
	if !errors.As(err, &me) {
		t.Fatalf("ожидалась *MirrorError: %v", err)
	}
	if len(me.Hosts) != 1 || me.Hosts[0] != host {
		t.Fatalf("молчит одно зеркало %q, а названы %v", host, me.Hosts)
	}
	if !errors.Is(err, ErrNetwork) || !errors.Is(err, ErrMirrorUnreachable) {
		t.Fatalf("ошибка остаётся «проверка не удалась» и называет причину: %v", err)
	}
	if !strings.Contains(err.Error(), host) {
		t.Fatalf("текст ошибки без имени зеркала: %v", err)
	}
}

func TestSourceHostLabels(t *testing.T) {
	u, _ := url.Parse("https://cdn.jsdelivr.net/gh/x@gh-pages/meta")
	if got := sourceLabel(&HTTPSource{Base: u.String()}, 3); got != "cdn.jsdelivr.net" {
		t.Errorf("HTTP-источник: %q", got)
	}
	if got := sourceLabel(newMem(), 3); got != "источник №4" {
		t.Errorf("источник без адреса получает номер: %q", got)
	}
	if got := sourceLabel(&HTTPSource{Base: "::не url"}, 0); got != "источник №1" {
		t.Errorf("негодный адрес: %q", got)
	}
}
