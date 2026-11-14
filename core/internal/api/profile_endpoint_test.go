// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func socksWithPort(port int32) *lavrayv1.Profile {
	return &lavrayv1.Profile{
		GroupId: 1, Type: lavrayv1.ProfileType_PROFILE_TYPE_SOCKS, Name: "d5",
		Address: "127.0.0.1", Port: port,
		Detail: &lavrayv1.Profile_SocksHttp{SocksHttp: &lavrayv1.SocksHttpDetail{Proto: lavrayv1.SocksHttpDetail_PROTO_SOCKS5}},
	}
}

// D-5: CreateProfile с портом вне 1–65535 — INVALID_ARGUMENT, границы проходят.
func TestCreateProfileRejectsPortOutOfRange(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	for _, port := range []int32{0, -1, 65536} {
		_, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: socksWithPort(port)}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("port %d: %v, ожидался INVALID_ARGUMENT", port, err)
		}
	}
	for _, port := range []int32{1, 65535} {
		if _, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: socksWithPort(port)})); err != nil {
			t.Errorf("port %d: %v", port, err)
		}
	}
}

// Профиль с port 0 из старой базы переименовывается, а сменить порт на 0 нельзя.
func TestUpdateProfileKeepsLegacyPortButRejectsNewZero(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	legacy := &model.Profile{GroupID: 1, Type: model.TypeSOCKS, Name: "old", Address: "127.0.0.1",
		Detail: &model.SocksHTTPDetail{Proto: "socks5"}}
	if err := cs.st.CreateProfile(legacy); err != nil {
		t.Fatal(err)
	}
	got, err := cs.profile.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: legacy.ID}))
	if err != nil {
		t.Fatal(err)
	}
	renamed := got.Msg
	renamed.Name = "renamed"
	if _, err := cs.profile.UpdateProfile(ctx, connect.NewRequest(&lavrayv1.UpdateProfileRequest{Profile: renamed})); err != nil {
		t.Fatalf("переименование старого профиля с port 0: %v", err)
	}
	created, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: socksWithPort(1080)}))
	if err != nil {
		t.Fatal(err)
	}
	zero := created.Msg
	zero.Port = 0
	_, err = cs.profile.UpdateProfile(ctx, connect.NewRequest(&lavrayv1.UpdateProfileRequest{Profile: zero}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("смена порта на 0: %v, ожидался INVALID_ARGUMENT", err)
	}
}

func TestCheckEndpointSkipsTypesWithoutServer(t *testing.T) {
	for typ := range endpointless {
		if err := checkEndpoint(&model.Profile{Type: typ}, nil); err != nil {
			t.Errorf("%s без порта: %v", typ, err)
		}
	}
	if err := checkEndpoint(&model.Profile{Type: model.TypeHysteria2}, nil); err == nil {
		t.Error("hysteria2 без порта принят")
	}
}
