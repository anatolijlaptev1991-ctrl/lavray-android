// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
)

// TestBackupFileHasNoPlaintextSecrets — S: в файле копии нет ни пароля копии,
// ни паролей и UUID профилей, ни адреса подписки, ни заголовка SQLite.
func TestBackupFileHasNoPlaintextSecrets(t *testing.T) {
	a := newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	exp := a.export(t)
	for _, secret := range []string{
		bkPassword, bkProxyPass, bkVmessUUID, bkSubToken, "trojan-secret-A",
		"vm.A.example", "SQLite format", "inbound.auth.pass", "lavray",
	} {
		if bytes.Contains(exp.Data, []byte(secret)) {
			t.Errorf("в файле копии открытым текстом найдено %q", secret)
		}
	}
	if !bytes.HasPrefix(exp.Data, []byte("LAVBK1")) {
		t.Errorf("файл не начинается с LAVBK1: %q", exp.Data[:8])
	}
}

// TestBackupPasswordNeverLogged — S: пароль не попадает ни в журнал ядра, ни в
// базу, ни в файлы каталога данных, ни в тексты ошибок; временные каталоги
// копии убраны.
func TestBackupPasswordNeverLogged(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)

	var errTexts []string
	note := func(err error) {
		if err != nil {
			errTexts = append(errTexts, err.Error())
		}
	}
	_, err := b.preview(exp.Data, bkPassword)
	note(err)
	_, err = b.importData(exp.Data, bkPassword+"-wrong", true)
	note(err)
	_, err = b.importData(exp.Data, bkPassword, false)
	note(err)
	_, err = b.importData(exp.Data, bkPassword, true)
	note(err)
	_, err = b.backup.ExportBackup(context.Background(), connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: "short"}))
	note(err)

	for _, pw := range []string{bkPassword, bkPassword + "-wrong", "short"} {
		for _, l := range append(a.logs.Tail(0), b.logs.Tail(0)...) {
			if strings.Contains(l.Message, pw) {
				t.Errorf("пароль %q в журнале: %s", pw, l.Message)
			}
		}
		for _, text := range errTexts {
			if strings.Contains(text, pw) && pw != "short" {
				t.Errorf("пароль %q в тексте ошибки: %s", pw, text)
			}
		}
	}
	for _, fx := range []*backupFixture{a, b} {
		_, _ = fx.st.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		assertPasswordNotPersisted(t, fx, bkPassword)
	}
	if len(b.logs.Tail(0)) == 0 {
		t.Error("журнал пуст: тест ничего не проверяет")
	}
}

func assertPasswordNotPersisted(t *testing.T, f *backupFixture, password string) {
	t.Helper()
	err := filepath.WalkDir(f.st.Dir(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && path != f.st.Dir() {
				t.Errorf("остался временный каталог %s", path)
			}
			return err
		}
		raw, rerr := os.ReadFile(path)
		if rerr == nil && bytes.Contains(raw, []byte(password)) {
			t.Errorf("пароль найден в файле %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestBackupLimitAboveGeneralCap — B: 20 МиБ проходят в BackupService (лимит
// 64 МиБ) и не проходят в ProfileService (общий лимит 16 МиБ).
func TestBackupLimitAboveGeneralCap(t *testing.T) {
	f := newBackupFixture(t)
	_, err := f.profile.ImportContent(context.Background(), connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content: strings.Repeat("A", 20<<20)}))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("ProfileService 20 МиБ: %v, ждали RESOURCE_EXHAUSTED", err)
	}
	_, err = f.preview(make([]byte, 20<<20), "password-1234")
	wantReason(t, err, connect.CodeInvalidArgument, reasonNotRecognized) // дошло до разбора
}

// TestBackupSizeLimitIs64MiB — B: ровно 64 МиБ доходят до разбора, на байт
// больше — RESOURCE_EXHAUSTED с причиной.
func TestBackupSizeLimitIs64MiB(t *testing.T) {
	f := newBackupFixture(t)
	_, err := f.preview(make([]byte, 64<<20), "password-1234")
	wantReason(t, err, connect.CodeInvalidArgument, reasonNotRecognized)
	_, err = f.preview(make([]byte, 64<<20+1), "password-1234")
	wantReason(t, err, connect.CodeResourceExhausted, reasonTooLarge)
	_, err = f.importData(make([]byte, 64<<20+1), "password-1234", true)
	wantReason(t, err, connect.CodeResourceExhausted, reasonTooLarge)
}

// TestConcurrentBackupOpsSerialized — C: одновременно идёт одна операция,
// остальные получают ABORTED backup_busy, после завершения путь свободен.
func TestConcurrentBackupOpsSerialized(t *testing.T) {
	f := newBackupFixture(t)
	seedBackupData(t, f.st, "A")
	exp := f.export(t)

	var blocked atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock) // упавший тест не должен оставить обработчик висеть
	f.srv.backup.onAcquired = func() {
		if blocked.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() { _, err := f.preview(exp.Data, bkPassword); done <- err }()
	<-entered

	_, err := f.backup.ExportBackup(context.Background(), connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: bkPassword}))
	wantReason(t, err, connect.CodeAborted, reasonBusy)
	_, err = f.importData(exp.Data, bkPassword, true)
	wantReason(t, err, connect.CodeAborted, reasonBusy)
	unblock()
	if err := <-done; err != nil {
		t.Fatalf("первая операция: %v", err)
	}
	if _, err := f.importData(exp.Data, bkPassword, true); err != nil {
		t.Fatalf("после освобождения: %v", err)
	}
}

