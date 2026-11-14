// SPDX-License-Identifier: GPL-3.0-only

// update_comp_apply.go — ApplyUpdates и Rollback для PRESETS и GEO (T-75/7.5).
// Применение требует скачанного и проверенного артефакта, как у приложения:
// файл в staging перечитывается и сверяется заново (анти-TOCTOU), затем
// updates.ApplyPresets/ApplyGeo проверяют содержимое и атомарно подменяют
// данные. Профиль, собранный из прежних данных, продолжает работать; если
// изменился активный набор или гео, а сессия идёт, сообщается ready_for_restart
// с причиной "profile". Параллельное применение и откат — ABORTED.
package api

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// restartProfile — причина ready_for_restart: перезапустить профиль, не приложение.
const restartProfile = "profile"

type eventSend = func(*lavrayv1.UpdateEvent) bool

// applyData применяет скачанный компонент-данные. Нарушение целостности — событие
// verify_failed и очистка staging; состояние окружения — ошибка вызова.
func (s *Server) applyData(c lavrayv1.UpdateComponent, stream *connect.ServerStream[lavrayv1.UpdateEvent]) error {
	staged := s.upd.stagedOf(c)
	if staged == nil {
		return reasonError(ReasonNotStaged, "сначала скачайте обновление (%s)", compName(c))
	}
	if !s.upd.beginApply() {
		return connect.NewError(connect.CodeAborted, errors.New("применение или откат данных уже идёт"))
	}
	defer s.upd.endApply()
	if h := s.upd.hookApply; h != nil {
		h()
	}
	send := eventSender(stream)
	s.upd.setCompPhase(c, lavrayv1.ComponentState_PHASE_APPLYING, "")
	restart, err := false, s.checkStagedFresh(staged)
	if err == nil {
		restart, err = s.applyStaged(c, staged)
	}
	if err != nil {
		return s.dataApplyFailed(c, staged, err, send)
	}
	s.upd.setCompStaged(c, nil)
	s.cleanComponentStaging(staged)
	send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Applied{Applied: compName(c)}})
	if restart && s.sessionActive() {
		send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_ReadyForRestart{ReadyForRestart: restartProfile}})
	}
	return nil
}

func compName(c lavrayv1.UpdateComponent) string {
	if c == compPresets {
		return "presets"
	}
	return "geo"
}

// applyStaged применяет артефакт; restart — изменились данные работающего профиля.
func (s *Server) applyStaged(c lavrayv1.UpdateComponent, staged *stagedComp) (bool, error) {
	if c == compPresets {
		return s.applyPresetFiles(staged)
	}
	return s.applyGeoFile(staged)
}

func (s *Server) applyPresetFiles(staged *stagedComp) (bool, error) {
	files := make([]updates.PresetFile, 0, len(staged.files))
	for _, f := range staged.files {
		data, err := s.readStaged(f, updates.MaxPresetBytes)
		if err != nil {
			return false, err
		}
		files = append(files, updates.PresetFile{ID: f.id, Data: data})
	}
	changes, err := updates.ApplyPresets(s.presetOverrideDir(), staged.manifest, files)
	if err != nil {
		return false, err
	}
	active := s.activePresetID()
	return slices.ContainsFunc(changes, func(c updates.PresetChange) bool { return c.ID == active }), nil
}

func (s *Server) applyGeoFile(staged *stagedComp) (bool, error) {
	sec := staged.manifest.Geo
	if sec == nil || len(staged.files) != 1 {
		return false, fmt.Errorf("%w: гео-набор не скачан", updates.ErrNotStaged)
	}
	data, err := s.readStaged(staged.files[0], updates.MaxGeoArchiveBytes)
	if err != nil {
		return false, err
	}
	required, err := presets.EmbeddedGeoFiles()
	if err != nil {
		return false, err
	}
	res, err := updates.ApplyGeo(s.geoDir(), data, *sec, updates.DefaultGeoLimits, required)
	if err != nil {
		return false, err
	}
	return res.Changed, nil
}

// readStaged читает файл из staging/components, не выходя за каталог и за предел
// размера: читается не больше limit+1 байт. Системные ошибки с путями не
// возвращаются — они уходят в журнал, наружу идёт ErrNotStaged.
func (s *Server) readStaged(f stagedFile, limit int64) ([]byte, error) {
	p, err := updates.InsideStaging(s.compStagingDir(), f.path)
	if errors.Is(err, updates.ErrUnsafePath) {
		return nil, err
	}
	if err != nil { // файла нет или он недоступен: путь в ошибке ФС наружу не идёт
		s.logDetail("staging", err)
		return nil, fmt.Errorf("%w: файл недоступен", updates.ErrNotStaged)
	}
	fh, err := os.Open(p)
	if err != nil {
		s.logDetail("staging", err)
		return nil, fmt.Errorf("%w: файл недоступен", updates.ErrNotStaged)
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, limit+1))
	if err != nil {
		s.logDetail("staging", err)
		return nil, fmt.Errorf("%w: файл не читается", updates.ErrNotStaged)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: файл в staging больше %d байт", updates.ErrTooLarge, limit)
	}
	return data, nil
}

// checkStagedFresh: скачанные данные не применяются, если манифест, по которому они
// сверены, успел истечь или вытеснен более новым (serial ниже виденного последней
// проверкой). Долго живущее ядро шлюза не ставит устаревшее.
func (s *Server) checkStagedFresh(st *stagedComp) error {
	mf := st.manifest
	if !mf.Expires.After(time.Now()) {
		return fmt.Errorf("%w: манифест скачанных данных истёк", updates.ErrExpired)
	}
	state := s.loadUpdateState()
	saved := state.SerialStable
	if mf.Channel == updates.ChannelBeta {
		saved = state.SerialBeta
	}
	if mf.Serial < saved {
		return fmt.Errorf("%w: serial скачанных данных %d ниже виденного %d", updates.ErrSerialRollback, mf.Serial, saved)
	}
	return nil
}
