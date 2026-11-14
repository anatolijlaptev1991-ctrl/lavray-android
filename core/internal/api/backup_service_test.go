// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/backup"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

func TestBackupRoundTripRestoresProfilesGroupsSettings(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	if !strings.HasPrefix(exp.SuggestedName, "lavray-backup-") || !strings.HasSuffix(exp.SuggestedName, ".lavbk") {
		t.Errorf("suggested_name = %q", exp.SuggestedName)
	}

	res, err := b.importData(exp.Data, bkPassword, true)
	if err != nil {
		t.Fatalf("ImportBackup: %v", err)
	}
	want := map[string]bool{"vm-A": true, "tr-A": true}
	got := profileNames(t, b.st)
	if len(got) != len(want) || !got["vm-A"] || !got["tr-A"] {
		t.Fatalf("профили после восстановления: %v", got)
	}
	if v, _, _ := b.st.GetSetting("ui.theme"); v != `"theme-A"` {
		t.Errorf("ui.theme = %q", v)
	}
	if v, _, _ := b.st.GetSetting("inbound.auth.pass"); v != `"`+bkProxyPass+`-A"` {
		t.Errorf("inbound.auth.pass = %q", v)
	}
	// секрет профиля и адрес подписки вернулись
	ps, _ := b.st.ListProfiles(-1)
	var sawUUID, sawTrojan bool
	for _, p := range ps {
		switch d := p.Detail.(type) {
		case *model.VMessDetail:
			sawUUID = sawUUID || d.UUID == bkVmessUUID
		case *model.TrojanVlessDetail:
			sawTrojan = sawTrojan || d.Password == "trojan-secret-A"
		}
	}
	if !sawUUID || !sawTrojan {
		t.Errorf("секреты профилей не вернулись: uuid=%v trojan=%v", sawUUID, sawTrojan)
	}
	groups, _ := b.st.ListGroups()
	var subURL string
	for _, g := range groups {
		if g.Kind == 2 {
			subURL = g.SubscriptionURL
		}
	}
	if subURL != bkSubToken+"-A" {
		t.Errorf("адрес подписки = %q", subURL)
	}
	if res.Restored.Profiles != 2 || res.Restored.Subscriptions != 1 || res.Restored.Groups != 3 || res.Restored.Presets != 1 {
		t.Errorf("restored = %+v", res.Restored)
	}
	// ListProfiles по RPC после восстановления видит новые данные
	lp, err := b.profile.ListProfiles(context.Background(), connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if err != nil || lp.Msg.Total != 2 {
		t.Errorf("ListProfiles: %v, %v", lp, err)
	}
}

func TestBackupWrongPasswordFails(t *testing.T) {
	a := newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	exp := a.export(t)
	before := profileNames(t, a.st)
	_, err := a.importData(exp.Data, bkPassword+"x", true)
	wantReason(t, err, connect.CodePermissionDenied, reasonWrongPassword)
	_, err = a.preview(exp.Data, bkPassword+"x")
	wantReason(t, err, connect.CodePermissionDenied, reasonWrongPassword)
	if got := profileNames(t, a.st); len(got) != len(before) {
		t.Errorf("данные изменились после неверного пароля: %v", got)
	}
}

func TestBackupTamperedAndNotRecognized(t *testing.T) {
	a := newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	exp := a.export(t)

	body := bytes.Clone(exp.Data)
	body[len(body)-1] ^= 0x01
	_, err := a.preview(body, bkPassword)
	wantReason(t, err, connect.CodeDataLoss, reasonTampered)

	cut := exp.Data[:len(exp.Data)/2]
	_, err = a.importData(cut, bkPassword, true)
	wantReason(t, err, connect.CodeDataLoss, reasonTampered)

	_, err = a.preview([]byte("это вовсе не файл копии"), bkPassword)
	wantReason(t, err, connect.CodeInvalidArgument, reasonNotRecognized)
	_, err = a.preview(nil, bkPassword)
	wantReason(t, err, connect.CodeInvalidArgument, reasonNotRecognized)
}

func TestBackupFromNewerVersionRejected(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	future := customBackup(t, a.st, bkPassword, func(db *sql.DB) {
		if _, err := db.Exec(`INSERT INTO schema_version(version, applied_at_ms) VALUES (9999, 1)`); err != nil {
			t.Fatal(err)
		}
	})
	_, err := b.preview(future, bkPassword)
	wantReason(t, err, connect.CodeFailedPrecondition, reasonNewerVersion)
	_, err = b.importData(future, bkPassword, true)
	wantReason(t, err, connect.CodeFailedPrecondition, reasonNewerVersion)
	if got := profileNames(t, b.st); !got["vm-B"] || got["vm-A"] {
		t.Errorf("данные получателя изменились: %v", got)
	}
}

func TestBackupDamagedInsideRejected(t *testing.T) {
	a := newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	// копия, зашифрованная верно, но с мусором вместо SQLite внутри
	body, err := backup.Pack([]byte(`{}`), bytes.NewReader([]byte("не SQLite")))
	if err != nil {
		t.Fatal(err)
	}
	file, err := backup.Seal(bkPassword, body, fastKDF)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.preview(file, bkPassword)
	wantReason(t, err, connect.CodeDataLoss, reasonTampered)
}

func TestPreviewSummaryCounts(t *testing.T) {
	a := newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	if err := a.st.SetSetting("update.serial_stable", `7`); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UnixMilli()
	exp := a.export(t)
	sum, err := a.preview(exp.Data, bkPassword)
	if err != nil {
		t.Fatalf("PreviewBackup: %v", err)
	}
	schema, _ := a.st.SchemaVersion()
	// Default (миграция) + группа 1 (фикстура) + Sub-A; два профиля; одна подписка; один набор; настройки без update.*
	if sum.Groups != 3 || sum.Profiles != 2 || sum.Subscriptions != 1 || sum.Presets != 1 || sum.Settings != 2 {
		t.Errorf("summary = %+v", sum)
	}
	if sum.AppVersion != version.Version || sum.SchemaVersion != schema {
		t.Errorf("версии: %q/%d, ждём %q/%d", sum.AppVersion, sum.SchemaVersion, version.Version, schema)
	}
	if sum.CreatedAtMs < before || sum.CreatedAtMs > time.Now().UnixMilli() {
		t.Errorf("created_at_ms = %d", sum.CreatedAtMs)
	}
	if exp.Summary.Profiles != sum.Profiles || exp.Summary.Groups != sum.Groups {
		t.Errorf("сводка экспорта %+v != предпросмотра %+v", exp.Summary, sum)
	}
	// предпросмотр ничего не меняет
	if v, _, _ := a.st.GetSetting("update.serial_stable"); v != "7" {
		t.Errorf("update.serial_stable = %q", v)
	}
}

func TestImportRequiresConfirmReplace(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	_, err := b.importData(exp.Data, bkPassword, false)
	wantReason(t, err, connect.CodeFailedPrecondition, reasonConfirmRequired)
	if got := profileNames(t, b.st); !got["vm-B"] || got["vm-A"] {
		t.Errorf("данные заменены без подтверждения: %v", got)
	}
}

func TestImportWhileConnectedPrecondition(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	if err := b.srv.inst.Start(context.Background(), inst.RunInfo{Mode: "system_proxy", ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	_, err := b.importData(exp.Data, bkPassword, true)
	wantReason(t, err, connect.CodeFailedPrecondition, reasonConnectionActive)
	if got := profileNames(t, b.st); !got["vm-B"] {
		t.Errorf("данные изменились при активном подключении: %v", got)
	}
	// экспорт и предпросмотр при подключении работают
	if _, err := b.preview(exp.Data, bkPassword); err != nil {
		t.Errorf("PreviewBackup при подключении: %v", err)
	}
	b.export(t)
	if err := b.srv.inst.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.importData(exp.Data, bkPassword, true); err != nil {
		t.Fatalf("после отключения: %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	a := newBackupFixture(t)
	for _, tc := range []struct {
		name, pw, reason string
		code             connect.Code
	}{
		{"пустой", "", reasonPasswordShort, connect.CodeInvalidArgument},
		{"7 символов", "abcdefg", reasonPasswordShort, connect.CodeInvalidArgument},
		{"7 кириллических", "пароль1", reasonPasswordShort, connect.CodeInvalidArgument},
		{"1025 байт", strings.Repeat("a", 1025), reasonPasswordLong, connect.CodeInvalidArgument},
	} {
		_, err := a.backup.ExportBackup(context.Background(),
			connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: tc.pw}))
		if connect.CodeOf(err) != tc.code || errorReason(err) != tc.reason {
			t.Errorf("%s: %v (причина %q)", tc.name, err, errorReason(err))
		}
	}
	for _, pw := range []string{"abcdefgh", "пароль12", strings.Repeat("я", 512)} { // 8 символов; 8 символов; 1024 байта
		if _, err := a.backup.ExportBackup(context.Background(),
			connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: pw})); err != nil {
			t.Errorf("пароль %q отвергнут: %v", pw, err)
		}
	}
	// те же правила для PreviewBackup и ImportBackup
	_, err := a.preview(nil, "short")
	wantReason(t, err, connect.CodeInvalidArgument, reasonPasswordShort)
	_, err = a.importData(nil, "short", true)
	wantReason(t, err, connect.CodeInvalidArgument, reasonPasswordShort)
}

func TestRestoreKeepsUpdateSerialViaAPI(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	if err := a.st.SetSetting("update.serial_stable", `1`); err != nil {
		t.Fatal(err)
	}
	if err := b.st.SetSetting("update.serial_stable", `42`); err != nil {
		t.Fatal(err)
	}
	exp := a.export(t)
	if _, err := b.importData(exp.Data, bkPassword, true); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := b.st.GetSetting("update.serial_stable"); v != "42" {
		t.Errorf("update.serial_stable = %q, ждём 42", v)
	}
}

func TestBackupSchemaMatchesStore(t *testing.T) {
	if metaCustomPresets != store.MetaCustomPresets {
		t.Fatalf("api.metaCustomPresets %q != store.MetaCustomPresets %q", metaCustomPresets, store.MetaCustomPresets)
	}
}
