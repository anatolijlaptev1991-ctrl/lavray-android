// SPDX-License-Identifier: GPL-3.0-only

// source.go — абстракции источников: метаданные (Source) и потоковые
// артефакты с докачкой (ArtifactSource). Прод: HTTPSource; тесты: память.
package updates

import (
	"context"
	"io"
)

// Source — откуда тянут метаданные (интерфейс для тестов; прод: HTTPSource
// поверх источников из sources.json и bootstrap-списка сборки).
type Source interface {
	// Fetch возвращает тело по имени (например "timestamp.json") либо ошибку.
	// Превышение лимита — ErrTooLarge, а не молчаливое усечение; 404 — ErrNotFound.
	Fetch(ctx context.Context, name string) ([]byte, error)
}

// ArtifactStream — открытый поток артефакта. Start — смещение, с которого
// сервер реально отдаёт тело (0, если Range проигнорирован); Total — полная
// длина файла, если известна (-1 — неизвестна).
type ArtifactStream struct {
	Body  io.ReadCloser
	Start int64
	Total int64
}

// ArtifactSource — источник артефактов с докачкой (HTTP Range).
type ArtifactSource interface {
	// Open открывает артефакт с запрошенного смещения offset (0 — с начала).
	Open(ctx context.Context, name string, offset int64) (*ArtifactStream, error)
}
