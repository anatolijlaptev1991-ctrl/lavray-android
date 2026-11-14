// SPDX-License-Identifier: GPL-3.0-only

// Package updatestest — оснастка тестов: ключи, подписанный корень, релизы и
// источник в памяти для проверки подсистемы обновлений снаружи пакета updates
// (api, cmd). Только тесты: продовые пути пакет не импортируют.
package updatestest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// Source — источник метаданных и артефактов в памяти.
type Source struct {
	mu    sync.Mutex
	files map[string][]byte
	// Down — источник недоступен (любой запрос — ошибка сети).
	Down bool
	// Gate, если задан, блокирует открытие артефакта до закрытия канала или
	// отмены контекста (гонки загрузки). Opened закрывается при первом Open.
	Gate   chan struct{}
	Opened chan struct{}
	opened sync.Once
	// Requests — имена запрошенных файлов (метаданные и "open:<путь>").
	Requests []string
}

// NewSource — пустой источник.
func NewSource() *Source { return &Source{files: map[string][]byte{}, Opened: make(chan struct{})} }

// Put кладёт файл.
func (s *Source) Put(name string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = b
}

func (s *Source) lookup(name, tag string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, tag+name)
	if s.Down {
		return nil, fmt.Errorf("источник недоступен")
	}
	b, ok := s.files[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", updates.ErrNotFound, name)
	}
	return b, nil
}

// Fetch — updates.Source.
func (s *Source) Fetch(_ context.Context, name string) ([]byte, error) {
	b, err := s.lookup(name, "")
	return append([]byte(nil), b...), err
}

// Open — updates.ArtifactSource (поддерживает докачку).
func (s *Source) Open(ctx context.Context, name string, off int64) (*updates.ArtifactStream, error) {
	b, err := s.lookup(name, "open:")
	if err != nil {
		return nil, err
	}
	s.opened.Do(func() { close(s.Opened) })
	if s.Gate != nil {
		select {
		case <-s.Gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &updates.ArtifactStream{Body: io.NopCloser(bytes.NewReader(b[off:])), Start: off, Total: int64(len(b))}, nil
}
