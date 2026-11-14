// SPDX-License-Identifier: GPL-3.0-only

// Package traffic — нейтральная модель трекера соединений и статистики (T-79).
// Здесь нет ни sing-box, ни proto: пакет описывает, ЧТО считает ядро
// (соединения, скорости по тегам), а откуда берутся данные, решает адаптер в
// inst (настоящий trafficcontrol.Manager форка) или FakeSource (тесты api, inst,
// mcp). Так api, уже импортирующий inst, не тянет за собой sing-box.
package traffic

import (
	"errors"
	"strings"
)

// ErrNotFound — соединения с таким id нет: не было, уже закрыто или источник
// остановлен. API отдаёт NOT_FOUND.
var ErrNotFound = errors.New("traffic: соединение не найдено")

// ConnInfo — одно соединение. Нулевое EndMs — соединение активно.
type ConnInfo struct {
	ID          string
	StartMs     int64
	EndMs       int64
	Outbound    string   // тег конечного исходящего ("proxy", "direct", …)
	Chain       []string // теги от выбранного правилом исходящего до конечного
	Destination string   // запрошенный адрес «хост:порт» — приватные данные
	Domain      string   // имя из сниффинга или резолва, если известно
	Inbound     string
	Rule        string
	Process     string // только имя файла: путь на диске пользователя наружу не уходит
	Network     string // "tcp" | "udp"
	Up          int64  // байт отправлено
	Down        int64  // байт получено
}

// Active сообщает, что соединение ещё не закрыто.
func (c ConnInfo) Active() bool { return c.EndMs == 0 }

// TagRate — скорость и накопленный объём по одному тегу исходящего.
type TagRate struct {
	Tag       string
	UpBps     float64
	DownBps   float64
	UpTotal   int64
	DownTotal int64
}

// EventKind — что случилось с соединением.
type EventKind uint8

const (
	EventOpened EventKind = iota + 1
	EventClosed
)

// Event — уведомление источника о соединении.
type Event struct {
	Kind EventKind
	ID   string
}

// ProcessName оставляет у пути процесса только имя файла. Разделители "/" и
// "\" понимаются на любой платформе: путь пришёл из TUN-сессии пользователя,
// а разбирает его, возможно, другая ОС (тесты в Linux CI).
func ProcessName(path string) string {
	path = strings.TrimSpace(path)
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}
