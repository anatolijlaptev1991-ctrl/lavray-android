// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

// platform_writer.go — log.PlatformWriter форка → LogSink ядра (T-79).
//
// Зачем он нужен. box.New создаёт trafficcontrol.Manager (источник
// соединений и счётчиков) только при непустом Options.PlatformLogWriter, а
// clash_api запрещён. Писатель заодно заменил прежнюю пересылку журнала через
// SetLogWritter (патч №3, подмена приватного поля через reflect/unsafe):
// журнал попадает в sink штатным путём, без обращения к устройству фабрики.
//
// Чем платформенный путь отличается от консольного. Фабрика журнала отдаёт
// платформенному писателю сообщения ВСЕХ уровней (консоль она фильтрует по
// log.level сама), поэтому уровень проверяется здесь. Сообщение приходит
// одной строкой, внутри которой могут быть переводы строки; разбор на строки и
// уровни тот же, что у консольного писателя (logWriter), — тест паритета
// TestPlatformWriterParityWithLogSink сверяет оба пути.

import (
	"fmt"
	"io"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// platformWriter передаёт сообщения sing-box в LogSink.
type platformWriter struct {
	level log.Level // самый подробный уровень, который нужен (log.level конфига)
	lines *logWriter
}

// newPlatformWriter создаёт писатель для конфигурации options. sink == nil —
// сообщения отбрасываются (писатель всё равно нужен ради Manager).
func newPlatformWriter(options *option.LogOptions, sink LogSink) log.PlatformWriter {
	w := &platformWriter{level: configuredLevel(options)}
	if sink != nil {
		w.lines = &logWriter{sink: sink}
	}
	return w
}

// configuredLevel — уровень из конфига. Пустой уровень sing-box трактует как
// trace, неизвестный отвергает ещё в box.New — сюда такой не дойдёт.
func configuredLevel(options *option.LogOptions) log.Level {
	if options == nil || options.Level == "" {
		return log.LevelTrace
	}
	level, err := log.ParseLevel(options.Level)
	if err != nil {
		return log.LevelInfo
	}
	return level
}

// WriteMessage реализует log.PlatformWriter.
func (w *platformWriter) WriteMessage(level log.Level, message string) {
	if w.lines == nil || level > w.level {
		return
	}
	_, _ = w.lines.Write([]byte(message + "\n"))
}

// silenceConsole глушит консольный вывод sing-box (stderr), чтобы каждая строка
// не шла дважды: в журнал ядра и в stderr, который читает оболочка. Это
// единственное, что осталось от патча SetLogWritter (подмена приватного поля
// фабрики через reflect/unsafe): журнал он больше не несёт, так что при смене
// устройства фабрики в новом пине худшее последствие — шум в stderr, а не
// потеря журнала. Паника патча гасится и сообщается через sink.
func silenceConsole(instance *box.Box, sink LogSink) {
	defer func() {
		if r := recover(); r != nil {
			sink("warn", fmt.Sprintf("консольный вывод sing-box не заглушен: %v", r))
		}
	}()
	instance.SetLogWritter(io.Discard)
}
