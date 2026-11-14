// SPDX-License-Identifier: GPL-3.0-only

// component_fetch.go — загрузка небольших артефактов компонентов (пресеты, гео-
// архив): целиком в память, с жёстким пределом размера, сверкой длины и sha256 из
// подписанного манифеста. Артефакт, не прошедший сверку, не принимается ни с
// одного зеркала; сетевой сбой переходит к следующему зеркалу.
package updates

import (
	"context"
	"fmt"
	"io"
)

// BlobSpec — что скачать и чем сверить (всё берётся из проверенного манифеста).
type BlobSpec struct {
	Path   string
	Sha256 string
	Length int64 // >0 — точная длина; 0 — длина неизвестна, действует Max
	Max    int64 // предел размера, когда Length неизвестна
}

func (s BlobSpec) limit() int64 {
	if s.Length > 0 {
		return s.Length
	}
	return s.Max
}

// FetchBlob скачивает артефакт с первого зеркала, чьи байты прошли сверку.
// progress (может быть nil) получает done и total (total=0 — длина неизвестна).
func FetchBlob(ctx context.Context, mirrors []ArtifactSource, spec BlobSpec, progress func(done, total int64)) ([]byte, error) {
	var last error
	for _, m := range mirrors {
		data, err := fetchBlobFrom(ctx, m, spec, progress)
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		last = preferIntegrity(last, err)
	}
	if last == nil {
		last = fmt.Errorf("%w: нет источников артефактов", ErrNetwork)
	}
	return nil, last
}

func fetchBlobFrom(ctx context.Context, src ArtifactSource, spec BlobSpec, progress func(done, total int64)) ([]byte, error) {
	st, err := src.Open(ctx, spec.Path, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer st.Body.Close()
	if st.Start != 0 {
		return nil, fmt.Errorf("%w: сервер отдал смещение %d вместо начала", ErrLengthMismatch, st.Start)
	}
	limit := spec.limit()
	raw, err := readCounting(&ctxReader{ctx: ctx, r: st.Body}, limit+1, spec.Length, progress)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	if int64(len(raw)) > limit {
		if spec.Length > 0 {
			return nil, fmt.Errorf("%w: источник отдал больше %d байт", ErrLengthMismatch, limit)
		}
		return nil, fmt.Errorf("%w: артефакт больше %d байт", ErrTooLarge, limit)
	}
	if spec.Length > 0 && int64(len(raw)) != spec.Length {
		return nil, fmt.Errorf("%w: получено %d из %d байт", ErrLengthMismatch, len(raw), spec.Length)
	}
	if err := CheckSha256(raw, spec.Sha256); err != nil {
		return nil, err
	}
	return raw, nil
}

// readCounting читает не больше max байт, сообщая прогресс (done растёт монотонно).
func readCounting(r io.Reader, max, total int64, progress func(done, total int64)) ([]byte, error) {
	var out []byte
	buf := make([]byte, 64<<10)
	for int64(len(out)) < max {
		n, err := r.Read(buf[:min(int64(len(buf)), max-int64(len(out)))])
		out = append(out, buf[:n]...)
		if n > 0 && progress != nil {
			progress(int64(len(out)), total)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
