// SPDX-License-Identifier: GPL-3.0-only

package api

// update_reason_invalid_test.go — отказ по подписанному, но неверно устроенному
// документу метаданных (D-7c): FAILED_PRECONDITION с устойчивой причиной
// metadata_invalid в начале сообщения и в заголовке, как у остальных причин.

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

func wantMetadataInvalid(t *testing.T, f *updFix, err error) *connect.Error {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeFailedPrecondition {
		t.Fatalf("ожидался FAILED_PRECONDITION: %v", err)
	}
	if got := ce.Meta().Get(UpdateReasonHeader); got != ReasonMetadataInvalid {
		t.Errorf("заголовок %s = %q, want %q (сообщение %q)", UpdateReasonHeader, got, ReasonMetadataInvalid, ce.Message())
	}
	if !strings.HasPrefix(ce.Message(), ReasonMetadataInvalid+": ") {
		t.Errorf("сообщение без причины в начале: %q", ce.Message())
	}
	if f.srv.loadUpdateState().LastCheckedAt != 0 {
		t.Error("last_checked_at не меняется при отказе")
	}
	return ce
}

// Манифест с настоящей подписью, но без geo.rev: отказ с причиной.
func TestCheckManifestWithoutGeoRevHasReason(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.5.0", Serial: 1, TSVersion: 1, Geo: geoArchive(t, "extra"),
		MutateManifest: func(mf map[string]any) { delete(mf["geo"].(map[string]any), "rev") }})
	_, err := f.check()
	ce := wantMetadataInvalid(t, f, err)
	if !strings.Contains(ce.Message(), `"rev"`) {
		t.Errorf("в сообщении нет названия поля: %q", ce.Message())
	}
}

// Другие неверные по форме документы (лишнее поле, мусор вместо timestamp,
// неверное значение) получают ту же причину.
func TestCheckMalformedMetadataHasReason(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate           func(mf map[string]any)
		garbageTimestamp bool
	}{
		"лишнее поле манифеста":  {mutate: func(mf map[string]any) { mf["unexpected"] = 1 }},
		"mandatory не булево":    {mutate: func(mf map[string]any) { mf["mandatory"] = "yes" }},
		"мусор вместо timestamp": {garbageTimestamp: true},
	} {
		f := newUpdFix(t, "0.5.0", nil)
		f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1, MutateManifest: tc.mutate})
		if tc.garbageTimestamp {
			f.src.Put("timestamp.json", []byte("не json"))
		}
		_, err := f.check()
		t.Run(name, func(t *testing.T) { wantMetadataInvalid(t, f, err) })
	}
}
