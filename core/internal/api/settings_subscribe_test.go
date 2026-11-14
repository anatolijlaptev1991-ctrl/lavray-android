// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// settingsRig — настоящий сервер и клиент настроек поверх httptest.
type settingsRig struct {
	srv      *Server
	settings lavrayv1connect.SettingsServiceClient
}

func newSettingsRig(t *testing.T) *settingsRig {
	t.Helper()
	var srv *Server
	_, httpSrv := newE2EServer(t, Deps{Runner: &inst.FakeRunner{}}, func(s *Server) { srv = s })
	return &settingsRig{srv: srv, settings: lavrayv1connect.NewSettingsServiceClient(httpSrv.Client(), httpSrv.URL)}
}

func (r *settingsRig) setPort(t *testing.T, port int64) {
	t.Helper()
	_, err := r.settings.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{
			"inbound.port": {Value: &lavrayv1.SettingValue_IntValue{IntValue: port}},
		},
	}))
	if err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
}

// settingsSub — клиентская сторона потока: события читает фоновая горутина.
// Вызов SubscribeSettings возвращается, когда сервер отправил заголовки, а это
// происходит вместе с первым событием, поэтому сам вызов тоже идёт в фоне.
type settingsSub struct {
	events chan *lavrayv1.SettingsChanged
	done   chan error
}

// subscribe открывает поток и ждёт, пока сервер зарегистрирует подписчика.
func (r *settingsRig) subscribe(t *testing.T, ctx context.Context) *settingsSub {
	t.Helper()
	before := r.srv.settingsSubscribers()
	sub := &settingsSub{events: make(chan *lavrayv1.SettingsChanged, 64), done: make(chan error, 1)}
	go func() {
		stream, err := r.settings.SubscribeSettings(ctx, connect.NewRequest(&lavrayv1.SubscribeSettingsRequest{}))
		if err != nil {
			sub.done <- err
			return
		}
		for stream.Receive() {
			sub.events <- stream.Msg()
		}
		sub.done <- stream.Err()
	}()
	waitForSettings(t, "регистрация подписчика", func() bool { return r.srv.settingsSubscribers() == before+1 })
	return sub
}

