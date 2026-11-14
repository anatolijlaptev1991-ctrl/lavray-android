// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_fixture_test.go — оснастка тестов BackupService (T-83): сервер с
// настоящим хранилищем и клиентом Connect, быстрые параметры Argon2id.

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/backup"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

const (
	bkPassword  = "Tr0ub4dor&3-UNIQUE-PW"
	bkProxyPass = "SuperSecretProxyPass-123"
	bkVmessUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"
	bkSubToken  = "https://sub.example.com/api/v1/token-SECRET-abc"
)

// fastKDF — минимально допустимые параметры Argon2id: тесты не ждут по 0,3 с.
var fastKDF = backup.Options{Params: backup.Params{T: 1, MKiB: 8 * 1024, P: 1}}

type backupFixture struct {
	srv     *Server
	st      *store.Store
	logs    *logs.Mux
	runner  *inst.FakeRunner
	url     string
	backup  lavrayv1connect.BackupServiceClient
	profile lavrayv1connect.ProfileServiceClient
}

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	lg := logs.NewMux(4000)
	runner := &inst.FakeRunner{}
	var srv *Server
	st, ts := newE2EServer(t, Deps{Runner: runner, Logs: lg}, func(s *Server) {
		srv = s
		s.backup.sealOpts = fastKDF
	})
	hc := ts.Client()
	return &backupFixture{
		srv: srv, st: st, logs: lg, runner: runner, url: ts.URL,
		backup:  lavrayv1connect.NewBackupServiceClient(hc, ts.URL),
		profile: lavrayv1connect.NewProfileServiceClient(hc, ts.URL),
	}
}

// seedBackupData наполняет хранилище данными с секретами; tag отличает «до» и
// «после». Профиль vmess с UUID, trojan с паролем, подписка с токеном в адресе.
func seedBackupData(t *testing.T, st *store.Store, tag string) {
	t.Helper()
	sub := &model.Group{Name: "Sub-" + tag, Kind: model.GroupSubscription, SubscriptionURL: bkSubToken + "-" + tag}
	if err := st.CreateGroup(sub); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*model.Profile{
		{GroupID: 1, Type: model.TypeVMess, Name: "vm-" + tag, Address: "vm." + tag + ".example", Port: 443,
			Detail: &model.VMessDetail{UUID: bkVmessUUID, Security: "auto"}},
		{GroupID: sub.ID, Type: model.TypeTrojan, Name: "tr-" + tag, Address: "tr." + tag + ".example", Port: 8443,
			Detail: &model.TrojanVlessDetail{Password: "trojan-secret-" + tag}},
	} {
		if err := st.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range map[string]string{
		"inbound.auth.pass": `"` + bkProxyPass + `-` + tag + `"`,
		"ui.theme":          `"theme-` + tag + `"`,
	} {
		if err := st.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetMeta(store.MetaCustomPresets, `[{"id":"custom-`+tag+`"}]`); err != nil {
		t.Fatal(err)
	}
}

// export снимает копию паролем bkPassword.
func (f *backupFixture) export(t *testing.T) *lavrayv1.ExportBackupResponse {
	t.Helper()
	res, err := f.backup.ExportBackup(context.Background(),
		connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: bkPassword}))
	if err != nil {
		t.Fatalf("ExportBackup: %v", err)
	}
	return res.Msg
}

func (f *backupFixture) importData(data []byte, password string, confirm bool) (*lavrayv1.ImportBackupResponse, error) {
	res, err := f.backup.ImportBackup(context.Background(), connect.NewRequest(&lavrayv1.ImportBackupRequest{
		Data: data, Password: password, ConfirmReplace: confirm}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (f *backupFixture) preview(data []byte, password string) (*lavrayv1.BackupSummary, error) {
	res, err := f.backup.PreviewBackup(context.Background(),
		connect.NewRequest(&lavrayv1.PreviewBackupRequest{Data: data, Password: password}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// wantReason проверяет код ошибки Connect и причину в деталях ErrorInfo.
func wantReason(t *testing.T, err error, code connect.Code, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ошибки нет, ждали %v/%s", code, reason)
	}
	if got := connect.CodeOf(err); got != code {
		t.Fatalf("код = %v (%v), ждали %v/%s", got, err, code, reason)
	}
	if got := errorReason(err); got != reason {
		t.Fatalf("причина = %q (%v), ждали %q", got, err, reason)
	}
}

// errorReason достаёт reason из первой детали ErrorInfo; "" — деталей нет.
func errorReason(err error) string {
	var ce *connect.Error
	if !asConnectErr(err, &ce) {
		return ""
	}
	for _, d := range ce.Details() {
		msg, merr := d.Value()
		if merr != nil {
			continue
		}
		if info, ok := msg.(*errdetails.ErrorInfo); ok {
			if info.Domain != "lavray.core" {
				return "domain=" + info.Domain
			}
			return info.Reason
		}
	}
	return ""
}

func asConnectErr(err error, target **connect.Error) bool {
	ce, ok := err.(*connect.Error)
	if ok {
		*target = ce
	}
	return ok
}

// profileNames — имена всех профилей хранилища (по группам).
func profileNames(t *testing.T, st *store.Store) map[string]bool {
	t.Helper()
	ps, err := st.ListProfiles(-1)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, p := range ps {
		out[p.Name] = true
	}
	return out
}

// customBackup строит валидный файл копии из хранилища и даёт тесту изменить
// SQLite-снимок до упаковки (например, поставить версию схемы «из будущего»).
func customBackup(t *testing.T, st *store.Store, password string, mutate func(db *sql.DB)) []byte {
	t.Helper()
	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := st.Snapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(snap))
		if err != nil {
			t.Fatal(err)
		}
		mutate(db)
		_ = db.Close()
	}
	raw := readFile(t, snap)
	body, err := backup.Pack([]byte(`{"app_version":"9.9.9","created_at_ms":1}`), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	file, err := backup.Seal(password, body, fastKDF)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
