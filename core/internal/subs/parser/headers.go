// SPDX-License-Identifier: GPL-3.0-only

package parser

// Заголовки подписки FR-UI-262: HTTP-заголовки ответа (разбирает subscriber)
// и строки «#key: value» в теле (разбирает parser). Общий формат значений —
// здесь. «Без лимита»/«Бессрочно» выводит UI по нулевым полям Info.

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// Известные ключи заголовков подписки (lowercase).
const (
	hdrUserInfo     = "subscription-userinfo"
	hdrTitle        = "profile-title"
	hdrInterval     = "profile-update-interval"
	hdrWebPageURL   = "profile-web-page-url"
	hdrSupportURL   = "support-url"
	hdrAnnounce     = "announce"
	hdrRouting      = "routing"
	hdrDisposition  = "content-disposition"
	titleBase64Pref = "base64:"
)

// splitBodyHeader разбирает строку «#key: value» (key — известный заголовок
// подписки); ok=false — обычный комментарий.
func splitBodyHeader(line string) (key, value string, ok bool) {
	s := strings.TrimPrefix(line, "#")
	k, v, has := strings.Cut(s, ":")
	if !has {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(k))
	switch key {
	case hdrUserInfo, hdrTitle, hdrInterval, hdrWebPageURL, hdrSupportURL, hdrAnnounce, hdrRouting:
		return key, strings.TrimSpace(v), true
	}
	return "", "", false
}

// ApplySubscriptionHeader применяет значение заголовка подписки к Info.
// Поддерживает и HTTP-заголовки ответа, и строки в теле (ключи совпадают;
// content-disposition имеет смысл только в HTTP — см. subscriber).
func ApplySubscriptionHeader(info *model.SubscriptionInfo, key, value string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case hdrUserInfo:
		return applyUserInfo(info, value)
	case hdrTitle:
		info.Title = decodeTitle(value)
		return nil
	case hdrInterval:
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || n < 0 {
			return fmt.Errorf("profile-update-interval: %q не является числом часов", value)
		}
		h := int32(n)
		info.UpdateIntervalHours = &h
		return nil
	case hdrWebPageURL:
		info.WebPageURL = strings.TrimSpace(value)
		return nil
	case hdrSupportURL:
		info.SupportURL = strings.TrimSpace(value)
		return nil
	case hdrAnnounce:
		info.Announce = strings.TrimSpace(value)
		return nil
	case hdrRouting:
		info.Routing = strings.TrimSpace(value)
		return nil
	default:
		return fmt.Errorf("неизвестный заголовок подписки %q", key)
	}
}

// applyUserInfo — «upload=1234; download=5678; total=91011; expire=1700000000»
// (байты; expire — unix-секунды → миллисекунды).
func applyUserInfo(info *model.SubscriptionInfo, value string) error {
	found := false
	firstBad := ""
	for _, part := range strings.Split(value, ";") {
		k, v, has := strings.Cut(part, "=")
		if !has {
			continue
		}
		v = strings.TrimSpace(v)
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			// T-54: пустое или нечисловое значение («expire=» у бессрочной
			// подписки, «total=inf») пропускает только своё поле, а не
			// обрывает разбор остальных.
			if firstBad == "" {
				firstBad = strings.TrimSpace(part)
			}
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "upload":
			info.UploadBytes = &n
			found = true
		case "download":
			info.DownloadBytes = &n
			found = true
		case "total":
			info.TotalBytes = &n
			found = true
		case "expire":
			ms := n * 1000
			info.ExpireMs = &ms
			found = true
		}
	}
	if !found {
		if firstBad != "" {
			return fmt.Errorf("subscription-userinfo: %q не является числом", firstBad)
		}
		return fmt.Errorf("subscription-userinfo: не найдено ни одного поля (upload/download/total/expire)")
	}
	return nil
}

// decodeTitle — profile-title: «base64:…» → декод (std/url, автопаддинг),
// иначе значение как есть.
func decodeTitle(value string) string {
	if strings.HasPrefix(strings.ToLower(value), titleBase64Pref) {
		if b := decodeB64Loose(value[len(titleBase64Pref):]); b != nil {
			return strings.ToValidUTF8(string(b), "�")
		}
		return ""
	}
	return strings.ToValidUTF8(value, "�")
}

// TitleFromDisposition — имя файла из content-disposition:
// attachment; filename=… (quoted-string или token); base64:… — как есть.
func TitleFromDisposition(value string) string {
	for _, part := range strings.Split(value, ";") {
		k, v, has := strings.Cut(part, "=")
		if !has || !strings.EqualFold(strings.TrimSpace(k), "filename") {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		if v == "" {
			return ""
		}
		return strings.ToValidUTF8(v, "�")
	}
	return ""
}

// encodeTitleB64 нужен тестам roundtrip заголовка (экспорт не используется
// в рантайме вне отладки).
func encodeTitleB64(s string) string {
	return titleBase64Pref + base64.StdEncoding.EncodeToString([]byte(s))
}
