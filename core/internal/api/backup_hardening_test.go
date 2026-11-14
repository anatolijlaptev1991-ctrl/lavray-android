// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_hardening_test.go — защита от сбоев и злоупотреблений вокруг копии
// (T-83, ревью безопасности): предел снимка при создании, подметание
// расшифрованных временных каталогов, гонка с Connect, ответы без путей.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// logText — весь журнал ядра одной строкой.
func logText(lg *logs.Mux) string {
	var sb strings.Builder
	for _, l := range lg.Tail(0) {
		sb.WriteString(l.Message)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// TestExportRefusedWhenDatabaseExceedsCap — S: база больше предела снимка не
// превращается в копию, которую потом нельзя восстановить.
func TestExportRefusedWhenDatabaseExceedsCap(t *testing.T) {
	f := newBackupFixture(t)
	seedBackupData(t, f.st, "A")
	f.srv.backup.maxSnapshot = 1024 // меньше любой SQLite-базы
	_, err := f.backup.ExportBackup(context.Background(),
		connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: bkPassword}))
	wantReason(t, err, connect.CodeResourceExhausted, reasonTooLarge)
	assertNoTempDirs(t, f.st)
}

func assertNoTempDirs(t *testing.T, st *store.Store) {
	t.Helper()
	ents, err := os.ReadDir(st.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() && (strings.HasPrefix(e.Name(), store.TempDirBackup) || strings.HasPrefix(e.Name(), store.TempDirRestore)) {
			t.Errorf("остался временный каталог %s", e.Name())
		}
	}
}

// TestStaleTempDirsSweptAtStart — S: расшифрованные снимки, оставшиеся после
// падения процесса, удаляются при создании сервера; чужие каталоги целы.
func TestStaleTempDirsSweptAtStart(t *testing.T) {
	dir, err := os.MkdirTemp("", "lavray-sweep-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		_ = os.RemoveAll(dir)
	})
	for _, name := range []string{"backup-777", "restore-888", "geo"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "backup-777", "snapshot.db"), []byte("PLAINTEXT"), 0o600); err != nil {
		t.Fatal(err)
	}
	lg := logs.NewMux(100)

	NewServer(Deps{Store: st, Logs: lg, Runner: &inst.FakeRunner{}})

	assertNoTempDirs(t, st)
	if _, err := os.Stat(filepath.Join(dir, "geo")); err != nil {
		t.Errorf("подмели чужой каталог: %v", err)
	}
	if !strings.Contains(logText(lg), "временных каталогов резервной копии: 2") {
		t.Errorf("подметание не отражено в журнале:\n%s", logText(lg))
	}
}

// TestTempDirRemovalFailureIsLogged — сбой удаления временного каталога не
// ломает успешную операцию, но попадает в журнал.
func TestTempDirRemovalFailureIsLogged(t *testing.T) {
	f := newBackupFixture(t)
	seedBackupData(t, f.st, "A")
	f.srv.backup.removeDir = func(dir string) error {
		defer func() { _ = os.RemoveAll(dir) }() // тест сам прибирает за собой
		return errors.New("Access is denied")
	}
	f.export(t)
	if !strings.Contains(logText(f.logs), "Access is denied") {
		t.Errorf("сбой удаления не записан в журнал:\n%s", logText(f.logs))
	}
}

