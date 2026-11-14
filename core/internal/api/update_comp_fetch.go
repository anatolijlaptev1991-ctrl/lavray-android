// SPDX-License-Identifier: GPL-3.0-only

// update_comp_fetch.go — загрузка наборов и гео-архива с зеркал и укладка в
// <данные>/updates/staging/components (T-75/7.5): сверка длины и sha256 по
// подписанному манифесту, атомарная запись, уборка прежних файлов.
package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// downloadPresets качает наборы, чья ревизия выше действующей; false — нечего.
// Набор принимается целиком: если файл любого из наборов не прошёл сверку или не
// записался, уже уложенные в staging файлы этой попытки убираются (discardAttempt).
func (s *Server) downloadPresets(ctx context.Context, res *updates.Result, send func(*lavrayv1.UpdateEvent) bool) (bool, error) {
	installed, err := s.builtinList()
	if err != nil {
		return false, s.dataFailure(compPresets, err)
	}
	offers := updates.PendingPresets(res.Manifest, installed)
	if len(offers) == 0 {
		return false, nil
	}
	revs := map[string]int{}
	files := make([]stagedFile, 0, len(offers))
	for _, o := range offers {
		e := res.Manifest.Presets[o.ID]
		data, err := updates.FetchBlob(ctx, res.Mirrors, updates.BlobSpec{Path: e.Path, Sha256: e.Sha256, Max: updates.MaxPresetBytes}, nil)
		if err != nil {
			s.discardAttempt(files)
			return true, s.compDownloadFailed(ctx, compPresets, err, send)
		}
		f, err := s.stageBlob(o.ID+".json", e.Sha256, data)
		if err != nil {
			s.discardAttempt(files)
			return true, s.dataFailure(compPresets, err)
		}
		f.id = o.ID
		files, revs[o.ID] = append(files, f), o.Rev
	}
	s.commitStaged(compPresets, &stagedComp{manifest: res.Manifest, version: revString(revs), files: files}, send)
	return true, nil
}

// downloadGeo качает гео-архив, если он отличается от установленного.
func (s *Server) downloadGeo(ctx context.Context, res *updates.Result, send func(*lavrayv1.UpdateEvent) bool) (bool, error) {
	sec := updates.PendingGeo(res.Manifest, updates.InstalledGeo(s.geoDir()))
	if sec == nil {
		return false, nil
	}
	if sec.Length > updates.MaxGeoArchiveBytes {
		return true, s.compDownloadFailed(ctx, compGeo, fmt.Errorf("%w: гео-архив %d байт", updates.ErrTooLarge, sec.Length), send)
	}
	started, last := time.Now(), time.Time{}
	send(compProgressEvent(compGeo, 0, sec.Length, 0))
	data, err := updates.FetchBlob(ctx, res.Mirrors, updates.BlobSpec{Path: sec.Path, Sha256: sec.Sha256, Length: sec.Length},
		func(done, total int64) {
			if done < total && time.Since(last) < progressEvery {
				return
			}
			last = time.Now()
			send(compProgressEvent(compGeo, done, total, float64(done)/max(time.Since(started).Seconds(), 0.001)))
		})
	if err != nil {
		return true, s.compDownloadFailed(ctx, compGeo, err, send)
	}
	f, err := s.stageBlob("geo.zip", sec.Sha256, data)
	if err != nil {
		return true, s.dataFailure(compGeo, err)
	}
	s.commitStaged(compGeo, &stagedComp{manifest: res.Manifest, version: geoLabel(sec.GeneratedAt, sec.Sha256), files: []stagedFile{f}}, send)
	return true, nil
}

// commitStaged запоминает скачанное, убирает прежние файлы и сообщает verified.
func (s *Server) commitStaged(c lavrayv1.UpdateComponent, st *stagedComp, send func(*lavrayv1.UpdateEvent) bool) {
	old := s.upd.stagedOf(c)
	s.upd.setCompStaged(c, st)
	s.cleanComponentStaging(old)
	for _, f := range st.files {
		version := st.version
		if f.id != "" {
			version = fmt.Sprintf("%s:%d", f.id, st.manifest.Presets[f.id].Rev)
		}
		send(verifiedEvent(c, version, f))
	}
}

// stageBlob записывает проверенный артефакт в staging/components атомарно.
func (s *Server) stageBlob(name, sha string, data []byte) (stagedFile, error) {
	dir := s.compStagingDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return stagedFile{}, err
	}
	final, err := updates.StagedPath(dir, sha, name)
	if err != nil {
		return stagedFile{}, err
	}
	tmp := final + updates.PartSuffix
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		_ = os.Remove(tmp)
		return stagedFile{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return stagedFile{}, err
	}
	return stagedFile{path: final, sha256: sha}, nil
}

// discardAttempt убирает из staging файлы, которые эта неудавшаяся попытка уже
// уложила. Файлы действующих скачанных артефактов (тот же sha256 и имя — тот же
// путь) и посторонние файлы остаются.
func (s *Server) discardAttempt(files []stagedFile) {
	s.cleanComponentStaging(&stagedComp{files: files})
}

// cleanComponentStaging удаляет файлы старого артефакта, не нужные текущим.
func (s *Server) cleanComponentStaging(old *stagedComp) {
	if old == nil {
		return
	}
	keep := map[string]bool{}
	for _, c := range []lavrayv1.UpdateComponent{compPresets, compGeo} {
		if st := s.upd.stagedOf(c); st != nil {
			for _, f := range st.files {
				keep[filepath.Clean(f.path)] = true
			}
		}
	}
	for _, f := range old.files {
		if !keep[filepath.Clean(f.path)] {
			_ = os.Remove(f.path)
		}
	}
}
