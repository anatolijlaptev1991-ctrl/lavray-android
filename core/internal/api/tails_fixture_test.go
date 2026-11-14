// SPDX-License-Identifier: GPL-3.0-only

package api

// tails_fixture_test.go — общая оснастка тестов «хвостов A» (очистка, дубликаты,
// резолв, экспорт, запоминание): профили в группе фикстуры и вызов обработчиков
// напрямую, без сети.

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// addTrojan сохраняет trojan-профиль в группу 1; password отличает серверы.
func (f *svcFixture) addTrojan(t *testing.T, name, addr string, port int, password string) *model.Profile {
	t.Helper()
	p := &model.Profile{GroupID: 1, Type: model.TypeTrojan, Name: name, Address: addr, Port: port,
		Detail: &model.TrojanVlessDetail{Password: password}}
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// addChain сохраняет цепочку из профилей ids в группу 1.
func (f *svcFixture) addChain(t *testing.T, name string, ids ...int64) *model.Profile {
	t.Helper()
	p := &model.Profile{GroupID: 1, Type: model.TypeChain, Name: name,
		Detail: &model.ChainDetail{ProfileIDs: ids}}
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// setLatency записывает результат теста профиля (меньше нуля — недоступен).
func (f *svcFixture) setLatency(t *testing.T, id int64, ms int32) {
	t.Helper()
	if err := f.srv.st.SaveTestResult(id, model.TestResult{LatencyMs: &ms}); err != nil {
		t.Fatal(err)
	}
}

// activate «подключает» профиль: менеджер запущен с этим ProfileID.
func (f *svcFixture) activate(t *testing.T, id int64) {
	t.Helper()
	err := f.srv.inst.Start(context.Background(), inst.RunInfo{ProfileID: id, GroupID: 1, Mode: "system_proxy", ConfigJSON: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
}

// ids возвращает id профилей группы 1 в каноническом порядке.
func (f *svcFixture) ids(t *testing.T) []int64 {
	t.Helper()
	list, err := f.srv.st.ListProfiles(1)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int64, 0, len(list))
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

// newReq — запрос Connect для вызова обработчика напрямую.
func newReq[T any](msg *T) *connect.Request[T] { return connect.NewRequest(msg) }

// sameIDs: списки равны как последовательности.
func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
