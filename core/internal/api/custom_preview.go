// SPDX-License-Identifier: GPL-3.0-only

package api

// custom_preview.go — PreviewCustomFields и ConfirmCustomFields (T-08B).
// Предпросмотр собирает конфиг профиля без custom-полей и с ними и отдаёт
// unified diff с замаскированными секретами, затронутые пути и хэш текущего
// содержимого. Подтверждение принимает только этот хэш: если содержимое
// успело измениться, хранилище отвечает «устарело», и UI показывает diff
// заново.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/textdiff"
)

const (
	diffContextLines = 3
	maxDiffLines     = 20000

	warnInternalFull    = "internal_full"
	warnInboundsChanged = "inbounds_changed"
)

func (s *Server) PreviewCustomFields(ctx context.Context, req *connect.Request[lavrayv1.PreviewCustomFieldsRequest]) (*connect.Response[lavrayv1.PreviewCustomFieldsResponse], error) {
	p, err := s.st.GetProfile(req.Msg.GetProfileId())
	if err != nil {
		return nil, mapErr(err)
	}
	if !model.HasCustomContent(p) {
		return nil, failedPrecondition("у профиля %d нет custom-полей", p.ID)
	}
	hash, err := model.CustomFieldsHash(p)
	if err != nil {
		return nil, invalidArg("custom-JSON профиля %d: %v", p.ID, err)
	}
	before, after, err := s.customConfigPair(p)
	if err != nil {
		return nil, err
	}
	diff, truncated := textdiff.Unified(before.text, after.text, diffContextLines, maxDiffLines)
	touched := touchedKeys(before.tree, after.tree) // предупреждения — по полному списку
	return connect.NewResponse(&lavrayv1.PreviewCustomFieldsResponse{
		CustomHash:       hash,
		Diff:             diff,
		TouchedKeys:      capTouchedKeys(touched),
		Warnings:         customWarnings(p, touched),
		AlreadyConfirmed: p.CustomFieldsConfirmed,
		DiffTruncated:    truncated,
	}), nil
}

func (s *Server) ConfirmCustomFields(ctx context.Context, req *connect.Request[lavrayv1.ConfirmCustomFieldsRequest]) (*connect.Response[lavrayv1.Profile], error) {
	hash := req.Msg.GetCustomHash()
	if !isSHA256Hex(hash) {
		return nil, invalidArg("custom_hash: ожидается 64 шестнадцатеричных символа из PreviewCustomFields")
	}
	p, err := s.st.ConfirmCustomFields(req.Msg.GetProfileId(), hash)
	switch {
	case errors.Is(err, store.ErrNoCustomContent):
		return nil, failedPrecondition("у профиля %d нет custom-полей", req.Msg.GetProfileId())
	case errors.Is(err, store.ErrCustomHashStale):
		return nil, failedPrecondition("custom-поля изменились после предпросмотра — откройте diff заново")
	case err != nil:
		return nil, mapErr(err)
	}
	s.logs.Logf(logs.LevelInfo, logs.SourceCore, "custom-поля профиля %d подтверждены", p.ID)
	return connect.NewResponse(profileToProto(p)), nil
}

// builtConfig — собранный конфиг: текст для diff (секреты замаскированы
// структурно, preview_redact.go) и исходное дерево encoding/json для путей
// (в ответ уходят только пути, не значения).
type builtConfig struct {
	text string
	tree map[string]any
}

// customConfigPair — конфиг без custom-полей и с ними (как после
// подтверждения). У internal-full «без» — пустой конфиг: неподтверждённым он
// не собирается вовсе.
func (s *Server) customConfigPair(p *model.Profile) (before, after builtConfig, err error) {
	with := *p
	with.CustomFieldsConfirmed = true
	if after, err = s.previewBuild(&with); err != nil || model.IsInternalFull(p) {
		return builtConfig{}, after, err
	}
	without := *p
	without.CustomFieldsConfirmed = false
	before, err = s.previewBuild(&without)
	return before, after, err
}

// previewBuild собирает конфиг без запуска. Код ошибки — как у сборки
// (INVALID_ARGUMENT, FAILED_PRECONDITION, NOT_FOUND…), текст замаскирован.
func (s *Server) previewBuild(p *model.Profile) (builtConfig, error) {
	res, err := s.dryRunBuild(p, lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY)
	if err != nil {
		code, msg := connect.CodeInvalidArgument, err.Error()
		if ce := new(connect.Error); errors.As(mapErr(err), &ce) {
			code, msg = ce.Code(), ce.Message()
		}
		return builtConfig{}, connect.NewError(code, fmt.Errorf("сборка конфига: %s", logs.Redact(msg)))
	}
	raw, err := cfgbuild.MarshalIndent(res.Config)
	if err != nil {
		return builtConfig{}, connect.NewError(connect.CodeInternal, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // числа в diff — как в конфиге, без float64
	var tree map[string]any
	if err := dec.Decode(&tree); err != nil {
		return builtConfig{}, connect.NewError(connect.CodeInternal, err)
	}
	masked, err := json.MarshalIndent(redactTree(tree, false), "", "  ")
	if err != nil {
		return builtConfig{}, connect.NewError(connect.CodeInternal, err)
	}
	return builtConfig{text: string(masked) + "\n", tree: tree}, nil
}

// customWarnings — коды предупреждений для диалога подтверждения.
func customWarnings(p *model.Profile, touched []string) []string {
	var out []string
	if model.IsInternalFull(p) {
		out = append(out, warnInternalFull)
	}
	for _, k := range touched {
		if k == "inbounds" || strings.HasPrefix(k, "inbounds[") || strings.HasPrefix(k, "inbounds.") {
			out = append(out, warnInboundsChanged)
			break
		}
	}
	return out
}

// isSHA256Hex — 64 символа [0-9a-f] (как отдаёт CustomFieldsHash).
func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
