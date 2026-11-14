// SPDX-License-Identifier: GPL-3.0-only

// filekeys.go — запрет путей к файлам в пользовательском и импортированном
// JSON (T-08, A1-01): custom_config, custom_outbound_json, internal-full,
// custom-outbound.
//
// Почему нужен отдельный обход. Allowlist корневых секций (merge.go) пускает
// целые поддеревья route/dns/outbounds. В них у sing-box полно ключей, чьё
// значение — путь на диске: experimental.cache_file.path (при «невалидном»
// файле sing-box УДАЛЯЕТ его и создаёт на его месте bbolt),
// route.rule_set[].path, tls.certificate_path, private_key_path,
// executable_path (outbound tor запускает программу) и т. д. Путь из чужого
// JSON не должен попасть в итоговый конфиг ни при каких значениях флага
// custom_fields_confirmed.
//
// Имена ключей сверяются без учёта регистра по правилам Unicode simple
// folding — так же, как декодер sing-box
// (sing/common/json/internal/contextjson: foldName == bytes.EqualFold):
// "Path", "KEY_PATH" и "Key_path" (знак Кельвина) — тот же ключ.
package cfgbuild

import (
	"fmt"
	"sort"
	"strings"
)

// fileKeySuffixes — окончания имён ключей sing-box (1.14, форк 628a9ed),
// значением которых служит путь к файлу или каталогу: certificate_path,
// key_path, private_key_path, config_path, executable_path, wrapper_path,
// data_directory, state_directory, dhcp_lease_files, mesh_psk_file, pid_file…
var fileKeySuffixes = []string{"_path", "_paths", "_file", "_files", "_dir", "_dirs", "_directory"}

// fileKeyExceptions — ключи с таким окончанием, которые файл не открывают:
// process_path — условие правила (сравнение с путём исполняемого файла
// процесса), а не обращение к диску.
var fileKeyExceptions = []string{"process_path"}

// exactFileKeys — ключи без характерного окончания, ведущие к файлам:
// directory (masquerade.directory у hysteria2 — раздача каталога),
// external_ui и external_ui_download_url (clash_api: каталог панели и
// скачивание архива с распаковкой — zip-slip, A1-11).
var exactFileKeys = []string{"directory", "external_ui", "external_ui_download_url"}

// urlPathTypes — значения type, при которых ключ path той же секции — путь
// в URL запроса, а не файл: транспорты ws/http/httpupgrade, исходящий http,
// DNS-серверы https/h3 ("/dns-query").
var urlPathTypes = map[string]bool{
	"http": true, "https": true, "h3": true, "ws": true, "httpupgrade": true,
}

// rejectFileKeys обходит пользовательский JSON (объекты и массивы любой
// вложенности) и возвращает INVALID_ARGUMENT на первом ключе с путём к файлу.
// root — начало пути ключа в сообщении ("" — ключи отсчитываются от корня
// конфига, например "route.rule_set[0].path"; "custom_outbound" — от поля
// профиля). Ключи обходятся в отсортированном порядке: ответ детерминирован.
// Маркеры конкатенации MergeJSON ("+key", "key+") отбрасываются.
func rejectFileKeys(root string, v any) error {
	return walkFileKeys(root, nil, v)
}

func walkFileKeys(trail string, stack []string, v any) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			base := mergeKeyBase(k)
			child := x[k]
			key := base
			if trail != "" {
				key = trail + "." + base
			}
			if isFileKey(stack, base, x, child) {
				return invalidArgument(key,
					"key %q sets a file path; file paths are not allowed in custom config (put the contents inline, e.g. tls.certificate, or remove the key)", key)
			}
			// Срез с ёмкостью len: append не делит память между соседями.
			next := append(stack[:len(stack):len(stack)], base)
			if err := walkFileKeys(key, next, child); err != nil {
				return err
			}
		}
	case []any:
		for i, el := range x {
			if err := walkFileKeys(fmt.Sprintf("%s[%d]", trail, i), stack, el); err != nil {
				return err
			}
		}
	}
	return nil
}

// isFileKey сообщает, что ключ key (без маркеров) объекта obj со значением
// val задаёт путь к файлу. stack — имена объемлющих ключей (без индексов).
// Пустая строка и пустой список путём не считаются: файла нет.
func isFileKey(stack []string, key string, obj map[string]any, val any) bool {
	if !nonEmptyStringish(val) {
		return false
	}
	switch {
	case strings.EqualFold(key, "path"):
		return !isURLPath(stack, obj)
	case strings.EqualFold(key, "output"):
		// log.output; в других секциях ключа output у sing-box нет.
		return len(stack) > 0 && strings.EqualFold(stack[len(stack)-1], "log")
	}
	for _, name := range exactFileKeys {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	for _, name := range fileKeyExceptions {
		if strings.EqualFold(key, name) {
			return false
		}
	}
	for _, suffix := range fileKeySuffixes {
		if hasFoldSuffix(key, suffix) {
			return true
		}
	}
	return false
}

// isURLPath — ключ path в этом объекте задаёт URL-путь: объект лежит в
// transport либо его type из urlPathTypes. Если у объекта несколько ключей,
// равных "type" с точностью до регистра, декодер sing-box возьмёт последний, а
// мы не знаем какой: считаем контекст неопределённым (путь отвергается).
func isURLPath(stack []string, obj map[string]any) bool {
	if n := len(stack); n > 0 && strings.EqualFold(stack[n-1], "transport") {
		return true
	}
	typ, count := "", 0
	for k, v := range obj {
		if strings.EqualFold(mergeKeyBase(k), "type") {
			count++
			typ, _ = v.(string)
		}
	}
	return count == 1 && urlPathTypes[typ]
}

// nonEmptyStringish — непустая строка либо непустой массив.
func nonEmptyStringish(v any) bool {
	switch x := v.(type) {
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	}
	return false
}

// hasFoldSuffix — s оканчивается на suffix (ASCII) без учёта регистра и
// Unicode-эквивалентов (ſ = s, знак Кельвина = k). Сравнение по рунам, не по
// байтам: эквиваленты занимают разное число байт.
func hasFoldSuffix(s, suffix string) bool {
	r := []rune(s)
	n := len([]rune(suffix))
	return len(r) >= n && strings.EqualFold(string(r[len(r)-n:]), suffix)
}
