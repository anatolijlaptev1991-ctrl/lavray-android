// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Пределы внутреннего контейнера.
const (
	// MaxSummaryBytes — предел JSON-сводки.
	MaxSummaryBytes = 64 << 10
	// MaxSnapshotBytes — предел снимка БД до сжатия (и при упаковке, и после
	// распаковки gzip). Один и тот же предел на обеих сторонах: копия, которую
	// удалось создать, всегда восстанавливается. Тело копии при этом не больше
	// 64 МиБ (MaxFileBytes), а SQLite сжимается хорошо.
	MaxSnapshotBytes = 128 << 20
)

var (
	errBodyTooLarge     = errors.New("тело копии больше предела")
	errSnapshotTooLarge = errors.New("снимок больше предела")
)

// bodyBuffer — буфер упаковки, который не растёт выше предела тела: несжимаемый
// снимок отвергается, как только сжатые данные перестают помещаться в файл.
type bodyBuffer struct {
	bytes.Buffer
	left int
}

func (b *bodyBuffer) Write(p []byte) (int, error) {
	if len(p) > b.left {
		return 0, errBodyTooLarge
	}
	b.left -= len(p)
	return b.Buffer.Write(p)
}

// Pack собирает тело под шифрование: u32 len ‖ summary JSON ‖ gzip(snapshot).
// Снимок читается потоком; больше MaxSnapshotBytes или тело, не помещающееся в
// файл копии, — ErrTooLarge.
func Pack(summary []byte, snapshot io.Reader) ([]byte, error) {
	return packLimit(summary, snapshot, MaxSnapshotBytes)
}

func packLimit(summary []byte, snapshot io.Reader, maxSnapshot int64) ([]byte, error) {
	if len(summary) > MaxSummaryBytes {
		return nil, fmt.Errorf("backup: сводка больше %d байт", MaxSummaryBytes)
	}
	buf := &bodyBuffer{left: MaxFileBytes - HeaderSize - TagSize}
	_, _ = buf.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(summary))))
	_, _ = buf.Write(summary)
	zw := gzip.NewWriter(buf)
	n, err := io.Copy(zw, io.LimitReader(snapshot, maxSnapshot+1))
	if err == nil && n > maxSnapshot {
		err = errSnapshotTooLarge
	}
	if err == nil {
		err = zw.Close()
	}
	switch {
	case errors.Is(err, errBodyTooLarge), errors.Is(err, errSnapshotTooLarge):
		return nil, fmt.Errorf("%w: %v", ErrTooLarge, err)
	case err != nil:
		return nil, fmt.Errorf("backup: gzip: %w", err)
	}
	return buf.Bytes(), nil
}

// capWriter пропускает в w не больше left байт, после чего отказывает. Ошибки
// самого w (например, диск полон) запоминаются отдельно: это не повреждение
// копии.
type capWriter struct {
	w    io.Writer
	left int64
	werr error
}

func (c *capWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > c.left {
		return 0, errSnapshotTooLarge
	}
	n, err := c.w.Write(p)
	c.left -= int64(n)
	if err != nil {
		c.werr = err
	}
	return n, err
}

// Unpack разбирает тело, расшифрованное Open: возвращает сводку, а снимок
// распаковывает потоком прямо в snapshot (временный файл). Больше
// MaxSnapshotBytes в snapshot не попадает ни байта. Любое отступление от
// формата — ErrCorrupt.
func Unpack(body []byte, snapshot io.Writer) (summary []byte, err error) {
	return unpackLimit(body, snapshot, MaxSnapshotBytes)
}

func unpackLimit(body []byte, snapshot io.Writer, maxSnapshot int64) ([]byte, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("%w: тело короче префикса длины", ErrCorrupt)
	}
	n := binary.LittleEndian.Uint32(body)
	if n > MaxSummaryBytes || int64(n) > int64(len(body)-4) {
		return nil, fmt.Errorf("%w: длина сводки %d", ErrCorrupt, n)
	}
	summary := body[4 : 4+n]
	rd := bytes.NewReader(body[4+n:])
	zr, err := gzip.NewReader(rd)
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrCorrupt, err)
	}
	zr.Multistream(false)
	cw := &capWriter{w: snapshot, left: maxSnapshot}
	if _, err := io.Copy(cw, zr); err != nil {
		if cw.werr != nil {
			return nil, fmt.Errorf("backup: запись снимка: %w", cw.werr)
		}
		return nil, fmt.Errorf("%w: gzip: %v", ErrCorrupt, err)
	}
	if rd.Len() != 0 {
		return nil, fmt.Errorf("%w: данные после gzip", ErrCorrupt)
	}
	return summary, nil
}