// TestConnectRefusedDuringRestore — C: пока данные заменяются, запуск
// подключения отклоняется, а не стартует на полуобновлённых данных.
func TestConnectRefusedDuringRestore(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	ps, err := b.st.ListProfiles(-1)
	if err != nil || len(ps) == 0 {
		t.Fatal(err)
	}

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	b.srv.backup.onReplacing = func() error {
		close(entered)
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() { _, err := b.importData(exp.Data, bkPassword, true); done <- err }()
	<-entered

	_, _, err = b.srv.startRun(context.Background(), ps[0], lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "восстановлен") {
		t.Fatalf("запуск во время замены: %v", err)
	}
	if b.runner.StartedCounter != 0 {
		t.Errorf("инстанс запущен во время замены (%d)", b.runner.StartedCounter)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatalf("замена: %v", err)
	}
	restored, _ := b.st.ListProfiles(-1)
	if len(restored) == 0 {
		t.Fatal("после замены нет профилей")
	}
	if _, _, err := b.srv.startRun(context.Background(), restored[0], lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY); err != nil {
		t.Fatalf("запуск после замены: %v", err)
	}
}

// TestImportWaitsForInFlightConnect — C: запуск, начавшийся после первой
// проверки «подключения нет», но до замены, виден замене: она отказывает, а не
// затирает данные под поднимающимся туннелем. (Сам запрет запуска посреди
// замены проверяет TestConnectRefusedDuringRestore.)
func TestImportWaitsForInFlightConnect(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	ps, err := b.st.ListProfiles(-1)
	if err != nil || len(ps) == 0 {
		t.Fatal(err)
	}
	b.runner.StartDelay = 300 * time.Millisecond

	connected := make(chan error, 1)
	b.srv.backup.beforeLock = func() {
		go func() {
			_, _, err := b.srv.startRun(context.Background(), ps[0], lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY)
			connected <- err
		}()
		for i := 0; i < 500 && b.srv.inst.Status().State != inst.StateStarting; i++ {
			time.Sleep(2 * time.Millisecond)
		}
	}
	_, err = b.importData(exp.Data, bkPassword, true)
	wantReason(t, err, connect.CodeFailedPrecondition, reasonConnectionActive)
	if err := <-connected; err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := profileNames(t, b.st); !got["vm-B"] || got["vm-A"] {
		t.Errorf("данные заменены под поднимающимся туннелем: %v", got)
	}
}

// TestUnmappedErrorsDoNotLeakPaths — S: необработанная ошибка уходит клиенту
// общей фразой с причиной backup_failed, без локальных путей; подробности — в
// журнале ядра. Данные при этом целы.
func TestUnmappedErrorsDoNotLeakPaths(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	const secretPath = `C:\Users\someone\AppData\LAVRay\backup-123\snapshot.db`
	b.srv.backup.onReplacing = func() error {
		return errors.New("запись снимка: open " + secretPath + ": Access is denied")
	}
	_, err := b.importData(exp.Data, bkPassword, true)
	wantReason(t, err, connect.CodeInternal, reasonFailed)
	if strings.Contains(err.Error(), "someone") || strings.Contains(err.Error(), "snapshot.db") {
		t.Errorf("в ответе клиенту локальный путь: %v", err)
	}
	if !strings.Contains(logText(b.logs), secretPath) {
		t.Errorf("подробности ошибки не записаны в журнал:\n%s", logText(b.logs))
	}
	if got := profileNames(t, b.st); !got["vm-B"] || got["vm-A"] {
		t.Errorf("данные изменились: %v", got)
	}
}

func TestFailedKeepsConnectErrorsAndCancellation(t *testing.T) {
	f := newBackupFixture(t)
	own := backupError(connect.CodeAborted, reasonBusy, "занято")
	if got := f.srv.backup.failed("x", own); got != own {
		t.Errorf("ошибка с причиной подменена: %v", got)
	}
	if got := f.srv.backup.failed("x", context.Canceled); connect.CodeOf(got) != connect.CodeCanceled {
		t.Errorf("отмена: %v", got)
	}
	if f.srv.backup.failed("x", nil) != nil {
		t.Error("nil превратился в ошибку")
	}
}

// TestBackupWithDanglingReferencesRejected — копия с «висячей» ссылкой
// (подделанная, но зашифрованная верным паролем) отвергается как повреждённая.
func TestBackupWithDanglingReferencesRejected(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	bad := customBackup(t, a.st, bkPassword, func(db *sql.DB) {
		for _, q := range []string{
			`PRAGMA foreign_keys = OFF`,
			`INSERT INTO profiles(id,group_id,type,sort_key,data,created_at_ms,updated_at_ms) VALUES (999999,424242,'trojan',1,'{}',1,1)`,
		} {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	})
	_, err := b.importData(bad, bkPassword, true)
	wantReason(t, err, connect.CodeDataLoss, reasonTampered)
	if got := profileNames(t, b.st); !got["vm-B"] || got["vm-A"] {
		t.Errorf("данные изменились: %v", got)
	}
}

func TestSummaryDocValidation(t *testing.T) {
	long := strings.Repeat("9", maxAppVersionLen+1)
	for name, raw := range map[string]string{
		"не JSON":             "{",
		"длинная версия":      `{"app_version":"` + long + `"}`,
		"отрицательное время": `{"created_at_ms":-1}`,
		"отрицательная схема": `{"schema_version":-5}`,
		"JSON не объект":      `[1,2,3]`,
		"тип поля не тот":     `{"groups":"много"}`,
	} {
		if _, err := parseSummaryDoc([]byte(raw)); err == nil {
			t.Errorf("%s: сводка принята", name)
		}
	}
	d, err := parseSummaryDoc([]byte(`{"app_version":"0.7.0","created_at_ms":5,"schema_version":4,"extra":1}`))
	if err != nil || d.AppVersion != "0.7.0" || d.CreatedAtMs != 5 {
		t.Errorf("корректная сводка: %+v, %v", d, err)
	}
	// пустая сводка допустима: показ возьмёт числа из самого снимка
	if _, err := parseSummaryDoc([]byte(`{}`)); err != nil {
		t.Errorf("пустая сводка: %v", err)
	}
}

// FuzzParseSummaryDoc — разбор сводки из чужой копии не паникует, а принятая
// сводка укладывается в границы.
func FuzzParseSummaryDoc(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"app_version":"0.7.0","schema_version":4,"created_at_ms":1790000000000,"groups":3}`,
		`{"groups":-1}`, `[]`, `null`, `{"app_version":1}`, `{"created_at_ms":1e30}`, "",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		d, err := parseSummaryDoc(raw)
		if err != nil {
			return
		}
		if len(d.AppVersion) > maxAppVersionLen || d.CreatedAtMs < 0 || d.SchemaVersion < 0 {
			t.Fatalf("принята сводка вне границ: %+v", d)
		}
		_ = d.summaryProto() // не паникует на любых числах
	})
}

var _ = model.TypeVMess
