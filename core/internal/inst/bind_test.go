// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

// T-55 (B1-36): занятый порт распознаётся по коду ошибки bind, а не только по
// тексту (текст локализуется и различается между ОС).
func TestIsAddrInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, bindErr := net.Listen("tcp", busy.Addr().String()) // тот же адрес: настоящая ошибка ОС
	if bindErr == nil {
		t.Fatal("второй Listen на занятый адрес удался")
	}

	wrapped := fmt.Errorf("inst: старт инстанса: %w",
		fmt.Errorf("singbox: старт: %w", fmt.Errorf("start inbound/mixed[mixed-in]: %w", bindErr)))
	if !IsAddrInUse(bindErr) || !IsAddrInUse(wrapped) {
		t.Fatalf("настоящая ошибка bind не распознана: %v", wrapped)
	}

	for _, text := range []string{
		"listen tcp 127.0.0.1:2080: bind: address already in use",
		"listen tcp 127.0.0.1:2080: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.",
	} {
		if !IsAddrInUse(errors.New(text)) {
			t.Errorf("текст не распознан: %q", text)
		}
	}
	for _, other := range []error{
		nil,
		errors.New("singbox: некорректный конфиг"),
		errors.New("dial tcp 127.0.0.1:1: connectex: No connection could be made because the target machine actively refused it."),
		errors.New("context deadline exceeded"),
	} {
		if IsAddrInUse(other) {
			t.Errorf("ложное срабатывание на %v", other)
		}
	}
}
