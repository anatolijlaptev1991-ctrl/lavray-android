// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// t50Handler — http.Handler со всеми сервисами (как в cmd/lavray-core, но без
// проверки токена: она в main).
func t50Handler(t *testing.T) http.Handler {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-t50-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return NewServer(Deps{Store: st, Runner: &inst.FakeRunner{}}).NewHandler()
}

func t50ImportContent(h http.Handler, contentBytes int) *httptest.ResponseRecorder {
	body := `{"content":"` + strings.Repeat("A", contentBytes) + `","previewOnly":true}`
	req := httptest.NewRequest(http.MethodPost, "/lavray.v1.ProfileService/ImportContent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// T-50 (A1-14): тело запроса RPC ограничено MaxRequestBytes. Раньше ядро
// читало сколько угодно: ImportContent на сотни МиБ раздувал память.
func TestRPCRejectsOversizedRequest(t *testing.T) {
	h := t50Handler(t)

	rec := t50ImportContent(h, MaxRequestBytes+1)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "resource_exhausted") {
		t.Fatalf("тело > %d МиБ: статус %d, ответ %.200s (ожидался 429 resource_exhausted)", MaxRequestBytes>>20, rec.Code, rec.Body.String())
	}

	// Запрос обычного размера лимитом не отвергается (ответ — любая другая
	// ошибка разбора, не resource_exhausted).
	rec = t50ImportContent(h, 1<<20)
	if strings.Contains(rec.Body.String(), "resource_exhausted") {
		t.Fatalf("запрос 1 МиБ отвергнут лимитом: %.200s", rec.Body.String())
	}
}