// TestBackupOpsNeverOverlap — C: при гонке многих вызовов внутри операции
// никогда не бывает двух сразу.
func TestBackupOpsNeverOverlap(t *testing.T) {
	f := newBackupFixture(t)
	seedBackupData(t, f.st, "A")
	var active, maxActive atomic.Int32
	f.srv.backup.onAcquired = func() {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		active.Add(-1)
	}
	var wg sync.WaitGroup
	var ok, busy atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.backup.ExportBackup(context.Background(),
				connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: bkPassword}))
			switch {
			case err == nil:
				ok.Add(1)
			case connect.CodeOf(err) == connect.CodeAborted && errorReason(err) == reasonBusy:
				busy.Add(1)
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()
	if maxActive.Load() != 1 {
		t.Errorf("одновременно шло %d операций", maxActive.Load())
	}
	if ok.Load() < 1 || ok.Load()+busy.Load() != 12 {
		t.Errorf("успешно %d, занято %d из 12", ok.Load(), busy.Load())
	}
}

func TestImportRefusedWhileSubscriptionUpdateRuns(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)
	b.srv.subsMu.Lock()
	b.srv.subsRunning[5] = true
	b.srv.subsMu.Unlock()
	_, err := b.importData(exp.Data, bkPassword, true)
	wantReason(t, err, connect.CodeAborted, reasonBusy)
	if got := profileNames(t, b.st); !got["vm-B"] {
		t.Errorf("данные изменились: %v", got)
	}
}

// TestRestoreNotifiesSettingsSubscribers — после восстановления подписчики
// настроек получают SettingsChanged: изменённые ключи и ключи, исчезнувшие
// (сброшены к умолчанию); update.* и служебные ключи в событие не входят.
func TestRestoreNotifiesSettingsSubscribers(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	for k, v := range map[string]string{"ui.lang": `"xx"`, "update.serial_stable": `5`, "sub.hwid_id": `"hw"`} {
		if err := b.st.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	exp := a.export(t)

	ch := make(chan *lavrayv1.SettingsChanged, 4)
	b.srv.setMu.Lock()
	b.srv.setSubs[ch] = struct{}{}
	b.srv.setMu.Unlock()

	if _, err := b.importData(exp.Data, bkPassword, true); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if got := ev.Changed["ui.theme"].GetStringValue(); got != "theme-A" {
			t.Errorf("changed[ui.theme] = %q", got)
		}
		if !contains(ev.Removed, "ui.lang") {
			t.Errorf("removed = %v, ждали ui.lang", ev.Removed)
		}
		for k := range ev.Changed {
			if strings.HasPrefix(k, "update.") || k == "sub.hwid_id" {
				t.Errorf("в событии лишний ключ %s", k)
			}
		}
		for _, k := range ev.Removed {
			if strings.HasPrefix(k, "update.") || k == "sub.hwid_id" {
				t.Errorf("в removed лишний ключ %s", k)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("подписчик не получил SettingsChanged")
	}
	// неудачное восстановление события не шлёт
	if _, err := b.importData(exp.Data, "wrong-password-1", true); err == nil {
		t.Fatal("ждали ошибку")
	}
	select {
	case ev := <-ch:
		t.Fatalf("событие при ошибке: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestRestoreRestartsSubscriptionScheduler — после восстановления планировщик
// подписок запущен заново (его состояние привязано к прежним id групп), а
// после остановки ядра не оживает.
func TestRestoreRestartsSubscriptionScheduler(t *testing.T) {
	a, b := newBackupFixture(t), newBackupFixture(t)
	seedBackupData(t, a.st, "A")
	seedBackupData(t, b.st, "B")
	exp := a.export(t)

	stop := b.srv.StartSubscriptionScheduler(context.Background())
	if n := b.srv.sched.starts(); n != 1 {
		t.Fatalf("запусков до восстановления: %d", n)
	}
	if _, err := b.importData(exp.Data, bkPassword, true); err != nil {
		t.Fatal(err)
	}
	if n := b.srv.sched.starts(); n != 2 {
		t.Errorf("запусков после восстановления: %d, ждали 2", n)
	}
	// неудачное восстановление планировщик не трогает
	if _, err := b.importData(exp.Data, "wrong-password-1", true); err == nil {
		t.Fatal("ждали ошибку")
	}
	if n := b.srv.sched.starts(); n != 2 {
		t.Errorf("запусков после неудачи: %d, ждали 2", n)
	}
	stop()
	if _, err := b.importData(exp.Data, bkPassword, true); err != nil {
		t.Fatal(err)
	}
	if n := b.srv.sched.starts(); n != 2 {
		t.Errorf("планировщик ожил после остановки: %d запусков", n)
	}
}

// TestAgentDeniedBackup — S: ни в одном режиме агент не вызывает копию.
func TestAgentDeniedBackup(t *testing.T) {
	f := newAgentFixture(t)
	bc := lavrayv1connect.NewBackupServiceClient(&http.Client{Transport: bearerRT{testAgentToken}}, f.url)
	calls := map[string]func() error{
		"ExportBackup": func() error {
			_, err := bc.ExportBackup(context.Background(), connect.NewRequest(&lavrayv1.ExportBackupRequest{Password: "password-1234"}))
			return err
		},
		"PreviewBackup": func() error {
			_, err := bc.PreviewBackup(context.Background(), connect.NewRequest(&lavrayv1.PreviewBackupRequest{Password: "password-1234"}))
			return err
		},
		"ImportBackup": func() error {
			_, err := bc.ImportBackup(context.Background(), connect.NewRequest(&lavrayv1.ImportBackupRequest{Password: "password-1234", ConfirmReplace: true}))
			return err
		},
	}
	for _, mode := range []string{AgentModeOff, AgentModeRead, AgentModeControl} {
		f.setMode(t, mode)
		for name, call := range calls {
			if err := call(); !denied(err) {
				t.Errorf("режим %s: %s не отклонён: %v", mode, name, err)
			}
		}
	}
}
