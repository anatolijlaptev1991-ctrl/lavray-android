// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_totals.go — выборка счётчиков трафика для GetTrafficTotals.

import (
	"errors"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// trafficTotals — счётчики по профилям. Непустой ids: читаются только они,
// несуществующие пропускаются; пустой — все профили, как раньше.
func (s *Server) trafficTotals(ids []int64) (map[int64]*lavrayv1.TrafficCounters, error) {
	var profiles []*model.Profile
	if len(ids) == 0 {
		all, err := s.st.ListProfiles(-1)
		if err != nil {
			return nil, err
		}
		profiles = all
	} else {
		for _, id := range ids {
			p, err := s.st.GetProfile(id)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			profiles = append(profiles, p)
		}
	}
	totals := make(map[int64]*lavrayv1.TrafficCounters, len(profiles))
	for _, p := range profiles {
		totals[p.ID] = &lavrayv1.TrafficCounters{
			UplinkBytes:   p.Traffic.UplinkBytes,
			DownlinkBytes: p.Traffic.DownlinkBytes,
		}
	}
	return totals, nil
}
