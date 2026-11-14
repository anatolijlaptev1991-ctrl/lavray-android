// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// migration_proto.go — отчёт и кандидаты переноса → proto.

func reportProto(r *nekoimport.Report) *lavrayv1.NekoBoxReport {
	out := &lavrayv1.NekoBoxReport{
		GroupsImported:   int32(r.GroupsImported),
		ProfilesByType:   make(map[string]int32, len(r.ProfilesByType)),
		AlreadyPresent:   int32(r.AlreadyPresent),
		Warnings:         append([]string(nil), r.Warnings...),
		RoutesFound:      int32(r.RoutesFound),
		RoutesImported:   int32(r.RoutesImported),
		Renumbered:       int32(r.Renumbered),
		SettingsImported: append([]string(nil), r.SettingsImported...),
		DryRun:           r.DryRun,
	}
	for typ, n := range r.ProfilesByType {
		out.ProfilesByType[typ] = int32(n)
	}
	for _, sk := range r.Skipped {
		out.Skipped = append(out.Skipped, &lavrayv1.NekoBoxSkipped{File: sk.File, Id: sk.ID, Type: sk.Type, Reason: sk.Reason})
	}
	if r.ActiveRoute != "" {
		route := r.ActiveRoute
		out.NekoboxActiveRoute = &route
	}
	return out
}

func candidateProto(c nekoimport.Candidate) *lavrayv1.NekoBoxCandidate {
	return &lavrayv1.NekoBoxCandidate{
		Path: c.Path, Source: c.Source,
		Groups: int32(c.Groups), Profiles: int32(c.Profiles), Routes: int32(c.Routes),
		LooksRunning: c.LooksRunning,
	}
}
