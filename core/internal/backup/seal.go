// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Ошибки пакета; вызывающий сопоставляет их errors.Is.
var (
	// ErrWrongPassword — keycheck не сошёлся: пароль неверен (либо изменены
	// соль или параметры, что снаружи неотличимо).
	ErrWrongPassword = errors.New("backup: неверный пароль")
	// ErrTampered — пароль верен, но тег подлинности не сошёлся: файл изменён
	// или обрезан.
	ErrTampered = errors.New("backup: файл изменён или повреждён")
	// ErrCorrupt — это не файл LAVBK1: нет сигнатуры, неверный заголовок,
	// файл короче минимума, битый контейнер внутри.
	ErrCorrupt = errors.New("backup: не файл резервной копии или контейнер повреждён")
	// ErrEmptyPassword — пустой пароль.
	ErrEmptyPassword = errors.New("backup: пустой пароль")
	// ErrTooLarge — файл или тело больше MaxFileBytes.
	ErrTooLarge = errors.New("backup: размер копии больше предела")
)

// MaxFileBytes — предел размера файла копии (и шифруемого тела): 64 МиБ.
const MaxFileBytes = 64 << 20

// Options — настройки Seal. Нулевое значение даёт боевые параметры
// (DefaultParams) и crypto/rand.
type Options struct {
	Params Params    // нулевые → DefaultParams
	Rand   io.Reader // источник соли и nonce; nil → crypto/rand
}

// Seal шифрует payload паролем и возвращает файл LAVBK1. Пароль берётся как
// есть (байты UTF-8, без нормализации); приложению нужен SealPassword.
func Seal(password string, payload []byte, opts Options) ([]byte, error) {
	if password == "" {
		return nil, ErrEmptyPassword
	}
	if opts.Params == (Params{}) {
		opts.Params = DefaultParams()
	}
	if err := opts.Params.Validate(); err != nil {
		return nil, fmt.Errorf("backup: параметры: %w", err)
	}
	if HeaderSize+len(payload)+TagSize > MaxFileBytes {
		return nil, ErrTooLarge
	}
	rnd := opts.Rand
	if rnd == nil {
		rnd = rand.Reader
	}
	h := Header{Params: opts.Params}
	if _, err := io.ReadFull(rnd, h.Salt[:]); err != nil {
		return nil, fmt.Errorf("backup: соль: %w", err)
	}
	if _, err := io.ReadFull(rnd, h.Nonce[:]); err != nil {
		return nil, fmt.Errorf("backup: nonce: %w", err)
	}
	key := DeriveKey(password, h.Salt, h.Params)
	defer wipe(key)
	h.KeyCheck = keyCheck(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("backup: шифр: %w", err)
	}
	header := h.Marshal()
	out := make([]byte, 0, HeaderSize+len(payload)+TagSize)
	out = append(out, header...)
	return aead.Seal(out, h.Nonce[:], payload, header), nil
}

// Open проверяет пароль и подлинность файла и возвращает открытое тело.
// Пароль берётся как есть (без нормализации); приложению нужен OpenPassword.
// Порядок ошибок: размер → заголовок (ErrCorrupt) → пароль (ErrWrongPassword)
// → подлинность (ErrTampered). Дорогой Argon2id запускается только после
// того, как заголовок прошёл строгую проверку границ.
func Open(password string, file []byte) ([]byte, error) {
	if len(file) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	h, err := ParseHeader(file)
	if err != nil {
		return nil, err
	}
	if len(file) < HeaderSize+TagSize {
		return nil, fmt.Errorf("%w: файл короче заголовка с тегом", ErrCorrupt)
	}
	if password == "" {
		return nil, ErrEmptyPassword
	}
	key := DeriveKey(password, h.Salt, h.Params)
	defer wipe(key)
	if want := keyCheck(key); !hmac.Equal(want[:], h.KeyCheck[:]) {
		return nil, ErrWrongPassword
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("backup: шифр: %w", err)
	}
	plain, err := aead.Open(nil, h.Nonce[:], file[HeaderSize:], file[:HeaderSize])
	if err != nil {
		return nil, ErrTampered
	}
	return plain, nil
}
