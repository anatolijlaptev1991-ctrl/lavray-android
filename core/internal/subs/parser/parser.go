// SPDX-License-Identifier: GPL-3.0-only

// Package parser разбирает содержимое ответа подписки в профили
// (W3, core-port-plan §4.1). Поддерживаемые форматы с авто-детекцией:
//
//   - base64-блоб (std/urlsafe, автопаддинг) → рекурсивный разбор декода;
//   - многострочник share-ссылок (plain, построчно);
//   - sing-box JSON (объект с "outbounds" или верхний уровень — массив;
//     FR-UI-264) — только известные типы outbounds, неизвестные skip;
//   - happ://-ссылки внутри содержимого → внутренняя расшифровка
//     (internal/subs/happ, без подпроцесса — UX-15).
//
// Заголовки подписки FR-UI-262 из тела (строки «#key: value») попадают в
// model.SubscriptionInfo; HTTP-заголовки ответа разбирает subscriber.
//
// Анти-деструктивность (LNB-29): пустой ответ и HTML-заглушка — явные ошибки,
// а не «0 профилей»; ошибки отдельных строк собираются в Result.Errors и не
// валят весь пакет. Паник нет (инвариант FuzzParseContent).
//
// Clash / Clash.Meta YAML — см. clash.go (proxies:-список в профили).
package parser

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/links"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs/happ"
)

// Лимиты (LNB-12: память и число профилей).
const (
	// MaxContentBytes — лимит размера содержимого подписки (64 МиБ, план §4.1).
	MaxContentBytes = 64 << 20
	// MaxClashYAMLBytes — лимит Clash/Clash.Meta YAML (T-50, A1-14): разбор
	// YAML раздувает память в ~36 раз (60 МиБ → 2,2 ГиБ ОЗУ, замер A1);
	// реальные подписки — десятки КиБ, файлы с большими rules — единицы МиБ.
	MaxClashYAMLBytes = 8 << 20
	// DefaultMaxProfiles — лимит числа профилей из одной подписки.
	DefaultMaxProfiles = 10000
	maxNesting         = 3 // base64/happ рекурсия
)

// Форматы результата.
const (
	FormatLines      = "lines"        // plain-многострочник (после декода base64/happ)
	FormatBase64     = "base64"       // верхний уровень — base64-блоб
	FormatSingBox    = "singbox-json" // sing-box JSON
	FormatHapp       = "happ"         // расшифрованный happ://
	FormatClash      = "clash-yaml"   // Clash / Clash.Meta YAML (FR-UI-119/121)
	ErrEmptyContent  = "пустой ответ подписки"
	ErrHTMLStubMsg   = "ответ похож на HTML-заглушку (captive portal / DPI)"
	ErrTooLargeMsg   = "содержимое подписки превышает лимит"
	errUnrecognized  = "формат подписки не распознан"
	maxErrLineRunes  = 96
	maxStoredURLErrs = 100
)

// Options — параметры разбора (нулевые значения заменяются дефолтами).
type Options struct {
	// MaxProfiles — лимит профилей; DefaultMaxProfiles при 0.
	MaxProfiles int
	// DecryptHapp — расшифровка happ-ссылки (инъекция для тестов; nil → happ.Decrypt).
	DecryptHapp func(string) (string, error)
}

func (o *Options) maxProfiles() int {
	if o.MaxProfiles <= 0 {
		return DefaultMaxProfiles
	}
	return o.MaxProfiles
}

func (o *Options) decryptHapp() func(string) (string, error) {
	if o.DecryptHapp == nil {
		return happ.Decrypt
	}
	return o.DecryptHapp
}

// LineError — ошибка одной строки многострочника (не валит пакет).
type LineError struct {
	Line int    `json:"line"` // номер строки (1-based) в текущем слое содержимого
	Text string `json:"text"` // усечённый текст строки
	Err  string `json:"err"`
}

// Result — итог разбора содержимого подписки.
type Result struct {
	Format   string                  `json:"format"`
	Profiles []*model.Profile        `json:"profiles"`
	Info     *model.SubscriptionInfo `json:"info,omitempty"`     // из строк «#key: value» в теле
	Warnings []string                `json:"warnings,omitempty"` // preview-предупреждения (LNB-25 и пр.)
	Errors   []LineError             `json:"errors,omitempty"`   // построчные ошибки
	Skipped  int                     `json:"skipped"`            // пропущено (unknown в sing-box JSON и пр.)
}

