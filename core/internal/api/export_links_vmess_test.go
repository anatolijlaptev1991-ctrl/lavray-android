// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"strings"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func (f *svcFixture) vmessLink(t *testing.T, id int64, format lavrayv1.ExportLinksRequest_Format) string {
	t.Helper()
	res, err := f.srv.ExportLinks(context.Background(), newReq(&lavrayv1.ExportLinksRequest{Ids: []int64{id}, Format: format}))
	if err != nil || len(res.Msg.Links) != 1 {
		t.Fatalf("ExportLinks: %v, %v", res, err)
	}
	return res.Msg.Links[0]
}

func (f *svcFixture) addVMess(t *testing.T) int64 {
	t.Helper()
	p := &model.Profile{GroupID: 1, Type: model.TypeVMess, Name: "vm", Address: "vm.example.org", Port: 443,
		Detail: &model.VMessDetail{UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Security: "auto"}}
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// isV2RayN: vmess://<base64 JSON> (старый формат); URL-формат содержит user@host.
func isV2RayN(link string) bool {
	return strings.HasPrefix(link, "vmess://") && !strings.Contains(link, "@")
}

// share.vmess_old_format задаёт формат vmess по умолчанию: по умолчанию (как у
// NekoBox) — старый v2rayN, выключенная настройка — URL-формат. Явный
// FORMAT_VMESS_V2RAYN от настройки не зависит.
func TestExportLinksVMessOldFormatSetting(t *testing.T) {
	f := newSvcFixture(t, nil)
	id := f.addVMess(t)

	if link := f.vmessLink(t, id, lavrayv1.ExportLinksRequest_FORMAT_STANDARD); !isV2RayN(link) {
		t.Errorf("по умолчанию ожидался формат v2rayN: %s", link)
	}
	if err := f.srv.st.SetSetting("share.vmess_old_format", "false"); err != nil {
		t.Fatal(err)
	}
	if link := f.vmessLink(t, id, lavrayv1.ExportLinksRequest_FORMAT_STANDARD); isV2RayN(link) {
		t.Errorf("при выключенной настройке ожидался URL-формат: %s", link)
	}
	if link := f.vmessLink(t, id, lavrayv1.ExportLinksRequest_FORMAT_VMESS_V2RAYN); !isV2RayN(link) {
		t.Errorf("явный FORMAT_VMESS_V2RAYN должен давать v2rayN: %s", link)
	}
	if err := f.srv.st.SetSetting("share.vmess_old_format", "true"); err != nil {
		t.Fatal(err)
	}
	if link := f.vmessLink(t, id, lavrayv1.ExportLinksRequest_FORMAT_STANDARD); !isV2RayN(link) {
		t.Errorf("при включённой настройке ожидался v2rayN: %s", link)
	}
}

// Настройка касается только vmess: остальные типы выходят как раньше.
func TestExportLinksOtherTypesIgnoreVMessSetting(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.addTrojan(t, "t", "t.example.org", 443, "pw")
	before := f.vmessLink(t, p.ID, lavrayv1.ExportLinksRequest_FORMAT_STANDARD)
	if err := f.srv.st.SetSetting("share.vmess_old_format", "false"); err != nil {
		t.Fatal(err)
	}
	if after := f.vmessLink(t, p.ID, lavrayv1.ExportLinksRequest_FORMAT_STANDARD); after != before {
		t.Fatalf("trojan-ссылка изменилась: %s -> %s", before, after)
	}
}
