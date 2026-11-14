// SPDX-License-Identifier: GPL-3.0-only

// strictjson.go — строгий разбор JSON метаданных и проверки полей.
package updates

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// decodeStrict — декодирует ровно один JSON-документ без неизвестных полей
// и лишнего контента после него.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: лишний контент после JSON-документа", ErrBadJSON)
	}
	return nil
}

// requireKeys — все перечисленные ключи обязаны присутствовать в объекте
// (encoding/json не различает «0/false» и «отсутствует»).
func requireKeys(signed json.RawMessage, keys ...string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(signed, &m); err != nil {
		return fmt.Errorf("%w: подписанная часть не является объектом: %v", ErrBadJSON, err)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("%w: %q", ErrMissingField, k)
		}
	}
	return nil
}

// requireSectionKeys — presence подсекции с обязательными ключами.
func requireSectionKeys(signed json.RawMessage, section string, keys ...string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(signed, &m); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	sub, ok := m[section]
	if !ok {
		return nil // секция опциональна; обязательность её полей — когда есть
	}
	return requireKeys(sub, keys...)
}

// checkHex64 — строка является 64-символьным hex sha256.
func checkHex64(s string) error {
	if len(s) != 64 {
		return fmt.Errorf("%w: не 64 hex-символа (%d)", ErrBadValue, len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("%w: не hex: %v", ErrBadValue, err)
	}
	return nil
}

// checkRelativePath — относительный путь без выхода за корень (zip-slip
// профилактика на уровне манифеста; повторяется при staging).
func checkRelativePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: пустой путь", ErrUnsafePath)
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("%w: обратный слэш в пути %q", ErrUnsafePath, p)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: абсолютный путь %q", ErrUnsafePath, p)
	}
	// W-10 (аудит round 2): Windows-специфика. «C:evil» — drive-relative путь,
	// «a.exe:stream» — альтернативный поток данных NTFS; «CON»/«NUL» —
	// зарезервированные имена устройств; хвостовые пробелы/точки Windows
	// молча отбрасывает, что позволяет обойти проверку расширения.
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("%w: NUL-байт в пути", ErrUnsafePath)
	}
	if strings.Contains(p, ":") {
		return fmt.Errorf("%w: двоеточие в пути %q (ADS/диск)", ErrUnsafePath, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: сегмент %q в пути %q", ErrUnsafePath, seg, p)
		}
		trimmed := strings.TrimRight(seg, " .")
		if trimmed != seg {
			return fmt.Errorf("%w: сегмент %q оканчивается пробелом/точкой", ErrUnsafePath, seg)
		}
		base := strings.ToUpper(strings.TrimSuffix(trimmed, path.Ext(trimmed)))
		switch base {
		case "CON", "PRN", "AUX", "NUL",
			"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return fmt.Errorf("%w: зарезервированное имя %q", ErrUnsafePath, seg)
		}
	}
	return nil
}
