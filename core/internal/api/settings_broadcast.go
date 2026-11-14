// SPDX-License-Identifier: GPL-3.0-only

package api

// settings_broadcast.go — рассылка SettingsChanged подписчикам SubscribeSettings
// (хвосты A). Подписчики лежат в Server.setSubs (settings_event.go собирает
// события). Рассылка не блокируется ни на одном клиенте: у каждого буфер на
// settingsSubBuffer событий, переполнивший его подписчик снимается, а его поток
// закрывается с ABORTED (settings_subscribe.go).

import (
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// settingsSubBuffer — размер буфера одного подписчика, событий.
const settingsSubBuffer = 16

// subscribeSettings регистрирует подписчика: канал событий и функция отписки.
// Канал закрывает только рассылка — при переполнении буфера; отписка его не
// закрывает, поэтому закрытый канал всегда значит «подписчик не успевал».
func (s *Server) subscribeSettings() (<-chan *lavrayv1.SettingsChanged, func()) {
	ch := make(chan *lavrayv1.SettingsChanged, settingsSubBuffer)
	s.setMu.Lock()
	s.setSubs[ch] = struct{}{}
	s.setMu.Unlock()
	return ch, func() {
		s.setMu.Lock()
		delete(s.setSubs, ch)
		s.setMu.Unlock()
	}
}

// settingsSubscribers — число подписчиков (тесты и диагностика).
func (s *Server) settingsSubscribers() int {
	s.setMu.Lock()
	defer s.setMu.Unlock()
	return len(s.setSubs)
}

// publishSettingsChanged рассылает событие подписчикам без блокировки. У кого
// буфер полон, того снимаем и закрываем его канал: клиент, который не читает,
// не должен ни тормозить остальных, ни копить память ядра. Пустое событие не
// рассылается.
func (s *Server) publishSettingsChanged(ev *lavrayv1.SettingsChanged) {
	if len(ev.Changed) == 0 && len(ev.Removed) == 0 {
		return
	}
	s.setMu.Lock()
	defer s.setMu.Unlock()
	for ch := range s.setSubs {
		select {
		case ch <- ev:
		default:
			delete(s.setSubs, ch)
			close(ch)
		}
	}
}

// settingWrite — одна запись настройки: ключ и JSON-значение.
type settingWrite struct{ key, raw string }

// writeSettings пишет значения и рассылает подписчикам те, что изменились.
// Событие уходит и при сбое посреди списка: записанное уже действует.
func (s *Server) writeSettings(writes []settingWrite) error {
	ev := &lavrayv1.SettingsChanged{Changed: map[string]*lavrayv1.SettingValue{}}
	defer s.publishSettingsChanged(ev)
	for _, w := range writes {
		before := s.settingJSON(w.key)
		if err := s.st.SetSetting(w.key, w.raw); err != nil {
			return err
		}
		if before != w.raw {
			ev.Changed[w.key] = typedSettingValue(w.key, w.raw)
		}
	}
	return nil
}
