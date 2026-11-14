// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"slices"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

func (f *svcFixture) connect(t *testing.T, in *lavrayv1.ConnectRequest) error {
	t.Helper()
	_, err := f.srv.Connect(context.Background(), newReq(in))
	return err
}

func (f *svcFixture) disconnect(t *testing.T) {
	t.Helper()
	if _, err := f.srv.Disconnect(context.Background(), newReq(&lavrayv1.DisconnectRequest{})); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
}

// remembered — что записано в remember.id и remember.modes (id -1 — не задан).
func (f *svcFixture) remembered(t *testing.T) (int64, []string) {
	t.Helper()
	return int64(f.srv.settingInt("remember.id")), f.srv.settingStringList("remember.modes")
}

// Успешный Connect по profile_id запоминает профиль и режим — даже без
// remember.enable: он управляет только автоподключением при старте.
func TestConnectRemembers(t *testing.T) {
	f := newSvcFixture(t, nil)
	first := f.addTrojan(t, "one", "one.example.org", 443, "1")
	second := f.addTrojan(t, "two", "two.example.org", 443, "2")

	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &first.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY}); err != nil {
		t.Fatal(err)
	}
	if id, modes := f.remembered(t); id != first.ID || !slices.Equal(modes, []string{"system_proxy"}) {
		t.Fatalf("после первого Connect: id=%d modes=%v", id, modes)
	}
	f.disconnect(t)
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &second.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_TUN}); err != nil {
		t.Fatal(err)
	}
	if id, modes := f.remembered(t); id != second.ID || !slices.Equal(modes, []string{"tun"}) {
		t.Fatalf("после второго Connect: id=%d modes=%v", id, modes)
	}
}

// Режим берётся по факту: без mode запоминается тот, в котором подключились.
func TestConnectRemembersEffectiveMode(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.addTrojan(t, "one", "one.example.org", 443, "1")
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID}); err != nil {
		t.Fatal(err)
	}
	if _, modes := f.remembered(t); !slices.Equal(modes, []string{"system_proxy"}) {
		t.Fatalf("modes = %v", modes)
	}
}

// Inline-профиля нет в базе: запоминать нечего, прежняя запись не затирается.
func TestInlineNotRemembered(t *testing.T) {
	f := newSvcFixture(t, nil)
	saved := f.addTrojan(t, "saved", "saved.example.org", 443, "1")
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &saved.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_TUN}); err != nil {
		t.Fatal(err)
	}
	f.disconnect(t)

	inline := &lavrayv1.Profile{
		Type: lavrayv1.ProfileType_PROFILE_TYPE_TROJAN, Name: "inline", Address: "inline.example.org", Port: 443,
		Detail: &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{Password: "pw"}},
	}
	if err := f.connect(t, &lavrayv1.ConnectRequest{InlineProfile: inline, Mode: lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY}); err != nil {
		t.Fatal(err)
	}
	if id, modes := f.remembered(t); id != saved.ID || !slices.Equal(modes, []string{"tun"}) {
		t.Fatalf("inline затёр запомненное: id=%d modes=%v", id, modes)
	}
}

// Сбой подключения и пробная сборка ничего не запоминают.
func TestConnectNotRememberedOnFailureOrDryRun(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.addTrojan(t, "one", "one.example.org", 443, "1")
	dry := true
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID, ValidateOnly: &dry}); err != nil {
		t.Fatal(err)
	}
	f.runner.StartErr = errors.New("boom")
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID}); err == nil {
		t.Fatal("ожидался сбой запуска")
	}
	if id, modes := f.remembered(t); id != -1 || len(modes) != 0 {
		t.Fatalf("запомнено при неудаче: id=%d modes=%v", id, modes)
	}
}

func (f *svcFixture) enableRemember(t *testing.T) {
	t.Helper()
	if err := f.srv.st.SetSetting("remember.enable", "true"); err != nil {
		t.Fatal(err)
	}
}

// Headless после падения или перезапуска подключает запомненный профиль в
// запомненном режиме.
func TestHeadlessReconnectsRemembered(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.addTrojan(t, "other", "other.example.org", 443, "0")
	p := f.addTrojan(t, "one", "one.example.org", 443, "1")
	f.enableRemember(t)
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_TUN}); err != nil {
		t.Fatal(err)
	}
	f.disconnect(t)
	if f.srv.StatusAlive() {
		t.Fatal("после Disconnect соединение живо")
	}

	if err := f.srv.ConnectLast(context.Background()); err != nil {
		t.Fatalf("ConnectLast: %v", err)
	}
	st := f.srv.inst.Status()
	if !f.srv.StatusAlive() || st.Run == nil || st.Run.ProfileID != p.ID || st.Run.Mode != "tun" {
		t.Fatalf("после ConnectLast: alive=%v run=%+v", f.srv.StatusAlive(), st.Run)
	}
	// Повтор при уже работающем соединении — успех, не ошибка.
	if err := f.srv.ConnectLast(context.Background()); err != nil {
		t.Fatalf("повторный ConnectLast: %v", err)
	}
}

// Без remember.enable автоподключения нет, запись при этом ведётся.
func TestHeadlessNeedsRememberEnable(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.addTrojan(t, "one", "one.example.org", 443, "1")
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID}); err != nil {
		t.Fatal(err)
	}
	f.disconnect(t)
	if err := f.srv.ConnectLast(context.Background()); !errors.Is(err, ErrNoRememberedProfile) {
		t.Fatalf("err = %v, want ErrNoRememberedProfile", err)
	}
}

// Запомненный профиль удалён: автоподключение молча пропускается, ядро живо.
func TestRememberedDeletedIgnored(t *testing.T) {
	f := newSvcFixture(t, nil)
	gone := f.addTrojan(t, "gone", "gone.example.org", 443, "1")
	f.enableRemember(t)
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &gone.ID}); err != nil {
		t.Fatal(err)
	}
	f.disconnect(t)
	if err := f.srv.st.DeleteProfile(gone.ID); err != nil {
		t.Fatal(err)
	}

	if err := f.srv.ConnectLast(context.Background()); !errors.Is(err, ErrNoRememberedProfile) {
		t.Fatalf("err = %v, want ErrNoRememberedProfile", err)
	}
	if f.srv.StatusAlive() {
		t.Fatal("подключился несуществующий профиль")
	}
	// Остальное по-прежнему работает: подключение другого профиля.
	other := f.addTrojan(t, "other", "other.example.org", 443, "2")
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &other.ID}); err != nil {
		t.Fatal(err)
	}
	if id, _ := f.remembered(t); id != other.ID {
		t.Fatalf("remember.id = %d, want %d", id, other.ID)
	}
}
