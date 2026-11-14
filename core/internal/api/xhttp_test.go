// SPDX-License-Identifier: GPL-3.0-only

package api

// xhttp_test.go — ссылки XHTTP: импорт с предупреждением, профиль с причиной
// unsupported_reason, Connect отвечает FAILED_PRECONDITION / transport_unsupported.

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/links"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

const xhttpTestLink = "vless://11111111-1111-1111-1111-111111111111@xh.example.org:443" +
	"?type=xhttp&security=tls&sni=xh.example.org&path=%2Fsplit&mode=auto#xhttp-node"

// addXHTTP сохраняет профиль XHTTP, разобранный из ссылки, в группу 1.
func addXHTTP(t *testing.T, st *store.Store) *model.Profile {
	t.Helper()
	p, err := links.ParseLink(xhttpTestLink)
	if err != nil {
		t.Fatal(err)
	}
	p.GroupID = 1
	if err := st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// [GE] Connect по настоящему connect-клиенту: код FAILED_PRECONDITION и причина
// transport_unsupported в ErrorInfo; инстанс не запускается.
func TestConnectXHTTPFailsPrecondition(t *testing.T) {
	cs := newE2E(t)
	p := addXHTTP(t, cs.st)

	_, err := cs.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("код = %v, err = %v; ожидался FAILED_PRECONDITION", connect.CodeOf(err), err)
	}
	if got := errorReason(err); got != "transport_unsupported" {
		t.Errorf("reason = %q, ожидалось transport_unsupported", got)
	}
	st, serr := cs.core.GetStatus(context.Background(), connect.NewRequest(&lavrayv1.GetStatusRequest{}))
	if serr != nil {
		t.Fatal(serr)
	}
	if st.Msg.GetState() == lavrayv1.CoreState_CORE_STATE_CONNECTED {
		t.Error("ядро подключено, хотя транспорт не поддерживается")
	}
}

func TestConnectXHTTPValidateOnlyAlsoFails(t *testing.T) {
	cs := newE2E(t)
	p := addXHTTP(t, cs.st)
	_, err := cs.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID, ValidateOnly: boolp(true)}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || errorReason(err) != "transport_unsupported" {
		t.Fatalf("validate_only: код %v, reason %q", connect.CodeOf(err), errorReason(err))
	}
}

// [GE] Предпросмотр: ссылка XHTTP не отвергается, помечена и даёт строку
// «xhttp_unsupported: …» в журнале ответа; ws-ссылка рядом чиста.
func TestImportPreviewWarnsXHTTP(t *testing.T) {
	cs := newE2E(t)
	wsLink := "vless://11111111-1111-1111-1111-111111111111@ws.example.org:443?type=ws&security=tls#ws-node"
	res, err := cs.profile.ImportContent(context.Background(), connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:     strings.Join([]string{xhttpTestLink, wsLink}, "\n"),
		PreviewOnly: boolp(true),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.ParsedProfiles) != 2 {
		t.Fatalf("профилей %d, ожидалось 2", len(res.Msg.ParsedProfiles))
	}
	if got := res.Msg.ParsedProfiles[0].UnsupportedReason; got != "xhttp_unsupported" {
		t.Errorf("unsupported_reason = %q", got)
	}
	if got := res.Msg.ParsedProfiles[1].UnsupportedReason; got != "" {
		t.Errorf("ws-профиль помечен: %q", got)
	}
	var warns []string
	for _, line := range res.Msg.Log {
		if strings.HasPrefix(line, "xhttp_unsupported:") {
			warns = append(warns, line)
		}
	}
	if len(warns) != 1 {
		t.Fatalf("предупреждений xhttp_unsupported: %d, log = %v", len(warns), res.Msg.Log)
	}
	if list, _ := cs.st.ListProfiles(-1); len(list) != 0 {
		t.Errorf("предпросмотр сохранил профилей: %d", len(list))
	}
}

// Сохранение не теряет транспорт: список отдаёт причину и сеть XHTTP.
func TestListProfilesReportsUnsupportedReason(t *testing.T) {
	f := newSvcFixture(t, nil)
	addXHTTP(t, f.srv.st)
	f.trojan(t)
	res, err := f.srv.ListProfiles(context.Background(), newReq(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	networks := map[string]lavrayv1.StreamSettings_Network{}
	for _, p := range res.Msg.Profiles {
		reasons[p.Name] = p.UnsupportedReason
		networks[p.Name] = p.GetStream().GetNetwork()
	}
	if reasons["xhttp-node"] != "xhttp_unsupported" || reasons["p"] != "" {
		t.Errorf("причины = %v", reasons)
	}
	if networks["xhttp-node"] != lavrayv1.StreamSettings_NETWORK_XHTTP {
		t.Errorf("сеть = %v, ожидалась NETWORK_XHTTP", networks["xhttp-node"])
	}
}

// unsupported_reason вычисляется ядром: значение клиента при сохранении игнорируется.
func TestUnsupportedReasonIsReadOnly(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	in := profileToProto(p)
	in.UnsupportedReason = "xhttp_unsupported"
	m, err := profileToModel(in)
	if err != nil {
		t.Fatal(err)
	}
	if out := profileToProto(m); out.UnsupportedReason != "" {
		t.Errorf("значение клиента просочилось: %q", out.UnsupportedReason)
	}
}

// Сеть XHTTP, режим и extra переживают круг proto → модель → proto (редактор не
// превращает профиль в TCP и не стирает настройки сервера при UpdateProfile).
func TestXHTTPNetworkRoundTrip(t *testing.T) {
	const extra = `{"xPaddingBytes":"100-1000"}`
	in := &lavrayv1.StreamSettings{
		Network: lavrayv1.StreamSettings_NETWORK_XHTTP, Path: "/p", XhttpMode: "stream-up", XhttpExtra: extra,
	}
	m := streamToModel(in)
	if m.Network != "xhttp" || m.XHTTPMode != "stream-up" || m.XHTTPExtra != extra {
		t.Fatalf("модель: %+v", m)
	}
	out := streamToProto(m)
	if out.Network != lavrayv1.StreamSettings_NETWORK_XHTTP || out.XhttpMode != "stream-up" || out.XhttpExtra != extra {
		t.Errorf("proto: %+v", out)
	}
}

// Сохранённый профиль отдаёт mode и extra списком, а UpdateProfile их не стирает.
func TestXHTTPModeExtraSurviveStoreAndUpdate(t *testing.T) {
	f := newSvcFixture(t, nil)
	p, err := links.ParseLink(strings.Replace(xhttpTestLink, "#xhttp-node", "&extra=%7B%22a%22%3A1%7D#xhttp-node", 1))
	if err != nil {
		t.Fatal(err)
	}
	p.GroupID = 1
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	listed, err := f.srv.ListProfiles(context.Background(), newReq(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	got := listed.Msg.Profiles[0]
	if got.GetStream().GetXhttpMode() != "auto" || got.GetStream().GetXhttpExtra() != `{"a":1}` {
		t.Fatalf("список потерял mode/extra: %+v", got.GetStream())
	}
	got.Name = "renamed"
	if _, err := f.srv.UpdateProfile(context.Background(), newReq(&lavrayv1.UpdateProfileRequest{Profile: got})); err != nil {
		t.Fatal(err)
	}
	after, err := f.srv.st.GetProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stream.XHTTPMode != "auto" || after.Stream.XHTTPExtra != `{"a":1}` || after.Name != "renamed" {
		t.Fatalf("после UpdateProfile: %+v / %q", after.Stream, after.Name)
	}
}
