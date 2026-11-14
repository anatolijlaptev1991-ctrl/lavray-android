// SPDX-License-Identifier: GPL-3.0-only

// download_stream.go — потоковая запись тела артефакта в .part с точным
// контролем длины: лишний байт немедленно прерывает загрузку.
package updates

import (
	"context"
	"fmt"
	"io"
	"os"
)

func (r *run) stream(ctx context.Context, h *digests, f *os.File, body io.ReadCloser, offset int64) error {
	defer body.Close()
	total := r.asset.Length
	rd := io.LimitReader(&ctxReader{ctx: ctx, r: body}, total-offset+1)
	r.report(offset)
	buf := make([]byte, 64<<10)
	for {
		n, rerr := rd.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			_, _ = h.Write(buf[:n])
			if h.written > total {
				return fmt.Errorf("%w: источник отдал больше %d байт", ErrLengthMismatch, total)
			}
			r.report(h.written)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("%w: %v", ErrNetwork, rerr)
		}
	}
	if h.written != total {
		return fmt.Errorf("%w: получено %d из %d байт", ErrLengthMismatch, h.written, total)
	}
	return nil
}
