// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// fileGrabConfig — custom_config, который задаёт experimental.cache_file.path
// на чужой файл (T-08, A1-01).
const fileGrabConfig = `{"experimental":{"cache_file":{"enabled":true,` +
	`"path":"C:/Users/victim/Documents/diploma.docx"}}}`

func fileGrabProfile() *lavrayv1.Profile {
	return &lavrayv1.Profile{
		Type:             lavrayv1.ProfileType_PROFILE_TYPE_SOCKS,
		Name:             "inline",
		Address:          "198.51.100.7",
		Port:             1080,
		Detail:           &lavrayv1.Profile_SocksHttp{SocksHttp: &lavrayv1.SocksHttpDetail{Proto: lavrayv1.SocksHttpDetail_PROTO_SOCKS5}},
		CustomConfigJson: strp(fileGrabConfig),
		// Клиент сам ставит флаг подтверждения — ядро его не читает (T-08B).
		CustomFieldsConfirmed: true,
	}
}

// T-08B: custom-поля inline-профиля не применяются никогда — ни при
// validate_only, ни при настоящем подключении; ответ предупреждает об этом,
// а путь к чужому файлу в конфиг инстанса не попадает.
func TestConnectInlineIgnoresConfirmedFlag(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	for _, validateOnly := range []bool{true, false} {
		resp, err := cs.core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{
			InlineProfile: fileGrabProfile(),
			ValidateOnly:  boolp(validateOnly),
		}))
		if err != nil {
			t.Fatalf("validate_only=%v: %v", validateOnly, err)
		}
		if !slices.Contains(resp.Msg.GetWarnings(), warnInlineCustomIgnored) {
			t.Fatalf("validate_only=%v: нет предупреждения %q: %v", validateOnly, warnInlineCustomIgnored, resp.Msg.GetWarnings())
		}
	}
	rc, err := cs.core.GetRuntimeConfig(ctx, connect.NewRequest(&lavrayv1.GetRuntimeConfigRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	// cache_file в конфиге есть всегда — его путь задаёт сам LAVRay; чужого пути быть не должно.
	if cfg := rc.Msg.GetSingBoxJson(); strings.Contains(cfg, "diploma.docx") || strings.Contains(cfg, "victim") {
		t.Fatalf("custom_config inline-профиля попал в конфиг инстанса:\n%s", cfg)
	}
}

// T-08: и подтверждённое содержимое не может открыть чужой файл — сборка
// отвечает INVALID_ARGUMENT и инстанс не запускается.
func TestConnectRefusesCustomConfigFilePath(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	in := fileGrabProfile()
	in.GroupId = 1
	created, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: in}))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetId()
	confirmByHash(t, cs, id)

	for _, validateOnly := range []bool{true, false} {
		_, err := cs.core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{
			ProfileId:    int64p(id),
			ValidateOnly: boolp(validateOnly),
		}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("validate_only=%v: code=%v err=%v, ожидался INVALID_ARGUMENT", validateOnly, connect.CodeOf(err), err)
		}
		if !strings.Contains(err.Error(), "experimental") {
			t.Fatalf("validate_only=%v: в сообщении нет ключа: %v", validateOnly, err)
		}
	}
	st, err := cs.core.GetStatus(ctx, connect.NewRequest(&lavrayv1.GetStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if st.Msg.GetState() != lavrayv1.CoreState_CORE_STATE_IDLE {
		t.Fatalf("инстанс запущен после отказа: %v", st.Msg.GetState())
	}
}
