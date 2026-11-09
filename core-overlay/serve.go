// SPDX-License-Identifier: GPL-3.0-only

package mobile

// serve.go — путь `serve` из cmd/lavray-core/main.go без того, что на телефоне
// не имеет смысла: агент и MCP, веб-панель, шлюз, протокол stdin, обновление
// приложения (Updater nil, планировщик обновлений не запускается) и проверка
// прав на TUN (TunCheck nil; право даёт VpnService).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/api"
	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/store"
)

// Лимиты HTTP-сервера (cmd/lavray-core/httpserver.go). ReadTimeout и
// WriteTimeout не задаются: серверные потоки живут часами.
const (
	httpReadHeaderTimeout = 10 * time.Second
	httpIdleTimeout       = 2 * time.Minute
	httpMaxHeaderBytes    = 64 << 10

	// maxUnixPath — предел sockaddr_un.sun_path (108 с завершающим нулём).
	maxUnixPath = 107

	instanceStopTimeout = 12 * time.Second
	httpDrainTimeout    = 2 * time.Second
)

// instance — запущенное ядро.
type instance struct {
	st        *store.Store
	server    *api.Server
	httpSrv   *http.Server
	ln        net.Listener
	sockPath  string
	stopSched func()
	mux       *logs.Mux
	serveDone chan struct{}
}

func validateOptions(o *Options, p Platform) error {
	switch {
	case o == nil:
		return errors.New("mobile: Options обязателен")
	case p == nil:
		return errors.New("mobile: Platform обязателен")
	case o.SocketPath == "":
		return errors.New("mobile: SocketPath обязателен")
	case len(o.SocketPath) > maxUnixPath:
		return fmt.Errorf("mobile: SocketPath длиннее %d байт (предел unix-сокета)", maxUnixPath)
	case o.AuthToken == "":
		return errors.New("mobile: AuthToken обязателен")
	}
	return nil
}

// startInstance поднимает ядро; при любой ошибке освобождает то, что успело открыться.
func startInstance(o *Options, p Platform) (*instance, error) {
	if err := validateOptions(o, p); err != nil {
		return nil, err
	}
	if err := prepareEnv(o); err != nil {
		return nil, err
	}
	if err := os.Chdir(o.DataDir); err != nil {
		return nil, fmt.Errorf("mobile: chdir: %w", err)
	}
	applyMemoryLimit(o.MemoryLimitMB)
	mux := logs.NewMux(1000)
	if err := applyTimeZone(o.TimeZone); err != nil {
		mux.Logf(logs.LevelWarn, "core", "%v (время в журнале по UTC)", err)
	}
	mux.Logf(logs.LevelInfo, "core", "часовой пояс ядра: %s (UTC%s)", time.Local, time.Now().Format("-07:00"))
	mux.Logf(logs.LevelInfo, "core", "lavray-core (android) запускается: data=%s", o.DataDir)
	if err := presets.EnsureGeo(o.DataDir); err != nil {
		mux.Logf(logs.LevelWarn, "core", "geo-базы: %v (правила geosite/geoip могут не работать)", err)
	}
	st, err := store.Open(o.DataDir)
	if err != nil {
		return nil, fmt.Errorf("mobile: хранилище: %w", err)
	}
	if err := applyAndroidDefaults(st); err != nil {
		_ = st.Close()
		return nil, err
	}
	ln, err := listenUnix(o.SocketPath)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	inst, err := assemble(o, p, st, ln, mux)
	if err != nil {
		_ = ln.Close()
		_ = os.Remove(o.SocketPath)
		_ = st.Close()
		return nil, err
	}
	return inst, nil
}

// listenUnix открывает сокет с правами 0600 (каталог приложения приватный, но
// доступ ограничивается и самим файлом).
func listenUnix(path string) (net.Listener, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("mobile: сокет %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("mobile: права сокета: %w", err)
	}
	return ln, nil
}

func assemble(o *Options, p Platform, st *store.Store, ln net.Listener, mux *logs.Mux) (*instance, error) {
	h := newHost(p)
	server := api.NewServer(api.Deps{Store: st, Logs: mux, Runner: newRunner(h)})
	installTestFactory(server, h.testFactory())
	stopSched := server.StartSubscriptionScheduler(context.Background())

	root := http.NewServeMux()
	server.Register(root)
	httpSrv := &http.Server{
		Handler:           authMiddleware(root, o.AuthToken),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
	inst := &instance{
		st: st, server: server, httpSrv: httpSrv, ln: ln, sockPath: o.SocketPath,
		stopSched: stopSched, mux: mux, serveDone: make(chan struct{}),
	}
	// RPC CoreService.Shutdown останавливает инстанс sing-box сам; остановку
	// остального выполняет Stop() (в горутине: хендлер ещё держит запрос).
	server.OnShutdown = func(context.Context) { go Stop() }
	go func() {
		defer close(inst.serveDone)
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mux.Logf(logs.LevelError, "core", "serve: %v", err)
		}
	}()
	mux.Logf(logs.LevelInfo, "core", "готов: unix://%s", o.SocketPath)
	return inst, nil
}

// stop: планировщик, sing-box (TUN снимается до закрытия HTTP), HTTP,
// хранилище, файл сокета. Порядок как в gracefulExit основного бинаря.
func (i *instance) stop() {
	i.stopSched()
	ctx, cancel := context.WithTimeout(context.Background(), instanceStopTimeout)
	defer cancel()
	if err := i.server.StopInstance(ctx); err != nil {
		i.mux.Logf(logs.LevelError, "core", "остановка инстанса: %v", err)
	}
	drain, cancelDrain := context.WithTimeout(context.Background(), httpDrainTimeout)
	defer cancelDrain()
	if err := i.httpSrv.Shutdown(drain); err != nil {
		_ = i.httpSrv.Close()
	}
	<-i.serveDone
	if err := i.st.Close(); err != nil {
		i.mux.Logf(logs.LevelError, "core", "закрытие хранилища: %v", err)
	}
	_ = os.Remove(i.sockPath)
}