// waitForSettings опрашивает условие до 3 с.
func waitForSettings(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// next ждёт следующее событие потока до 3 с.
func (sub *settingsSub) next(t *testing.T) *lavrayv1.SettingsChanged {
	t.Helper()
	select {
	case ev := <-sub.events:
		return ev
	case err := <-sub.done:
		t.Fatalf("поток закрыт: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("событие не пришло за 3 с")
	}
	return nil
}

// SetSettings приходит подписчику как SettingsChanged с типизированным значением.
func TestSubscribeSettingsChanges(t *testing.T) {
	r := newSettingsRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := r.subscribe(t, ctx)

	r.setPort(t, 3000)
	ev := stream.next(t)
	if v := ev.Changed["inbound.port"].GetIntValue(); v != 3000 || len(ev.Removed) != 0 {
		t.Fatalf("event = %v", ev)
	}
	// Та же запись второй раз значение не меняет — события нет.
	r.setPort(t, 3000)
	r.setPort(t, 3001)
	if v := stream.next(t).Changed["inbound.port"].GetIntValue(); v != 3001 {
		t.Fatalf("после повтора пришло значение %d, want 3001", v)
	}
}

// ResetSettings сообщает сброшенные ключи в removed.
func TestSubscribeSettingsRemovedOnReset(t *testing.T) {
	r := newSettingsRig(t)
	r.setPort(t, 3000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := r.subscribe(t, ctx)

	_, err := r.settings.ResetSettings(ctx, connect.NewRequest(&lavrayv1.ResetSettingsRequest{Keys: []string{"inbound.port"}}))
	if err != nil {
		t.Fatal(err)
	}
	ev := stream.next(t)
	if len(ev.Removed) != 1 || ev.Removed[0] != "inbound.port" || len(ev.Changed) != 0 {
		t.Fatalf("event = %v", ev)
	}
}

// Отмена потока клиентом снимает подписку.
func TestSubscribeSettingsCancelUnsubscribes(t *testing.T) {
	r := newSettingsRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	stream := r.subscribe(t, ctx)
	_ = stream

	cancel()
	waitForSettings(t, "снятие подписки после отмены", func() bool { return r.srv.settingsSubscribers() == 0 })
	// Публикация после отписки безопасна.
	r.srv.publishSettingsChanged(&lavrayv1.SettingsChanged{Removed: []string{"x"}})
}

// Несколько клиентов видят одно и то же; уход одного другим не мешает.
func TestSubscribeSettingsMultiClient(t *testing.T) {
	r := newSettingsRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaveCtx, leave := context.WithCancel(ctx)
	streams := []*settingsSub{
		r.subscribe(t, ctx), r.subscribe(t, ctx), r.subscribe(t, leaveCtx),
	}

	r.setPort(t, 4000)
	for i, s := range streams {
		if v := s.next(t).Changed["inbound.port"].GetIntValue(); v != 4000 {
			t.Fatalf("клиент %d получил %d", i, v)
		}
	}
	leave()
	waitForSettings(t, "уход третьего клиента", func() bool { return r.srv.settingsSubscribers() == 2 })
	r.setPort(t, 4001)
	for i, s := range streams[:2] {
		if v := s.next(t).Changed["inbound.port"].GetIntValue(); v != 4001 {
			t.Fatalf("оставшийся клиент %d получил %d", i, v)
		}
	}
}

// slowSender — send, который блокируется до release и запоминает события.
type slowSender struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	got     []*lavrayv1.SettingsChanged
}

func newSlowSender() *slowSender {
	return &slowSender{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (s *slowSender) send(ev *lavrayv1.SettingsChanged) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	s.mu.Lock()
	s.got = append(s.got, ev)
	s.mu.Unlock()
	return nil
}

func (s *slowSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func eventN(i int) *lavrayv1.SettingsChanged {
	return &lavrayv1.SettingsChanged{Removed: []string{string(rune('a' + i%26))}}
}

// Медленный подписатель не тормозит публикацию и после переполнения буфера
// (16) закрывается с ABORTED; быстрый подписчик получает всё.
func TestSubscribeSettingsSlowSubscriberDropped(t *testing.T) {
	r := newSettingsRig(t)
	srv := r.srv
	slow := newSlowSender()
	slowDone := make(chan error, 1)
	go func() { slowDone <- srv.serveSettings(context.Background(), slow.send) }()

	var fastGot int32
	var fastMu sync.Mutex
	fastCtx, fastCancel := context.WithCancel(context.Background())
	defer fastCancel()
	go func() {
		_ = srv.serveSettings(fastCtx, func(*lavrayv1.SettingsChanged) error {
			fastMu.Lock()
			fastGot++
			fastMu.Unlock()
			return nil
		})
	}()
	waitForSettings(t, "два подписчика", func() bool { return srv.settingsSubscribers() == 2 })

	// publish отдаёт событие и ждёт, пока быстрый подписчик его заберёт: иначе
	// его собственный буфер мог бы переполниться из-за планировщика.
	fastCount := func() int32 {
		fastMu.Lock()
		defer fastMu.Unlock()
		return fastGot
	}
	published := int32(0)
	publish := func(ev *lavrayv1.SettingsChanged) {
		srv.publishSettingsChanged(ev)
		published++
		waitForSettings(t, "быстрый забрал событие", func() bool { return fastCount() == published })
	}
	// Первое событие забирает обработчик и застревает в send; ещё 16 лежат в
	// буфере и подписчика не снимают.
	publish(eventN(0))
	<-slow.entered
	for i := 1; i <= settingsSubBuffer; i++ {
		publish(eventN(i))
	}
	if n := srv.settingsSubscribers(); n != 2 {
		t.Fatalf("при заполненном буфере подписчиков %d, want 2", n)
	}
	// Следующее переполняет буфер: подписчик снят, публикация не заблокировалась.
	done := make(chan struct{})
	go func() {
		publish(eventN(settingsSubBuffer + 1))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("публикация заблокирована медленным подписчиком")
	}
	if n := srv.settingsSubscribers(); n != 1 {
		t.Fatalf("после переполнения подписчиков %d, want 1", n)
	}

	close(slow.release)
	select {
	case err := <-slowDone:
		if connect.CodeOf(err) != connect.CodeAborted {
			t.Fatalf("медленный поток завершился %v, want ABORTED", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("медленный поток не завершился")
	}
	if slow.count() != settingsSubBuffer+1 {
		t.Fatalf("медленный получил %d событий, want %d (буфер доставлен до закрытия)", slow.count(), settingsSubBuffer+1)
	}
	if got := fastCount(); got != int32(settingsSubBuffer+2) {
		t.Fatalf("быстрый получил %d событий, want %d", got, settingsSubBuffer+2)
	}
}

// Ошибка отправки (клиент ушёл) завершает обработчик и снимает подписку.
func TestSubscribeSettingsSendErrorUnsubscribes(t *testing.T) {
	r := newSettingsRig(t)
	boom := errors.New("write: broken pipe")
	done := make(chan error, 1)
	go func() {
		done <- r.srv.serveSettings(context.Background(), func(*lavrayv1.SettingsChanged) error { return boom })
	}()
	waitForSettings(t, "подписчик", func() bool { return r.srv.settingsSubscribers() == 1 })

	r.srv.publishSettingsChanged(eventN(0))
	if err := <-done; !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	waitForSettings(t, "снятие подписки", func() bool { return r.srv.settingsSubscribers() == 0 })
}
