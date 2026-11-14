// SPDX-License-Identifier: GPL-3.0-only

package api

// connect_profile.go — профиль для Connect: ровно один источник, profile_id
// (сохранённый) или inline_profile (без сохранения). Inline-профиль никогда не
// подтверждён (T-08B): подтверждение привязано к сохранённому профилю.

import (
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const (
	// warnInlineCustomIgnored — custom-поля inline-профиля отброшены.
	warnInlineCustomIgnored = "inline_custom_ignored: custom-поля inline-профиля не применяются — сохраните профиль и подтвердите их"
	// warnCustomUnconfirmed — custom-поля сохранённого профиля не подтверждены
	// (в том числе сброшены миграцией 0.3.0) и в конфиг не входят.
	warnCustomUnconfirmed = "custom_unconfirmed: custom-поля профиля не подтверждены и не применяются — откройте diff и подтвердите их"
)

// connectProfile — профиль запроса и предупреждения о нём.
func (s *Server) connectProfile(in *lavrayv1.ConnectRequest) (*model.Profile, []string, error) {
	switch {
	case in.ProfileId != nil && in.InlineProfile != nil:
		return nil, nil, invalidArg("задан и profile_id, и inline_profile — ровно один источник")
	case in.ProfileId != nil:
		p, err := s.st.GetProfile(in.GetProfileId())
		if err != nil {
			return nil, nil, err
		}
		return p, unconfirmedWarning(p), nil
	case in.InlineProfile != nil:
		// inline: подключение без сохранения — группа может быть не указана
		return inlineProfile(in.InlineProfile)
	}
	return nil, nil, invalidArg("не задан ни profile_id, ни inline_profile")
}

// inlineProfile — inline-профиль без custom-полей. «Свой полный конфиг»
// без подтверждения не собирается вовсе, поэтому отказ сразу и с причиной;
// custom_outbound/custom_config отбрасываются с предупреждением.
func inlineProfile(in *lavrayv1.Profile) (*model.Profile, []string, error) {
	p, err := profileToModel(in)
	if err != nil {
		return nil, nil, err
	}
	if model.IsInternalFull(p) {
		return nil, nil, invalidArg("inline-профиль «свой полный конфиг» не подключается: сохраните профиль и подтвердите конфиг")
	}
	if !model.HasCustomContent(p) {
		return p, nil, nil
	}
	p.CustomOutboundJSON, p.CustomConfigJSON = "", ""
	return p, []string{warnInlineCustomIgnored}, nil
}

// unconfirmedWarning — предупреждение для профиля с неподтверждёнными
// custom-полями. internal-full без подтверждения не собирается вовсе — там
// понятная ошибка сборки, предупреждение не нужно.
func unconfirmedWarning(p *model.Profile) []string {
	if p.CustomFieldsConfirmed || model.IsInternalFull(p) || !model.HasCustomContent(p) {
		return nil
	}
	return []string{warnCustomUnconfirmed}
}
