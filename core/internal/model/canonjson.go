// SPDX-License-Identifier: GPL-3.0-only

package model

// canonjson.go — канонический вид JSON для хэша custom-полей (T-08B). Два
// документа с одним смыслом дают одну строку: ключи объектов отсортированы,
// пробелов нет, \u-экранирование нормализовано, числа сохраняются текстом
// (json.Number, без округления до float64), HTML не экранируется.
// Дубли ключей отвергаются: наш разбор и разбор sing-box могли бы понять такой
// документ по-разному, и подтверждённое содержимое разошлось бы с применённым.
//
// Документ сначала разбирается в дерево, затем выводится одним проходом в
// общий буфер — время линейно по размеру, а не «размер × вложенность».

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	// maxCanonDepth — предел вложенности (у encoding/json свой, 10000).
	maxCanonDepth = 512
	// MaxCanonBytes — предел документа: больше custom-JSON не бывает (API
	// принимает до 256 КиБ), а канонизация идёт при каждом чтении профиля.
	MaxCanonBytes = 1 << 20
)

var (
	// ErrDuplicateKey — в объекте JSON один ключ встречается дважды.
	ErrDuplicateKey = errors.New("повторяющийся ключ в объекте JSON")
	// ErrCanonTooLarge — документ больше MaxCanonBytes.
	ErrCanonTooLarge = fmt.Errorf("JSON больше %d КиБ", MaxCanonBytes>>10)
)

// CanonicalJSON возвращает канонический вид документа raw. Пустой (или из
// одних пробелов) документ — пустая строка без ошибки.
func CanonicalJSON(raw string) (string, error) {
	if len(raw) > MaxCanonBytes {
		return "", ErrCanonTooLarge
	}
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	tree, err := parseValue(dec, 0)
	if err != nil {
		return "", err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", errors.New("после документа JSON есть лишние данные")
	}
	var out bytes.Buffer
	out.Grow(len(raw))
	writeCanon(&out, tree)
	return out.String(), nil
}

// parseValue читает одно значение: map[string]any, []any, string,
// json.Number, bool или nil.
func parseValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxCanonDepth {
		return nil, fmt.Errorf("вложенность JSON больше %d", maxCanonDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			return parseObject(dec, depth)
		case '[':
			return parseArray(dec, depth)
		}
		return nil, fmt.Errorf("неожиданный разделитель %q", v)
	case string, json.Number, bool, nil:
		return v, nil
	}
	return nil, fmt.Errorf("неожиданный токен %T", tok)
}

func parseObject(dec *json.Decoder, depth int) (any, error) {
	obj := map[string]any{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("ключ объекта не строка: %v", tok)
		}
		if _, dup := obj[key]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateKey, key)
		}
		val, err := parseValue(dec, depth+1)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	_, err := dec.Token() // '}'
	return obj, err
}

func parseArray(dec *json.Decoder, depth int) (any, error) {
	arr := []any{}
	for dec.More() {
		val, err := parseValue(dec, depth+1)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	_, err := dec.Token() // ']'
	return arr, err
}

// writeCanon выводит дерево parseValue: ключи — по порядку байтов UTF-8.
func writeCanon(out *bytes.Buffer, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeString(out, k)
			out.WriteByte(':')
			writeCanon(out, x[k])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCanon(out, e)
		}
		out.WriteByte(']')
	case string:
		writeString(out, x)
	case json.Number:
		out.WriteString(x.String())
	case bool:
		if x {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	default: // nil
		out.WriteString("null")
	}
}

// writeString — строка JSON без HTML-экранирования (<, >, & как есть).
func writeString(out *bytes.Buffer, s string) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // строка Go кодируется всегда
	out.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}
