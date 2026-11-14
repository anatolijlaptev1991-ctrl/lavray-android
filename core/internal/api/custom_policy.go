// SPDX-License-Identifier: GPL-3.0-only

package api

// custom_policy.go — custom-поля в профилях от клиента (T-08B). Флаг
// подтверждения клиента не читается (profileToModel), подтверждение — хэш,
// который ставит только ConfirmCustomFields; при UpdateProfile хранилище
// берёт его из БД и стирает, если содержимое изменилось.

import (
	"fmt"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// maxCustomJSONBytes — предел одного custom-JSON (custom_outbound,
// custom_config, полный конфиг internal-full).
const maxCustomJSONBytes = 256 << 10

// profileFromClient — профиль из Create/UpdateProfile (old — текущая версия,
// nil при создании) с проверкой изменённых custom-JSON.
func profileFromClient(in *lavrayv1.Profile, old *model.Profile) (*model.Profile, error) {
	m, err := profileToModel(in)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := checkCustomJSON(m, old); err != nil {
		return nil, err
	}
	if err := checkEndpoint(m, old); err != nil {
		return nil, err
	}
	return m, nil
}

// customField — одно custom-JSON поле профиля для проверки.
type customField struct {
	name string
	raw  func(*model.Profile) string
}

var customFields = []customField{
	{"custom_outbound_json", func(p *model.Profile) string { return p.CustomOutboundJSON }},
	{"custom_config_json", func(p *model.Profile) string { return p.CustomConfigJSON }},
	{"custom.full_config_json", model.InternalFullJSON},
}

// checkCustomJSON проверяет изменённые custom-JSON поля: не больше
// maxCustomJSONBytes, JSON-объект, без дублей ключей и мусора после
// документа. Неизменённые (как в old) не проверяются: профиль, сохранённый до
// этих правил, можно переименовать, не исправляя его custom-JSON.
func checkCustomJSON(m, old *model.Profile) error {
	for _, f := range customFields {
		raw := f.raw(m)
		if old != nil && raw == f.raw(old) {
			continue
		}
		if err := checkJSONObject(raw); err != nil {
			return invalidArg("%s: %v", f.name, err)
		}
	}
	return nil
}

// checkJSONObject — пусто или JSON-объект в пределах maxCustomJSONBytes.
func checkJSONObject(raw string) error {
	if len(raw) > maxCustomJSONBytes {
		return fmt.Errorf("больше %d КиБ", maxCustomJSONBytes>>10)
	}
	canon, err := model.CanonicalJSON(raw)
	if err != nil {
		return err
	}
	if canon != "" && !strings.HasPrefix(canon, "{") {
		return fmt.Errorf("ожидается JSON-объект")
	}
	return nil
}
