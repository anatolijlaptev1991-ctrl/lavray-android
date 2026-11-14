// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

// Платформенный писатель отдаёт в sink то же самое, что прежний путь
// (SetLogWritter + logWriter). Пока это так, прежний путь не нужен: иначе каждая
// строка журнала ядра дублировалась бы.
//
// Сверка идёт на двух уровнях: на сообщениях, отформатированных фабрикой журнала
// форка (многострочные, цветные, с тегом и без, ниже и выше уровня), и на живом
// инстансе (строки запуска и остановки).
func TestPlatformWriterParityWithLogSink(t *testing.T) {
	t.Run("форматированные сообщения", parityOnFormattedMessages)
	t.Run("живой инстанс", parityOnLiveInstance)
}

func parityOnFormattedMessages(t *testing.T) {
	opts := &option.LogOptions{Level: "info"}
	base := time.Unix(1_700_000_000, 0)
	console := log.Formatter{BaseTime: base}
	platform := log.Formatter{BaseTime: base, DisableLineBreak: true}
	cases := []struct {
		level    log.Level
		tag, msg string
	}{
		{log.LevelInfo, "inbound/mixed[mixed-in]", "tcp server started at 127.0.0.1:2080"},
		{log.LevelWarn, "dns", "lookup failed"},
		{log.LevelError, "", "без тега"},
		{log.LevelInfo, "router", "первая строка\nвторая строка\n\nчетвёртая"},
		{log.LevelDebug, "router", "ниже уровня: в журнал не идёт"},
		{log.LevelTrace, "dns", "ещё ниже"},
		{log.LevelError, "connection", "ошибка с хвостом\n"},
	}
	sinkC, linesC := collect()
	sinkP, linesP := collect()
	cw := newLogWriter(sinkC)
	pw := newPlatformWriter(opts, sinkP)
	for _, c := range cases {
		now := base.Add(5 * time.Second)
		if c.level <= log.LevelInfo { // консоль фабрика фильтрует по log.level сама
			_, _ = cw.Write([]byte(console.Format(nil, c.level, c.tag, c.msg, now)))
		}
		pw.WriteMessage(c.level, platform.Format(nil, c.level, c.tag, c.msg, now))
	}
	if got, want := linesP(), linesC(); fmt.Sprint(got) != fmt.Sprint(want) || len(got) == 0 {
		t.Fatalf("платформенный путь:\n%v\nконсольный путь:\n%v", got, want)
	}
}

func parityOnLiveInstance(t *testing.T) {
	ctx := include.Context(context.Background())
	port := freeTCPPort(t)
	var options option.Options
	if err := sbjson.UnmarshalContext(ctx, trafficConfig(port, "debug"), &options); err != nil {
		t.Fatal(err)
	}
	sinkC, linesC := collect()
	sinkP, linesP := collect()
	instance, err := box.New(box.Options{
		Context: ctx, Options: options,
		PlatformLogWriter: newPlatformWriter(options.Log, sinkP),
	})
	if err != nil {
		t.Fatal(err)
	}
	instance.SetLogWritter(newLogWriter(sinkC)) // прежний путь — для сравнения
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	// Соединение к порту входа: в журнал уходит строка запуска слушателя и, на
	// уровне debug, подробности.
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second); err == nil {
		_ = c.Close()
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	got, want := linesP(), linesC()
	if len(got) < 3 {
		t.Fatalf("в журнале платформенного писателя %d строк: %v", len(got), got)
	}
	// Префикс соединения «[id 5ms]» содержит время на момент форматирования: две
	// записи одной строки могут отличаться миллисекундой — сравниваем без него.
	if stripDurations(got) != stripDurations(want) {
		t.Fatalf("журналы разошлись.\nплатформенный (%d):\n%v\nконсольный (%d):\n%v", len(got), got, len(want), want)
	}
}

// durationPrefix — «[id 5ms] » в начале сообщения о соединении.
var durationPrefix = regexp.MustCompile(`^\[(\d+) [^\]]*\] `)

func stripDurations(lines []captured) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.level + " " + durationPrefix.ReplaceAllString(l.msg, "[$1] "))
		b.WriteByte('\n')
	}
	return b.String()
}