// Error — структурная ошибка парсера.
type Error struct {
	Msg   string
	Inner error
}

func (e *Error) Error() string {
	if e.Inner != nil {
		return e.Msg + ": " + e.Inner.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Inner }

// Parse разбирает содержимое подписки. Возвраты:
//   - (*Result, nil) — есть профили (или есть построчные ошибки в Errors);
//   - (nil, *Error) — пустой ответ, HTML-заглушка, превышение лимита,
//     формат не распознан (0 профилей и 0 распознанных строк).
func Parse(content []byte, opts Options) (*Result, error) {
	if len(content) > MaxContentBytes {
		return nil, &Error{Msg: ErrTooLargeMsg}
	}
	s := strings.TrimSpace(string(content))
	if s == "" {
		return nil, &Error{Msg: ErrEmptyContent}
	}
	if looksLikeHTML(s) {
		return nil, &Error{Msg: ErrHTMLStubMsg}
	}

	p := &parserState{opts: opts, maxProfiles: opts.maxProfiles(), decryptHapp: opts.decryptHapp()}

	// 1) sing-box JSON (объект с outbounds или массив).
	if c := firstByte(s); c == '{' || c == '[' {
		if res, handled, err := parseSingBoxJSON([]byte(s), p.maxProfiles); handled {
			if err != nil {
				return nil, &Error{Msg: err.Error()}
			}
			return res, nil
		}
		// Валидный JSON без outbounds — не пробуем дальше (LNB-16: явная
		// ошибка вместо молчаливых 0 профилей).
		return nil, &Error{Msg: errUnrecognized + " (JSON без массива outbounds)"}
	}

	// 1b) Clash / Clash.Meta YAML (FR-UI-119/121): большинство панелей
	// отдают именно этот формат. Детектор дешёвый (строка «proxies:»),
	// полный YAML-разбор — только после него.
	if looksLikeClash(s) {
		if len(s) > MaxClashYAMLBytes {
			// Msg остаётся ErrTooLargeMsg: api.mapErr по нему выбирает
			// RESOURCE_EXHAUSTED; подробности — в Inner.
			return nil, &Error{Msg: ErrTooLargeMsg, Inner: fmt.Errorf(
				"Clash YAML %d МиБ при лимите %d МиБ", (len(s)+(1<<20)-1)>>20, MaxClashYAMLBytes>>20)}
		}
		res, err := parseClash([]byte(s), opts)
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	// 2) base64-блоб → рекурсия.
	if decoded, ok := decodeB64IfValid(s); ok {
		res, err := p.parse(decoded, 1)
		if err != nil {
			return nil, err
		}
		res.Format = FormatBase64
		return res, nil
	}

	// 3) многострочник (plain).
	res, err := p.parse([]byte(s), 0)
	if err != nil {
		return nil, err
	}
	res.Format = FormatLines
	return res, nil
}

// parserState — параметры, разделяемые между уровнями рекурсии.
type parserState struct {
	opts        Options
	maxProfiles int
	decryptHapp func(string) (string, error)
}

// parse — разбор «слоя» содержимого (многострочник); depth — текущая
// вложенность (base64/happ).
func (p *parserState) parse(content []byte, depth int) (*Result, error) {
	res := &Result{Format: FormatLines}
	info := &model.SubscriptionInfo{}
	haveInfo := false
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Строки «#key: value» — заголовки подписки в теле (FR-UI-262);
		// прочие «#…» — комментарии.
		if strings.HasPrefix(line, "#") {
			if key, value, ok := splitBodyHeader(line); ok {
				if err := ApplySubscriptionHeader(info, key, value); err != nil {
					res.Errors = append(res.Errors, LineError{Line: i + 1, Text: truncateRunes(line), Err: err.Error()})
				} else {
					haveInfo = true
				}
			}
			continue
		}
		if len(res.Profiles) >= p.maxProfiles {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("достигнут лимит числа профилей (%d) — остальные строки пропущены", p.maxProfiles))
			break
		}

		// happ:// внутри содержимого — расшифровать и разобрать рекурсивно.
		if happ.IsHappLink(line) {
			decoded, err := p.decryptHapp(line)
			if err != nil {
				res.Errors = append(res.Errors, LineError{Line: i + 1, Text: truncateRunes(line), Err: "happ: " + err.Error()})
				continue
			}
			if depth+1 >= maxNesting {
				res.Errors = append(res.Errors, LineError{Line: i + 1, Text: truncateRunes(line), Err: "happ: превышена вложенность расшифровки"})
				continue
			}
			sub, err := p.parse([]byte(decoded), depth+1)
			if err != nil {
				res.Errors = append(res.Errors, LineError{Line: i + 1, Text: truncateRunes(line), Err: "happ: " + err.Error()})
				continue
			}
			for _, prof := range sub.Profiles {
				if len(res.Profiles) >= p.maxProfiles {
					break
				}
				res.Profiles = append(res.Profiles, prof)
			}
			if sub.Info != nil {
				mergeInfo(info, sub.Info)
				haveInfo = true
			}
			res.Warnings = append(res.Warnings, sub.Warnings...)
			res.Errors = append(res.Errors, sub.Errors...)
			continue
		}

		prof, err := links.ParseLink(line)
		if err != nil {
			if len(res.Errors) < maxStoredURLErrs {
				res.Errors = append(res.Errors, LineError{Line: i + 1, Text: truncateRunes(line), Err: err.Error()})
			}
			continue
		}
		// LNB-25/FR-UI-257: c_cfg/c_out отбрасывает сам links.ParseLink
		// (neko.go), а core/cmd/cs для custom-типов до профиля ДОХОДЯТ —
		// здесь, в subscription-пути, они ОБНУЛЯЮТСЯ (подписка не может
		// доставить произвольную команду/конфиг ядра) и добавляется
		// preview-предупреждение для UI. links.ParseLink в общем виде не
		// меняется — экспорт BuildNekoLink сохраняет полный bean.
		if strings.HasPrefix(strings.ToLower(line), "nekoray://") {
			if w := nekoCustomFieldsWarning(line); w != "" {
				res.Warnings = append(res.Warnings, fmt.Sprintf("строка %d: %s", i+1, w))
			}
			stripNekoCustomCore(prof)
		}
		prof.GroupID = 0 // назначает subscriber/store при применении
		res.Profiles = append(res.Profiles, prof)
	}
	if haveInfo {
		res.Info = info
	}
	if len(res.Profiles) == 0 && len(res.Errors) == 0 {
		return nil, &Error{Msg: errUnrecognized}
	}
	if len(res.Profiles) == 0 && len(res.Errors) > 0 {
		// Ни одного профиля, все строки с ошибками — деструктивно применять
		// нельзя; отдаём ошибку с первыми деталями (LNB-29).
		first := res.Errors[0]
		return nil, &Error{Msg: fmt.Sprintf("не распознано ни одного профиля (первая ошибка: строка %d: %s)", first.Line, first.Err)}
	}
	return res, nil
}

