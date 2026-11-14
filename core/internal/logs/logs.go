// SPDX-License-Identifier: GPL-3.0-only

// Package logs — мультиплексор логов ядра LAVRay (W6, core-port-plan §2
// logging/): единая точка входа строк лога с РЕДАКЦИЕЙ ДО записи в ring-буфер
// и ДО доставки подписчикам (LNB-01/28: секреты не попадают ни в файл, ни к
// клиенту). Подписки — каналы (W7 подвязывает LogService/web/WS поверх
// Subscribe/Tail).
//
// Правила (BUGS.md LNB-01, LNB-28, fix-план п.1 «Redaction-слой»):
//   - userinfo URL (логин:пароль@) скрывается целиком;
//   - query-параметры с секретными именами (token/key/secret/password/…) —
//     значение заменяется на [REDACTED];
//   - JSON-поля password/token/uuid/secret/private_key/auth — значение
//     скрывается (в любом контексте, не только валидный JSON);
//   - содержимое share/happ-ссылок (база после «vmess://» и др.) — [REDACTED];
//   - пары KEY=… секретных имён (env-паттерн, LNB-28) — [REDACTED].
//   - MAC-адреса (шлюз, устройства сети; строки sing-box после T-33) —
//     остаются первые два байта: 50:88:**:**:**:** (D-07, MaskMACs).
//
// env/argv внешних ядер и core_config в лог не пишутся ВООБЩЕ — это политика
// вызывающего кода (extcore W6+/W8); Redact — эшелонированная защита от
// случайной утечки.
package logs

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Уровни лога (sing-box: trace..panic; proto LogLevel маппится в API-слое).
const (
	LevelTrace = "trace"
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	LevelPanic = "panic"
)

// Источники (поле source контракта LogLine).
const (
	SourceCore    = "core"
	SourceSingBox = "singbox" // вывод sing-box (inst.SingBoxRunner.SetLogSink, T-33)
	SourceSubs    = "subs"
	SourceTest    = "test"
	SourceAPI     = "api"
	SourceUpdate  = "update"
	SourceGW      = "gateway"
)

// Line — строка журнала (сообщение уже redacted и без ANSI).
type Line struct {
	TsMs    int64  `json:"ts_ms"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

// defaultRingCapacity — размер ring-буфера по умолчанию (хвост GetTail).
const defaultRingCapacity = 1000

// Mux — мультиплексор: ring-буфер + fan-out подписчикам.
type Mux struct {
	mu      sync.Mutex
	ring    []Line
	cap     int
	nextSeq uint64 // monotonic для Tail-since (будущее; пока simple)
	subs    map[*subscription]struct{}
}

type subscription struct {
	ch      chan Line
	filter  func(Line) bool
	dropped atomic.Uint64 // строки, не поместившиеся в канал подписчика
}

// NewMux создаёт мультиплексор с ring-буфером capacity (<=0 → 1000).
func NewMux(capacity int) *Mux {
	if capacity <= 0 {
		capacity = defaultRingCapacity
	}
	return &Mux{cap: capacity, subs: map[*subscription]struct{}{}}
}

// Logf пишет строку: Redact + ANSI-чистка применяются ЗДЕСЬ, до буфера и
// подписчиков. level "" → info, source "" → "core".
func (m *Mux) Logf(level, source, format string, args ...any) {
	if level == "" {
		level = LevelInfo
	}
	if source == "" {
		source = SourceCore
	}
	// L4 аудита №1: ANSI снимаем ДО редакции. Иначе «PASS[31mWORD=x»
	// после Redact не матчится (ключ разорван escape-последовательностью),
	// а после StripANSI получается открытый «PASSWORD=x» в журнале и
	// подписчиках (LNB-01). Повторная редакция страхует случаи, когда сама
	// escape-последовательность склеивала части секрета.
	// D-07: MAC-адреса (шлюз, устройства сети) в журнале маскируются так же,
	// как секреты, — до буфера, подписчиков и экспорта журнала из UI.
	msg := MaskMACs(Redact(StripANSI(fmt.Sprintf(format, args...))))
	m.publish(Line{TsMs: time.Now().UnixMilli(), Level: level, Source: source, Message: msg})
}

func (m *Mux) publish(l Line) {
	m.mu.Lock()
	m.ring = append(m.ring, l)
	if len(m.ring) > m.cap {
		// сдвиг головного элемента (копирование редких срезов — ок для лога)
		m.ring = m.ring[len(m.ring)-m.cap:]
	}
	for s := range m.subs {
		if s.filter != nil && !s.filter(l) {
			continue
		}
		// неблокирующая доставка: переполнение канала подписчика — строка
		// теряется для ЭТОГО подписчика (антифлуд; FR-UI-216 — backpressure
		// на стороне ядра)
		select {
		case s.ch <- l:
		default:
			s.dropped.Add(1)
		}
	}
	m.mu.Unlock()
}
