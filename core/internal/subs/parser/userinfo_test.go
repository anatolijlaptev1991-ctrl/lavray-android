// SPDX-License-Identifier: GPL-3.0-only

package parser

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// T-54 (B1 §2.3): нецелое или пустое значение одного поля не отбрасывает
// остальные поля Subscription-Userinfo.
func TestUserInfoBadValueKeepsOtherFields(t *testing.T) {
	cases := []struct {
		name, value                         string
		upload, download, total, expireSecs *int64 // nil — поле не должно быть задано
	}{
		{"пустой expire в конце", "upload=1; download=2; total=3; expire=", t54i64(1), t54i64(2), t54i64(3), nil},
		{"пустой expire в начале", "expire=; upload=5; download=6; total=100", t54i64(5), t54i64(6), t54i64(100), nil},
		{"дробное total", "upload=1; download=2; total=1.5e9; expire=60", t54i64(1), t54i64(2), nil, t54i64(60)},
		{"отрицательное download", "upload=1; download=-2; total=3", t54i64(1), nil, t54i64(3), nil},
		{"слово вместо числа", "upload=1; download=abc; total=3; expire=60", t54i64(1), nil, t54i64(3), t54i64(60)},
		{"всё корректно", "upload=1; download=2; total=3; expire=60", t54i64(1), t54i64(2), t54i64(3), t54i64(60)},
	}
	for _, c := range cases {
		info := &model.SubscriptionInfo{}
		if err := ApplySubscriptionHeader(info, "subscription-userinfo", c.value); err != nil {
			t.Errorf("%s: %q → ошибка %v, ожидалось успешное применение годных полей", c.name, c.value, err)
			continue
		}
		check := func(field string, got, want *int64) {
			switch {
			case want == nil && got != nil:
				t.Errorf("%s: %s = %d, поле не должно быть задано", c.name, field, *got)
			case want != nil && got == nil:
				t.Errorf("%s: %s не задано, ожидалось %d", c.name, field, *want)
			case want != nil && *got != *want:
				t.Errorf("%s: %s = %d, ожидалось %d", c.name, field, *got, *want)
			}
		}
		check("upload", info.UploadBytes, c.upload)
		check("download", info.DownloadBytes, c.download)
		check("total", info.TotalBytes, c.total)
		var gotExpireSecs *int64
		if info.ExpireMs != nil {
			s := *info.ExpireMs / 1000
			gotExpireSecs = &s
		}
		check("expire", gotExpireSecs, c.expireSecs)
	}
}

// Ни одного годного поля — по-прежнему ошибка (прежнее поведение).
func TestUserInfoNoValidFieldIsStillAnError(t *testing.T) {
	for _, value := range []string{"upload=abc", "expire=", "total=-1", "foo=1", ""} {
		if err := ApplySubscriptionHeader(&model.SubscriptionInfo{}, "subscription-userinfo", value); err == nil {
			t.Errorf("%q: ошибка ожидалась (нет ни одного годного поля)", value)
		}
	}
}

// То же для строки «#subscription-userinfo:» в теле подписки.
func TestUserInfoBadValueInBodyKeepsInfo(t *testing.T) {
	body := "#subscription-userinfo: upload=1; download=2; total=; expire=\n" +
		"trojan://pw@example.org:443?security=tls#srv\n"
	res, err := Parse([]byte(body), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Info == nil || res.Info.UploadBytes == nil || *res.Info.UploadBytes != 1 || res.Info.DownloadBytes == nil || *res.Info.DownloadBytes != 2 {
		t.Fatalf("info потеряна: %+v", res.Info)
	}
	if len(res.Errors) != 0 {
		t.Errorf("годные поля не должны давать построчных ошибок: %+v", res.Errors)
	}
}

func t54i64(v int64) *int64 { return &v }