// stripNekoCustomCore обнуляет поля ядра custom-профиля, доставленные
// neko-ссылкой (W3-1, LNB-25/FR-UI-257): Command (произвольная команда
// TypeCustomExternal), Core/ExternalCoreName, OutboundJSON/FullConfigJSON/
// ConfigTemplate (значение «cs» bean — произвольный конфиг ядра). Профиль
// сохраняется как каркас (имя/тип/адрес и пр.); конфиг и команду
// пользователь восстанавливает вручную. CustomFieldsConfirmed не задаётся
// (остаётся false). Применяется ТОЛЬКО в subscription-пути — links.ParseLink
// в общем виде не меняется (roundtrip BuildNekoLink/ParseLink сохранён).
func stripNekoCustomCore(p *model.Profile) {
	d, ok := p.Detail.(*model.CustomDetail)
	if !ok {
		return
	}
	d.Command = nil
	d.Core = ""
	d.ExternalCoreName = ""
	d.OutboundJSON = ""
	d.FullConfigJSON = ""
	d.ConfigTemplate = ""
}

// nekoCustomFieldsWarning проверяет наличие custom-полей в neko-ссылке
// (без их применения — только предупреждение preview, FR-UI-257).
func nekoCustomFieldsWarning(link string) string {
	const prefix = "nekoray://"
	body := link[len(prefix):]
	_, b64, has := strings.Cut(body, "#")
	if !has {
		return ""
	}
	raw := decodeB64Loose(b64)
	if raw == nil {
		return ""
	}
	var bean map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bean); err != nil {
		return ""
	}
	var found []string
	for _, key := range []string{"c_cfg", "c_out", "core", "cmd", "cs"} {
		v, ok := bean[key]
		if !ok {
			continue
		}
		if s := strings.TrimSpace(strings.Trim(string(v), `"`)); s != "" && s != "[]" && s != "null" {
			found = append(found, key)
		}
	}
	if len(found) == 0 {
		return ""
	}
	return "neko-профиль содержит " + strings.Join(found, ", ") + " — поля отброшены при импорте (LNB-25); включить вручную можно только с diff и подтверждением"
}

