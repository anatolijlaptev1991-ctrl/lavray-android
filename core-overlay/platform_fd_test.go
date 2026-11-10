// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"errors"
	"testing"

	tun "github.com/sagernet/sing-tun"
)

func TestAdoptFd(t *testing.T) {
	boom := errors.New("tun.New failed")
	tests := []struct {
		name       string
		buildErr   error
		wantClosed bool
	}{
		{"успех: дескриптор у туннеля", nil, false},
		{"ошибка сборки: дескриптор закрыт", boom, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var closed []int
			_, err := adoptFd(42, func(fd int) { closed = append(closed, fd) }, func(fd int) (tun.Tun, error) {
				if fd != 42 {
					t.Errorf("build получил fd %d", fd)
				}
				return nil, tc.buildErr
			})
			if !errors.Is(err, tc.buildErr) {
				t.Fatalf("err = %v", err)
			}
			if got := len(closed) == 1 && closed[0] == 42; got != tc.wantClosed {
				t.Errorf("закрыт = %v (%v), ждали %v", got, closed, tc.wantClosed)
			}
		})
	}
}
