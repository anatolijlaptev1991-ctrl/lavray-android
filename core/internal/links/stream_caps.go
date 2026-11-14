// SPDX-License-Identifier: GPL-3.0-only

package links

// stream_caps.go — лимиты полей потока (LNB-17) и параметры XHTTP ссылки.

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// validateStreamCaps — лимиты полей потока (LNB-17).
func validateStreamCaps(scheme string, st *model.StreamSettings) error {
	for _, f := range []struct {
		name, val string
	}{
		{"path", st.Path}, {"host", st.Host}, {"sni", st.SNI},
		{"alpn", st.ALPN}, {"reality_public_key", st.RealityPublicKey},
		{"reality_short_id", st.RealityShortID}, {"reality_spx", st.RealitySPX},
		{"header_type", st.HeaderType}, {"xhttp_mode", st.XHTTPMode},
	} {
		if err := checkFieldLen(scheme, f.name, f.val, maxFieldLength); err != nil {
			return err
		}
	}
	if err := checkFieldLen(scheme, "xhttp_extra", st.XHTTPExtra, maxXHTTPExtra); err != nil {
		return err
	}
	if st.Security == "reality" && st.RealityPublicKey == "" {
		return fmt.Errorf("%s: security=reality без pbk", scheme)
	}
	return nil
}

// applyXHTTPQuery читает параметры mode и extra ссылки. Только у XHTTP: у остальных
// транспортов их нет, и значения из ссылки в профиль не попадают.
func applyXHTTPQuery(st *model.StreamSettings, get func(string) (string, bool)) {
	if st.Network != "xhttp" {
		return
	}
	st.XHTTPMode, _ = get("mode")
	st.XHTTPExtra, _ = get("extra")
}

// addIf добавляет параметр запроса, только если значение не пусто.
func (b *linkBuilder) addIf(key, value string) *linkBuilder {
	if value != "" {
		b.addQ(key, value)
	}
	return b
}
