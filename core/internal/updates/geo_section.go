// SPDX-License-Identifier: GPL-3.0-only

package updates

import "fmt"

// MaxGeoRev — потолок ревизии гео-набора (unix-секунды с большим запасом): один
// подписанный огромный rev не должен навсегда закрыть набор для следующих.
const MaxGeoRev = int64(1) << 40

// validate проверяет секцию geo подписанного манифеста: обязательные поля,
// путь, sha256, длину и ревизию.
func (g *GeoSection) validate(signed []byte) error {
	if err := requireSectionKeys(signed, "geo", "path", "sha256", "length", "rev"); err != nil {
		return err
	}
	if err := checkRelativePath(g.Path); err != nil {
		return fmt.Errorf("geo.path: %w", err)
	}
	if err := checkHex64(g.Sha256); err != nil {
		return fmt.Errorf("geo.sha256: %w", err)
	}
	if g.Length < 1 {
		return fmt.Errorf("%w: geo.length=%d < 1", ErrBadValue, g.Length)
	}
	if g.Rev < 1 || g.Rev > MaxGeoRev {
		return fmt.Errorf("%w: geo.rev=%d вне 1…%d", ErrBadValue, g.Rev, MaxGeoRev)
	}
	return nil
}
