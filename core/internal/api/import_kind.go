// SPDX-License-Identifier: GPL-3.0-only

// import_kind.go — вид импортируемого контента (T-19). Ссылка на подписку и
// happ://crypt…, раскрывающийся в неё, не профиль: раньше парсер share-ссылок
// превращал «https://панель/sub/TOKEN» в ложный HTTP-прокси «панель:443».
package api

import (
	"net/url"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs/happ"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs/parser"
)

// parseHTTPURL разбирает ОДНУ строку вида http(s)://хост[...] без пробелов.
func parseHTTPURL(v string) (*url.URL, bool) {
	if v == "" || strings.ContainsAny(v, " \t\r\n") {
		return nil, false
	}
	low := strings.ToLower(v)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return nil, false
	}
	u, err := url.Parse(v)
	if err != nil || u.Hostname() == "" {
		return nil, false
	}
	return u, true
}

// subscriptionURLOf сообщает, что контент — ссылка на страницу подписки:
// http(s)-ссылка с путём или query. Ссылка HTTP-прокси — это
// «http(s)://[user:pass@]хост:порт[?security=…&sni=…][#имя]» без пути (так её
// печатает «Копировать ссылку»); она остаётся профилем.
func subscriptionURLOf(content string) (string, bool) {
	v := strings.TrimSpace(content)
	u, ok := parseHTTPURL(v)
	if !ok {
		return "", false
	}
	if strings.Trim(u.Path, "/") != "" {
		return v, true
	}
	for key := range u.Query() {
		if key != "security" && key != "sni" {
			return v, true
		}
	}
	return "", false
}

// isSubscriptionContent — контент (в том числе happ://crypt…, расшифрованный
// в процессе) является одной ссылкой на подписку. Ошибку расшифровки здесь не
// отдаём: её покажет парсер при обычном разборе.
func isSubscriptionContent(content string) bool {
	v := strings.TrimSpace(content)
	if happ.IsHappLink(v) {
		plain, err := happDecryptInProcess(v)
		if err != nil {
			return false
		}
		v = plain
	}
	_, ok := subscriptionURLOf(v)
	return ok
}

// contentKindOf — вид контента, разобранного в профили, по формату парсера.
func contentKindOf(content, format string) lavrayv1.ImportContentResponse_ContentKind {
	if happ.IsHappLink(strings.TrimSpace(content)) {
		return lavrayv1.ImportContentResponse_CONTENT_KIND_HAPP
	}
	switch format {
	case parser.FormatLines:
		return lavrayv1.ImportContentResponse_CONTENT_KIND_SHARE_LINKS
	case parser.FormatBase64:
		return lavrayv1.ImportContentResponse_CONTENT_KIND_BASE64
	case parser.FormatSingBox:
		return lavrayv1.ImportContentResponse_CONTENT_KIND_SINGBOX_JSON
	case parser.FormatClash:
		return lavrayv1.ImportContentResponse_CONTENT_KIND_CLASH_YAML
	}
	return lavrayv1.ImportContentResponse_CONTENT_KIND_UNSPECIFIED
}

// resolveSubscriptionURL приводит ввод пользователя к ссылке подписки:
// happ://crypt… расшифровывается до сохранения группы (как в NekoBox), прочее
// возвращается как есть. Обновление подписки понимает только http(s).
func resolveSubscriptionURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if !happ.IsHappLink(v) {
		return raw, nil
	}
	plain, err := happDecryptInProcess(v)
	if err != nil {
		return "", invalidArg("не удалось расшифровать ссылку: %v", err)
	}
	plain = strings.TrimSpace(plain)
	if _, ok := parseHTTPURL(plain); !ok {
		return "", invalidArg("расшифрованная ссылка не содержит ссылки на подписку")
	}
	return plain, nil
}
