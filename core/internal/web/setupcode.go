// SPDX-License-Identifier: GPL-3.0-only

package web

// setupcode.go — одноразовый код первичной настройки. Когда панель слушает не
// loopback, SetupFirstAdmin без кода открыл бы администратора первому, кто зашёл
// по сети (или странице, пересвязанной DNS на адрес роутера). Код лежит в файле
// каталога данных с правами 0600, поэтому доступен тому, у кого есть доступ к
// роутеру (SSH, LuCI), а не любому клиенту LAN. Код переживает перезапуск ядра и
// пропадает после создания администратора.

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// SetupCodeFile — имя файла с кодом в каталоге данных.
	SetupCodeFile = "web-setup-code"
	setupCodeLen  = 10
	// setupAlphabet без неоднозначных символов 0/O, 1/I/L.
	setupAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

// SetupCode — код первичной настройки, хранимый в файле.
type SetupCode struct {
	path string

	mu   sync.Mutex
	code string // пусто после Consume
}

// NewSetupCode читает код из dir или создаёт новый.
func NewSetupCode(dir string) (*SetupCode, error) {
	s := &SetupCode{path: filepath.Join(dir, SetupCodeFile)}
	if raw, err := os.ReadFile(s.path); err == nil {
		if c := normalizeCode(string(raw)); validCode(c) {
			s.code = c
			return s, nil
		}
	}
	code, err := randomCode()
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(s.path, []byte(code+"\n")); err != nil {
		return nil, fmt.Errorf("web: файл кода настройки: %w", err)
	}
	s.code = code
	return s, nil
}

// Code — текущий код ("" после Consume).
func (s *SetupCode) Code() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

// Path — файл с кодом.
func (s *SetupCode) Path() string { return s.path }

// Check сравнивает присланный код с ожидаемым за постоянное время. Регистр,
// пробелы и дефисы не важны.
func (s *SetupCode) Check(given string) bool {
	want := s.Code()
	if want == "" || len(given) > 64 {
		return false
	}
	got := normalizeCode(given)
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Consume удаляет код: администратор создан, код больше не нужен.
func (s *SetupCode) Consume() {
	s.mu.Lock()
	s.code = ""
	s.mu.Unlock()
	_ = os.Remove(s.path)
}

func normalizeCode(s string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\n", "", "\r", "", "\t", "").Replace(s))
}

func validCode(c string) bool {
	if len(c) != setupCodeLen {
		return false
	}
	return strings.Trim(c, setupAlphabet) == ""
}

func randomCode() (string, error) {
	b := make([]byte, setupCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("web: источник случайности недоступен: %w", err)
	}
	for i := range b {
		// 256 не делится на 31 нацело, но смещение при 10 символах несущественно
		// для одноразового кода с лимитером попыток.
		b[i] = setupAlphabet[int(b[i])%len(setupAlphabet)]
	}
	return string(b), nil
}
