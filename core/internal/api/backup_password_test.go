// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_password_test.go — пароль копии и Unicode (D-6): NFC и NFD одного
// пароля равнозначны, старые «сырые» NFD-копии открываются, длина считается по
// нормализованной форме.

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// Один пароль в двух формах Unicode: «é» и «й» одной кодовой точкой (NFC) либо
// буквой со следующим знаком (NFD).
const (
	bkPwNFC = "caf\u00e9-\u0439\u043e\u0433\u0443\u0440\u0442"
	bkPwNFD = "cafe\u0301-\u0438\u0306\u043e\u0433\u0443\u0440\u0442"
)

func (f *backupFixture) exportWith(t *testing.T, password string) []byte {
	t.Helper()
	res, err := f.backup.ExportBackup(context.Background(),
		connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: password}))
	if err != nil {
		t.Fatalf("ExportBackup: %v", err)
	}
	return res.Msg.Data
}

func TestBackupPasswordNFCExportNFDPreviewImport(t *testing.T) {
	for _, tc := range []struct{ name, seal, open string }{
		{"NFC → NFD", bkPwNFC, bkPwNFD},
		{"NFD → NFC", bkPwNFD, bkPwNFC},
	} {
		a, b := newBackupFixture(t), newBackupFixture(t)
		seedBackupData(t, a.st, "A")
		seedBackupData(t, b.st, "B")
		data := a.exportWith(t, tc.seal)
		sum, err := b.preview(data, tc.open)
		if err != nil || sum.Profiles != 2 {
			t.Fatalf("%s: PreviewBackup: %v, %v", tc.name, sum, err)
		}
		res, err := b.importData(data, tc.open, true)
		if err != nil || res.Restored.Profiles != 2 {
			t.Fatalf("%s: ImportBackup: %v, %v", tc.name, res, err)
		}
		if got := profileNames(t, b.st); !got["vm-A"] || !got["tr-A"] {
			t.Errorf("%s: профили после восстановления: %v", tc.name, got)
		}
	}
}

// Старая копия, запечатанная «сырыми» байтами NFD, открывается тем же паролем
// в NFD; неверный пароль по-прежнему отказывает.
func TestBackupLegacyRawNFDStillOpens(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	legacy := customBackup(t, a.st, bkPwNFD, nil) // backup.Seal без нормализации
	if _, err := b.preview(legacy, bkPwNFD); err != nil {
		t.Fatalf("PreviewBackup(NFD) старой копии: %v", err)
	}
	if _, err := b.importData(legacy, bkPwNFD, true); err != nil {
		t.Fatalf("ImportBackup(NFD) старой копии: %v", err)
	}
	if got := profileNames(t, b.st); !got["vm-A"] {
		t.Errorf("профили после восстановления старой копии: %v", got)
	}
	_, err := b.preview(legacy, bkPwNFD+"x")
	wantReason(t, err, connect.CodePermissionDenied, reasonWrongPassword)
}

func TestBackupWrongPasswordUnicodeStillFails(t *testing.T) {
	a := newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	data := a.exportWith(t, bkPwNFC)
	for _, pw := range []string{bkPwNFC + "x", bkPwNFD + "x", "cafe-йогурт"} {
		_, err := a.preview(data, pw)
		wantReason(t, err, connect.CodePermissionDenied, reasonWrongPassword)
		_, err = a.importData(data, pw, true)
		wantReason(t, err, connect.CodePermissionDenied, reasonWrongPassword)
	}
}

// Длина пароля меряется по нормализованной форме: 8 символов и 1024 байта — как
// в интерфейсе (ui/src/lib/password.ts).
func TestPasswordPolicyMeasuredOnNFC(t *testing.T) {
	a := newBackupFixture(t)
	export := func(pw string) error {
		_, err := a.backup.ExportBackup(context.Background(),
			connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: pw}))
		return err
	}
	// «abcdefé» в NFD — 8 кодовых точек, в NFC — 7: слишком короткий.
	wantReason(t, export("abcdefe\u0301"), connect.CodeInvalidArgument, reasonPasswordShort)
	// «abcdefgé» в NFD — 9 кодовых точек, в NFC — 8: годится.
	if err := export("abcdefge\u0301"); err != nil {
		t.Errorf("8 символов в NFC отвергнуты: %v", err)
	}
	// 512 × «é» — ровно 1024 байта в NFC (в NFD было бы 1536): годится в обеих формах.
	for _, pw := range []string{strings.Repeat("\u00e9", 512), strings.Repeat("e\u0301", 512)} {
		if err := export(pw); err != nil {
			t.Errorf("1024 байта в NFC отвергнуты (%d байт сырых): %v", len(pw), err)
		}
	}
	wantReason(t, export(strings.Repeat("\u00e9", 513)), connect.CodeInvalidArgument, reasonPasswordLong)
	// То же правило для просмотра и восстановления.
	_, err := a.preview(nil, "abcdefe\u0301")
	wantReason(t, err, connect.CodeInvalidArgument, reasonPasswordShort)
	_, err = a.importData(nil, "abcdefe\u0301", true)
	wantReason(t, err, connect.CodeInvalidArgument, reasonPasswordShort)
}
