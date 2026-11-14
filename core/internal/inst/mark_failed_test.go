// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"context"
	"testing"
)

func TestMarkFailedOnlyWhenIdleAndAllowsRestart(t *testing.T) {
	m := NewManager(&FakeRunner{})
	if !m.MarkFailed("нет прав") {
		t.Fatal("idle → MarkFailed должен сработать")
	}
	if st := m.Status(); st.State != StateCrashed || st.LastError != "нет прав" {
		t.Fatalf("статус: %+v", st)
	}
	if m.MarkFailed("ещё раз") {
		t.Fatal("из Crashed MarkFailed ничего не меняет")
	}
	if err := m.Start(context.Background(), RunInfo{ProfileID: 1, Mode: "system_proxy", ConfigJSON: []byte("{}")}); err != nil {
		t.Fatalf("из Crashed старт разрешён: %v", err)
	}
	if m.MarkFailed("x") {
		t.Fatal("при работающем инстансе MarkFailed запрещён")
	}
}
