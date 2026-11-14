// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"slices"
	"testing"
)

// DeleteSettings удаляет существующие ключи, остальные не трогает и возвращает
// только те, у которых запись была.
func TestDeleteSettings(t *testing.T) {
	s := openTemp(t)
	for k, v := range map[string]string{"a": "1", "b": "2", "c": "3"} {
		if err := s.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.DeleteSettings([]string{"a", "missing", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("deleted = %v, want [a c]", got)
	}
	left, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left["b"] != "2" {
		t.Fatalf("осталось %v, want только b", left)
	}
	if again, err := s.DeleteSettings([]string{"a"}); err != nil || len(again) != 0 {
		t.Fatalf("повторное удаление: %v, %v", again, err)
	}
}
