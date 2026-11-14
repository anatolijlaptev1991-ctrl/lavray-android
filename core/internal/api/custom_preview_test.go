// SPDX-License-Identifier: GPL-3.0-only

package api

// custom_preview_test.go — маскирование секретов в diff PreviewCustomFields и
// предупреждения (T-08B, ревью).

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func TestRedactTreeMasksByKeyAndPEM(t *testing.T) {
	tree := map[string]any{
		"tls": map[string]any{
			"key":         []any{"-----BEGIN " + "PRIVATE KEY-----", "MIIEvQIBADANBgkq", "-----END PRIVATE KEY-----"},
			"client_key":  "-----BEGIN EC " + "PRIVATE KEY-----\nAAAA\n-----END EC PRIVATE KEY-----",
			"certificate": "-----BEGIN CERTIFICATE-----\nPUBLICBUTPEM\n-----END CERTIFICATE-----",
			"server_name": "example.com",
		},
		"transport": map[string]any{"headers": map[string]any{"Cookie": "sess=TOPSECRETCOOKIE", "Authorization": "Token abcdef123"}},
		"password":  "Sup3rS3cret",
		"auth":      map[string]any{"nested": "deepsecret", "n": 1234},
		"uuid_note": "id 2f1f9d24-5c43-4f8e-9a0a-0f6c4d7d5e11 in text",
		"port":      443,
	}
	out := fmt.Sprint(redactTree(tree, false))
	for _, secret := range []string{"MIIEvQIBADANBgkq", "AAAA", "PUBLICBUTPEM", "TOPSECRETCOOKIE", "abcdef123", "Sup3rS3cret", "deepsecret", "1234", "2f1f9d24-5c43"} {
		if strings.Contains(out, secret) {
			t.Errorf("не замаскировано %q: %s", secret, out)
		}
	}
	for _, keep := range []string{"example.com", "443"} {
		if !strings.Contains(out, keep) {
			t.Errorf("несекретное %q пропало: %s", keep, out)
		}
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a1, a2, b := fingerprint("alpha"), fingerprint("alpha"), fingerprint("beta")
	if a1 != a2 || a1 == b || !regexp.MustCompile(`^\[redacted:[0-9a-f]{6}\]$`).MatchString(a1) {
		t.Fatalf("отпечатки: %q %q %q", a1, a2, b)
	}
}

func TestPreviewMasksPEMAndHeaders(t *testing.T) {
	cs := newE2E(t)
	out := `{"tls":{"enabled":true,"server_name":"tr.example.com","client_key":["-----BEGIN ` + "PRIVATE KEY-----" + `","MIIEPEMLINE","-----END PRIVATE KEY-----"]},` +
		`"transport":{"type":"ws","path":"/x","headers":{"Cookie":"sess=TOPSECRETCOOKIE","Authorization":"Token abcdef123"}}}`
	p := trojanWithCustom(t, cs, out, "")
	d := previewOf(t, cs, p.GetId()).GetDiff()
	for _, secret := range []string{"MIIEPEMLINE", "TOPSECRETCOOKIE", "abcdef123", "Sup3rS3cretPassw0rd"} {
		if strings.Contains(d, secret) {
			t.Errorf("в diff секрет %q:\n%s", secret, d)
		}
	}
	if !strings.Contains(d, "[redacted:") || !strings.Contains(d, "/x") {
		t.Fatalf("в diff нет замаскированных/обычных значений:\n%s", d)
	}
}

func TestPreviewShowsChangedSecret(t *testing.T) {
	// custom_outbound меняет только пароль: diff не пуст, старое и новое
	// значения видны как разные отпечатки, но не в открытом виде.
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, `{"password":"N3wS3cretValue"}`, "")
	pv := previewOf(t, cs, p.GetId())
	d := pv.GetDiff()
	if strings.Contains(d, "N3wS3cretValue") || strings.Contains(d, "Sup3rS3cretPassw0rd") {
		t.Fatalf("пароль в diff:\n%s", d)
	}
	var minus, plus string
	for _, line := range strings.Split(d, "\n") {
		if strings.Contains(line, `"password"`) && strings.HasPrefix(line, "-") {
			minus = line
		}
		if strings.Contains(line, `"password"`) && strings.HasPrefix(line, "+") {
			plus = line
		}
	}
	if minus == "" || plus == "" || strings.TrimPrefix(minus, "-") == strings.TrimPrefix(plus, "+") {
		t.Fatalf("смена пароля не видна в diff:\n%s", d)
	}
	if !slices.ContainsFunc(pv.GetTouchedKeys(), func(k string) bool { return strings.HasSuffix(k, ".password") }) {
		t.Fatalf("touched_keys без password: %v", pv.GetTouchedKeys())
	}
}

func TestConnectWarnsUnconfirmedCustom(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	resp, err := cs.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(p.GetId()), ValidateOnly: boolp(true)}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(resp.Msg.GetWarnings(), warnCustomUnconfirmed) {
		t.Fatalf("нет предупреждения %q: %v", warnCustomUnconfirmed, resp.Msg.GetWarnings())
	}
	confirmByHash(t, cs, p.GetId())
	resp, err = cs.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(p.GetId()), ValidateOnly: boolp(true)}))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(resp.Msg.GetWarnings(), warnCustomUnconfirmed) {
		t.Fatal("после подтверждения предупреждения нет")
	}
}

func TestCloneProfileIsUnconfirmed(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	confirmByHash(t, cs, p.GetId())
	resp, err := cs.profile.CloneProfiles(context.Background(), connect.NewRequest(&lavrayv1.CloneProfilesRequest{Ids: []int64{p.GetId()}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Msg.GetNewIds()) != 1 {
		t.Fatalf("new_ids = %v", resp.Msg.GetNewIds())
	}
	for _, id := range resp.Msg.GetNewIds() {
		if confirmed(t, cs, id) {
			t.Fatal("клон унаследовал подтверждение: его содержимое не проходило diff")
		}
	}
	if !confirmed(t, cs, p.GetId()) {
		t.Fatal("клонирование сняло подтверждение оригинала")
	}
}

func TestInboundsWarningBeyondTouchedCap(t *testing.T) {
	// Предупреждение считается по полному списку путей, а не по первым 100.
	var keys []string
	for i := range maxTouchedKeys + 50 {
		keys = append(keys, fmt.Sprintf("dns.k%03d", i))
	}
	keys = append(keys, "inbounds[0].listen")
	full := &model.Profile{}
	if w := customWarnings(full, keys); !slices.Contains(w, warnInboundsChanged) {
		t.Fatalf("warnings = %v", w)
	}
	if n := len(capTouchedKeys(keys)); n != maxTouchedKeys {
		t.Fatalf("в ответе %d путей, ждём %d", n, maxTouchedKeys)
	}
}
