// SPDX-License-Identifier: GPL-3.0-only

// client_sources.go — шаги цепочки проверки, связанные с корнем и списком
// источников: refreshRoot (ротация + защита от отсутствующей роли artifact) и
// fetchSources (подписанный sources.json поверх bootstrap-списка сборки).
package updates

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// refreshRoot тянет корни N+1… с bootstrap-источников (подписанный sources.json
// ещё нельзя проверить: ключи могли смениться), проверяет срок итогового корня
// и наличие роли artifact; принятый корень сохраняет (версия не понижается).
func (c *Client) refreshRoot(ctx context.Context, now time.Time) error {
	next, _, err := AdvanceRootWith(ctx, c.BootstrapSources, c.Trust, now, c.queryPolicy())
	if err != nil {
		return err
	}
	if next.Root().Version > c.Trust.Root().Version {
		if perr := PersistRoot(c.RootStore, next); perr != nil {
			c.warn(fmt.Sprintf("не удалось сохранить принятый root.json: %v", perr))
		}
	}
	c.Trust = next
	c.resetCountersAfterRotation()
	if _, err := next.Root().ArtifactKeys(); err != nil {
		return err
	}
	return nil
}

// resetCountersAfterRotation: если с корня, при котором сохранены счётчики
// (State.RootVersion), из роли timestamp или manifest УДАЛЁН ключ, их версии
// и serial обнуляются. Признак — версия корня в состоянии, а не факт ротации
// в этом запуске: ротация могла пройти в прошлом запуске, не дошедшем до записи
// состояния.
func (c *Client) resetCountersAfterRotation() {
	st := c.state()
	if st.RootVersion <= 0 || c.Trust.Root().Version <= st.RootVersion {
		return
	}
	if c.Trust.KeysRemovedSince(st.RootVersion, RoleTimestamp) {
		st.TimestampVersion, st.ResetTimestamp = 0, true
	}
	if c.Trust.KeysRemovedSince(st.RootVersion, RoleManifest) {
		st.SerialStable, st.SerialBeta, st.ResetSerials = 0, 0, true
	}
}

// fetchSources — подписанный sources.json с любого bootstrap-источника; его
// метаданные-зеркала добавляются к bootstrap-списку. Нет подписанного списка —
// работаем только с bootstrap (первая проверка; источник мог быть недоступен).
func (c *Client) fetchSources(ctx context.Context, now time.Time) ([]Source, *Sources) {
	all := append([]Source(nil), c.BootstrapSources...)
	var signed *Sources
	for _, a := range queryAll(ctx, c.BootstrapSources, "sources.json", c.queryPolicy()) {
		if a.err != nil || len(a.body) == 0 {
			continue
		}
		env, err := c.Trust.Verify(a.body, RoleSources)
		if err != nil {
			continue
		}
		parsed, err := ParseSources(env.Signed)
		if err != nil || expires(parsed.Expires, now) {
			continue
		}
		signed = parsed
		break
	}
	if signed != nil {
		for _, base := range signed.Metadata {
			all = append(all, &HTTPSource{Base: base, Client: c.HTTP})
		}
	}
	return all, signed
}

// preferred ставит источник победившего timestamp первым (без дублей).
func preferred(srcs []Source, first Source) []Source {
	out := make([]Source, 0, len(srcs))
	out = append(out, first)
	for _, s := range srcs {
		if s != first {
			out = append(out, s)
		}
	}
	return out
}

// mirrors — источники артефактов: подписанный список, затем bootstrap-источники,
// умеющие отдавать потоки.
func mirrors(signed *Sources, bootstrap []Source, hc *http.Client) []ArtifactSource {
	var out []ArtifactSource
	if signed != nil {
		for _, base := range signed.Artifacts {
			out = append(out, &HTTPSource{Base: base, Client: hc})
		}
	}
	for _, s := range bootstrap {
		if a, ok := s.(ArtifactSource); ok {
			out = append(out, a)
		}
	}
	return out
}

func (c *Client) queryPolicy() QueryPolicy {
	return QueryPolicy{Timeout: c.PhaseTimeout, Grace: c.Grace}
}