// mergeInfo — заголовки внутреннего слоя (happ) не перетирают уже заданные.
func mergeInfo(dst, src *model.SubscriptionInfo) {
	if src.UploadBytes != nil {
		dst.UploadBytes = src.UploadBytes
	}
	if src.DownloadBytes != nil {
		dst.DownloadBytes = src.DownloadBytes
	}
	if src.TotalBytes != nil {
		dst.TotalBytes = src.TotalBytes
	}
	if src.ExpireMs != nil {
		dst.ExpireMs = src.ExpireMs
	}
	if src.UpdateIntervalHours != nil {
		dst.UpdateIntervalHours = src.UpdateIntervalHours
	}
	if src.Title != "" && dst.Title == "" {
		dst.Title = src.Title
	}
	if src.WebPageURL != "" && dst.WebPageURL == "" {
		dst.WebPageURL = src.WebPageURL
	}
	if src.SupportURL != "" && dst.SupportURL == "" {
		dst.SupportURL = src.SupportURL
	}
	if src.Announce != "" && dst.Announce == "" {
		dst.Announce = src.Announce
	}
	if src.Routing != "" && dst.Routing == "" {
		dst.Routing = src.Routing
	}
}

// firstByte — первый непробельный байт.
func firstByte(s string) byte {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			return s[i]
		}
	}
	return 0
}

// looksLikeHTML — детекция HTML-заглушки по структуре (не подстроке — LNB-16).
func looksLikeHTML(s string) bool {
	head := strings.ToLower(s)
	if len(head) > 512 {
		head = head[:512]
	}
	return strings.HasPrefix(head, "<!doctype html") ||
		strings.HasPrefix(head, "<html") ||
		strings.HasPrefix(head, "<?xml")
}

// decodeB64IfValid — DecodeB64IfValid плана §4.1: весь текст (пробелы
// вырезаются) должен быть корректным base64 (std/url, автопаддинг), а декод —
// валидным UTF-8. Некорректный padding отвергается (deviations.yaml).
func decodeB64IfValid(s string) ([]byte, bool) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\n' || c == '\r' || c == ' ' || c == '\t':
			continue
		default:
			b.WriteByte(c)
		}
	}
	compact := b.String()
	if len(compact) < 8 { // короче — не может быть подпиской
		return nil, false
	}
	if strings.Contains(compact, "=") {
		// паддинг допускается только в конце
		trim := strings.TrimRight(compact, "=")
		if strings.Contains(trim, "=") {
			return nil, false
		}
	}
	padded := compact + strings.Repeat("=", (4-len(compact)%4)%4)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		decoded, err := enc.DecodeString(padded)
		if err == nil {
			if !utf8.Valid(decoded) {
				return nil, false
			}
			return decoded, true
		}
	}
	return nil, false
}

// decodeB64Loose — base64 с любым паддингом (std/url) для инспекции bean.
func decodeB64Loose(s string) []byte {
	s = strings.TrimSpace(s)
	for _, cand := range []string{s, strings.TrimRight(s, "=")} {
		padded := cand + strings.Repeat("=", (4-len(cand)%4)%4)
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
			if b, err := enc.DecodeString(padded); err == nil {
				return b
			}
		}
	}
	return nil
}

// truncateRunes — усечение строки для ошибок/логов (без секрета целиком).
func truncateRunes(s string) string {
	r := []rune(s)
	if len(r) <= maxErrLineRunes {
		return s
	}
	return string(r[:maxErrLineRunes]) + "…"
}

// UnknownFormatError сообщает, что содержимое не опознано (для UI).
func UnknownFormatError(sample string) error {
	return &Error{Msg: errUnrecognized + " (начало: " + truncateRunes(sample) + ")"}
}

// ErrUnrecognized — сентинел для проверок (сам текст см. UnknownFormatError).
var ErrUnrecognized = errors.New(errUnrecognized)
