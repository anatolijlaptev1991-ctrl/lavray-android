// SPDX-License-Identifier: GPL-3.0-only

package inst

// sblog.go — журнал sing-box → журнал ядра (T-33, B2-12, D1-04). Без тега
// singbox_run: разбор строк и писатель не зависят от sing-box, раннер лишь
// передаёт писатель в box.Box.SetLogWritter (NEKO_PATCHES №3).

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// LogSink принимает строку журнала sing-box: уровень в терминах logs.Level*
// ("trace".."panic") и сообщение без префикса уровня и без ANSI.
type LogSink func(level, message string)

// LogSinker — раннер, умеющий пересылать собственный журнал (SingBoxRunner).
// api.NewServer подключает к такому раннеру журнал ядра.
type LogSinker interface {
	SetLogSink(sink LogSink)
}

// maxPendingLine — предел накопления строки без перевода строки.
const maxPendingLine = 64 << 10

// logLabel — «INFO[0000] …» либо «… INFO …» (при log.timestamp): метка уровня
// sing-box (log/format.go: верхний регистр) и необязательное «[секунды]».
var logLabel = regexp.MustCompile(`(?:^|\s)(PANIC|FATAL|ERROR|WARN|INFO|DEBUG|TRACE)(?:\[\d+\])?\s`)

// parseSingBoxLine разбирает одну строку журнала sing-box. Без метки уровня
// строка целиком уходит как info.
func parseSingBoxLine(line string) (level, message string) {
	line = logs.StripANSI(line)
	m := logLabel.FindStringSubmatchIndex(line)
	if m == nil {
		return logs.LevelInfo, line
	}
	switch line[m[2]:m[3]] {
	case "PANIC":
		level = logs.LevelPanic
	case "FATAL", "ERROR":
		level = logs.LevelError
	case "WARN":
		level = logs.LevelWarn
	case "DEBUG":
		level = logs.LevelDebug
	case "TRACE":
		level = logs.LevelTrace
	default:
		level = logs.LevelInfo
	}
	return level, line[m[1]:]
}

// logWriter — io.Writer для SetLogWritter: собирает байты в строки и отдаёт
// каждую в sink. sing-box пишет одну запись за вызов Write, но строки
// собираются и из частичных записей.
type logWriter struct {
	mu   sync.Mutex
	sink LogSink
	buf  []byte
}

func newLogWriter(sink LogSink) io.Writer { return &logWriter{sink: sink} }

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			w.accumulate(p)
			break
		}
		if len(w.buf) == 0 && i <= maxPendingLine {
			w.emit(p[:i]) // полная строка целиком в записи: без копии в буфер
		} else {
			w.accumulate(p[:i])
			w.emit(w.buf)
			w.buf = w.buf[:0]
		}
		p = p[i+1:]
	}
	return n, nil
}

// accumulate дописывает кусок строки без перевода строки. Предел
// maxPendingLine проверяется ДО append: переполнение выдаётся порцией ровно в
// предел, поэтому буфер не раздувается даже от одной огромной записи. Буфер
// переиспользуется между строками.
func (w *logWriter) accumulate(chunk []byte) {
	for len(w.buf)+len(chunk) > maxPendingLine {
		take := maxPendingLine - len(w.buf)
		w.buf = append(w.buf, chunk[:take]...)
		chunk = chunk[take:]
		cut := runeBoundary(w.buf, chunk[0])
		var carry [utf8.UTFMax]byte
		n := copy(carry[:], w.buf[cut:])
		w.emit(w.buf[:cut])
		w.buf = append(w.buf[:0], carry[:n]...)
	}
	w.buf = append(w.buf, chunk...)
}

// runeBoundary — длина части buf, которую можно выдать, не разрезав символ
// UTF-8: next — первый байт остатка. Если он продолжение символа, начатого в
// конце buf, неполный символ остаётся для следующей порции (не больше
// UTFMax-1 байт; на мусорных данных режем как есть).
func runeBoundary(buf []byte, next byte) int {
	if utf8.RuneStart(next) {
		return len(buf)
	}
	for k := 1; k < utf8.UTFMax && k <= len(buf); k++ {
		if utf8.RuneStart(buf[len(buf)-k]) {
			return len(buf) - k
		}
	}
	return len(buf)
}

func (w *logWriter) emit(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if len(line) == 0 {
		return
	}
	level, msg := parseSingBoxLine(strings.ToValidUTF8(string(line), "�"))
	w.sink(level, msg)
}
