// SPDX-License-Identifier: GPL-3.0-only

package api

// resolve_domains.go — ResolveDomains (FR-UI-043): адрес сервера → IP, домен →
// SNI/host. Разбор параллельный (resolver.go), запись одной транзакцией.

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// maxResolveIDs — предел числа профилей в одном запросе.
const maxResolveIDs = 1000

// resolveEntry — профиль запроса и его судьба.
type resolveEntry struct {
	id     int64
	p      *model.Profile // загруженный профиль; nil — не найден
	host   string         // имя для разбора; "" — пропущен
	reason string         // причина отказа; "" — не отказ
	update *model.Profile // что записать (после разбора)
}

// ResolveDomains превращает адрес-домен профилей в IP, перенося домен в SNI и
// host, если они пусты. Цепочки, custom-профили и уже-IP пропускаются
// (skipped_ids); непрошедшие (профиля нет, нет ответа, таймаут 5 с на хост)
// попадают в failed, остальные разрешаются. Пустой список или больше 1000 id —
// INVALID_ARGUMENT. Отмена запроса ничего не записывает.
func (s *Server) ResolveDomains(ctx context.Context, req *connect.Request[lavrayv1.ResolveDomainsRequest]) (*connect.Response[lavrayv1.ResolveDomainsResponse], error) {
	if n := len(req.Msg.Ids); n == 0 || n > maxResolveIDs {
		return nil, invalidArg("ids: нужен список от 1 до %d профилей, получено %d", maxResolveIDs, n)
	}
	entries, err := s.loadResolveEntries(req.Msg.Ids)
	if err != nil {
		return nil, mapErr(err)
	}
	results := s.resolve.lookupAll(ctx, resolveHosts(entries))
	if ctx.Err() != nil {
		return nil, mapErr(ctx.Err())
	}
	if err := s.planResolved(entries, results); err != nil {
		return nil, mapErr(err)
	}
	if err := s.writeResolved(entries); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(resolveResponse(entries)), nil
}

// loadResolveEntries читает профили запроса (повторы id отбрасываются).
func (s *Server) loadResolveEntries(ids []int64) ([]*resolveEntry, error) {
	seen := map[int64]bool{}
	var out []*resolveEntry
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		e := &resolveEntry{id: id}
		p, err := s.st.GetProfile(id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			e.reason = reasonNotFound
		case err != nil:
			return nil, err
		default:
			e.p = p
			e.host, _ = resolvableHost(p)
		}
		out = append(out, e)
	}
	return out, nil
}

// resolveHosts — имена к разбору без повторов (в порядке запроса).
func resolveHosts(entries []*resolveEntry) []string {
	seen := map[string]bool{}
	var hosts []string
	for _, e := range entries {
		if e.host != "" && !seen[e.host] {
			seen[e.host] = true
			hosts = append(hosts, e.host)
		}
	}
	return hosts
}

// planResolved сопоставляет итоги разбора профилям. Профиль перечитывается:
// правка пользователя за время разбора (до 5 с на хост) не затирается.
func (s *Server) planResolved(entries []*resolveEntry, results map[string]resolution) error {
	for _, e := range entries {
		if e.host == "" || e.reason != "" {
			continue
		}
		res := results[e.host]
		if res.reason != "" {
			e.reason = res.reason
			continue
		}
		fresh, err := s.st.GetProfile(e.id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			e.reason = reasonNotFound
		case err != nil:
			return err
		case fresh.Address != e.p.Address:
			e.reason = reasonChanged
		default:
			e.update = withResolvedAddress(fresh, res.addr)
		}
	}
	return nil
}

// writeResolved пишет все обновления одной транзакцией.
func (s *Server) writeResolved(entries []*resolveEntry) error {
	return s.st.InTx(func(tx *store.Store) error {
		for _, e := range entries {
			if e.update == nil {
				continue
			}
			if err := tx.UpdateProfile(e.update); err != nil {
				return err
			}
		}
		return nil
	})
}

// resolveResponse раскладывает судьбы профилей по трём спискам ответа.
func resolveResponse(entries []*resolveEntry) *lavrayv1.ResolveDomainsResponse {
	resp := &lavrayv1.ResolveDomainsResponse{}
	for _, e := range entries {
		switch {
		case e.reason != "":
			resp.Failed = append(resp.Failed, &lavrayv1.ResolveFailure{Id: e.id, Reason: e.reason})
		case e.update != nil:
			resp.ResolvedIds = append(resp.ResolvedIds, e.id)
		default:
			resp.SkippedIds = append(resp.SkippedIds, e.id)
		}
	}
	return resp
}
