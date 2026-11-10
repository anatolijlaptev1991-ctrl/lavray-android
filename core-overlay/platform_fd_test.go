// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"errors"
	"net/netip"
	"testing"

	tun "github.com/sagernet/sing-tun"

	"github.com/sagernet/sing-box/option"
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

// openTunPlatform — Platform, у которого OpenTun отдаёт заданный дескриптор.
type openTunPlatform struct {
	fakePlatform
	fd      int32
	openErr error
	opened  int
}

func (p *openTunPlatform) OpenTun(string) (int32, error) {
	p.opened++
	return p.fd, p.openErr
}

func newOpenTestPlatform(plat Platform, nameErr error, closed *[]int) *boxPlatform {
	bp := newBoxPlatform(newHost(plat))
	bp.nameOf = func(int32) (string, error) { return "tun0", nameErr }
	bp.closeFn = func(fd int) { *closed = append(*closed, fd) }
	return bp
}

func testTunOptions() *tun.Options {
	return &tun.Options{
		Inet4Address: []netip.Prefix{netip.MustParsePrefix("172.19.0.1/30")},
		AutoRoute:    true,
		MTU:          1500,
	}
}

// Kotlin отдаёт Go независимый дескриптор. Если имя интерфейса узнать не удалось, дескриптор закрывается:
// иначе он утекал бы на каждую неудачную попытку подключения.
func TestOpenInterfaceClosesTheDescriptorWhenTheNameCannotBeRead(t *testing.T) {
	nameErr := errors.New("ioctl TUNGETIFF failed")
	plat := &openTunPlatform{fd: 77}
	var closed []int
	bp := newOpenTestPlatform(plat, nameErr, &closed)

	_, err := bp.OpenInterface(testTunOptions(), option.TunPlatformOptions{})
	if err == nil || !errors.Is(err, nameErr) {
		t.Fatalf("err = %v, ждали обёртку %v", err, nameErr)
	}
	if plat.opened != 1 {
		t.Fatalf("OpenTun вызван %d раз", plat.opened)
	}
	if len(closed) != 1 || closed[0] != 77 {
		t.Errorf("закрыты дескрипторы %v, ждали [77]", closed)
	}
}

func TestOpenInterfaceDoesNotCloseWhenOpenTunFails(t *testing.T) {
	plat := &openTunPlatform{fd: -1, openErr: errors.New("VPN permission is not granted")}
	var closed []int
	bp := newOpenTestPlatform(plat, nil, &closed)

	if _, err := bp.OpenInterface(testTunOptions(), option.TunPlatformOptions{}); err == nil {
		t.Fatal("ждали ошибку OpenTun")
	}
	if len(closed) != 0 {
		t.Errorf("закрыты %v: дескриптора не было", closed)
	}
}
