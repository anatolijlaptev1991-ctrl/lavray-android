# Архитектура LAVRay для Android (контракт для разработки)

## Раскладка репозитория

```
third_party/lavray/        субмодуль, тег v0.9.3 (только чтение)
core-overlay/              Go-код, копируется в build/go/core/mobile при сборке
scripts/build-core.sh      собирает app/libs/lavray-core.aar
tools/                     генераторы (DTO из proto, словари, иконки)
app/                       Android-приложение (Kotlin, Compose)
docs/                      записки, замеры, чек-листы
```

Идентификатор приложения: `io.github.anatolijlaptev1991_ctrl.lavray` (как у десктопа, дефис заменён: в Android он недопустим).
minSdk 26, targetSdk и compileSdk 36. ABI: arm64-v8a, armeabi-v7a, x86_64. Версия 0.9.3.

## Контракт Go ↔ Kotlin (пакет gomobile `mobile`, Java-пакет `lavray.core.mobile`: классы `Mobile`, `Options`, `Platform`, `NetworkListener`; библиотека `libgojni.so`)

Сложные структуры передаются строкой JSON: это избавляет от итераторов gomobile.

```go
package mobile

// Platform реализует Kotlin (процесс :vpn).
type Platform interface {
    // OpenTun вызывает VpnService.Builder.establish и возвращает дескриптор.
    // optionsJSON: {mtu, inet4:[cidr], inet6:[cidr], dns:[ip], includePackages:[], excludePackages:[],
    //   route4:[cidr], route6:[cidr]} (route* считает sing-box; без них добавьте 0.0.0.0/0 и ::/0).
    // Go делает dup дескриптора: Kotlin остаётся владельцем исходного и закрывает его сам.
    OpenTun(optionsJSON string) (int32, error)
    // Protect вызывает VpnService.protect(fd).
    Protect(fd int32) error
    // FindOwnerUid вызывает ConnectivityManager.getConnectionOwnerUid (API 29+), -1 если нет.
    FindOwnerUid(proto int32, srcIP string, srcPort int32, dstIP string, dstPort int32) (int32, error)
    PackagesForUid(uid int32) string // пакеты через запятую
    // Монитор сети по умолчанию; listener вызывается при каждой смене.
    StartNetworkMonitor(l NetworkListener) error
    CloseNetworkMonitor() error
    // Interfaces возвращает JSON-массив {name,index,mtu,addrs:[cidr],flags,metered,type};
    // flags — маска IFF_* Linux (нет поля: UP|RUNNING|MULTICAST), type — "wifi"|"cellular"|"ethernet"|"other",
    // необязательные dns:[ip] и gateway:[ip] попадают в Environment() локального DNS.
    Interfaces() (string, error)
}

type NetworkListener interface {
    // name "" и index -1 означают отсутствие сети.
    Update(name string, index int32, metered bool)
}

type Options struct {
    DataDir, TempDir, SocketPath, AuthToken string
    MemoryLimitMB int32 // 0 — без ограничения (debug.SetMemoryLimit)
    TimeZone      string // IANA, java.util.TimeZone.getDefault().id; у Go на Android нет /etc/localtime
}

func Start(o *Options, p Platform) error // синхронно: возвращается, когда API готов
func Stop()                              // корректное завершение, идемпотентно
func Version() string
func SetTun(stack string, mtu int32) // stack и MTU из настроек приложения, действуют со следующего подключения
```

Не ожидайте от `Start` режима «системный прокси»: ядро запускается только с tun-inbound (подключение `CONNECT_MODE_TUN`), иначе Connect вернёт ошибку.

Подмешивание платформы: HTTP-обёртка с `service.ContextWith` не годится. Значения контекста переживают `context.WithoutCancel`, но `ContextWith` кладёт в контекст общий изменяемый реестр служб sing-box; временные инстансы проверки задержки наследуют контекст одного RPC и работают параллельно, так что делили бы и затирали Router, журнал и NetworkManager друг друга. Поэтому каждый экземпляр (рабочий и временный) получает контекст с НОВЫМ реестром, платформой и реестром DNS-транспортов, где `local` заменён системным резолвером Android. Рабочий раннер делает это в `Start`, временные инстансы получают фабрику `api.Server.newTestInst` (устанавливается при `Mobile.start`). Один слушатель сети Kotlin обслуживает все экземпляры (хаб).

Обёртка раннера над `inst.SingBoxRunner` приводит конфиг к правилам Android перед стартом:
- удаляется `mixed-in` и любой слушатель на localhost: другие приложения на телефоне не должны видеть локальный прокси;
- из правил убираются условия `process_name` и `process_path` (на телефоне они ничего не выбирают, а поиск владельца соединения стоит вызова Binder);
- у `tun-in` убирается `interface_name`, значения `stack` и `mtu` берутся из `Mobile.setTun`;
- также убираются `experimental.clash_api` и `experimental.v2ray_api` (локальные слушатели), `route.default_interface` и `route.default_mark`; `auto_detect_interface` остаётся: при платформе он включает `Protect(fd)` на каждый исходящий сокет, а к интерфейсу не привязывает;
- правило, оставшееся без условий после снятия `process_*`, удаляется;
- поиск владельца соединения (`FindOwnerUid`) выполняется, только если в конфиге есть `package_name`, `user`, `user_id` или `find_process`; иначе Binder не вызывается.

## Процессы и запуск

`LavrayVpnService : VpnService` в процессе `:vpn`, FGS-тип `specialUse` (документация не запрещает его запуск из `BOOT_COMPLETED`; `systemExempted` бросает исключение, если согласия на VPN нет).

`onBind`: для `VpnService.SERVICE_INTERFACE` — `super.onBind`, иначе собственный `Binder` с `endpoint()` (путь сокета и токен).

Ядро Go стартует лениво при первом `onBind` или `onStartCommand`. Когда VPN выключен и интерфейс отвязался, сервис вызывает `Mobile.stop()` и завершает процесс, чтобы вернуть память рантайма Go.

Своё приложение исключается из туннеля (`addDisallowedApplication(packageName)`), поэтому сокеты ядра и временные инстансы проверки не попадают в петлю. В режиме белого списка приложений это выполняется автоматически.

## Автоподключение

- `BootReceiver` в процессе `:vpn`, `BOOT_COMPLETED`. Настройка «Подключаться при запуске телефона» хранится в SharedPreferences процесса `:vpn`.
- `VpnService.prepare() != null`: согласие утеряно. Сервис не стартует, показывается уведомление «Нужно разрешение VPN». Вызовы из Receiver документацией не описаны, поведение проверяется на эмуляторе.
- Запуск без нашего extra считается запуском системы (Always-on), такой запуск тоже подключает последний сервер.
- Манифест не отключает `SUPPORTS_ALWAYS_ON`.
- Ожидание сети: `NetworkCallback` по `NET_CAPABILITY_INTERNET` и `NOT_VPN`. Без опроса, без WakeLock и без AlarmManager.
- Повторы после сбоя: 5 с, 15 с, 45 с, 2 мин, далее 5 мин с разбросом ±20%; появление сети сбрасывает задержку; ручное «Отключить» отменяет всё.

## Клиент API в Kotlin

HTTP/1.1 поверх `android.net.LocalSocket`: unary `application/json`, серверные потоки `application/connect+json` (конверт 5 байт, chunked). DTO генерируются из `proto/lavray.proto` скриптом `tools/protogen` в `@Serializable` классы (proto3 JSON: camelCase, int64 строкой, enum строкой).

## Что не переносится

Перечень с причинами ведётся в `docs/NOT-PORTED.md`.
