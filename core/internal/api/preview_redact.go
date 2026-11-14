// SPDX-License-Identifier: GPL-3.0-only

package api

// preview_redact.go — маскирование секретов в дереве конфига перед diff
// (T-08B). Маскирование по тексту знает только «ключ: значение» в одной
// строке и пропускает многострочные PEM, заголовки Cookie/Authorization и
// ключи TLS, поэтому дерево обходится структурно:
//   - значение секретного ключа (logs.IsSecretKey, key, *_key, cookie,
//     authorization) и всё под ним, а также любая строка с PEM-блоком
//     заменяются на «[redacted:xxxxxx]» — короткий отпечаток HMAC с ключом
//     процесса: сменённый секрет виден в diff как изменение, но не
//     раскрывается и между запусками не сопоставляется;
//   - остальные строки проходят logs.Redact (UUID, ссылки с учётными
//     данными, токены в URL).

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// previewSecretKey — ключ HMAC отпечатков; новый при каждом запуске ядра.
var previewSecretKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic(fmt.Sprintf("preview_redact: crypto/rand: %v", err))
	}
	return k
}()

// previewSecretNames — ключи, секретные помимо logs.IsSecretKey.
var previewSecretNames = map[string]bool{
	"key": true, "cookie": true, "set-cookie": true,
	"authorization": true, "proxy-authorization": true,
}

// isPreviewSecretKey — значение ключа name маскируется целиком.
func isPreviewSecretKey(name string) bool {
	n := strings.ToLower(name)
	return previewSecretNames[n] || strings.HasSuffix(n, "_key") || strings.HasSuffix(n, "-key") || logs.IsSecretKey(n)
}

// redactTree — копия дерева encoding/json с замаскированными секретами;
// secret — значение лежит под секретным ключом.
func redactTree(v any, secret bool) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = redactTree(val, secret || isPreviewSecretKey(k))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = redactTree(e, secret)
		}
		return out
	case string:
		if secret || strings.Contains(x, "-----BEGIN") {
			return fingerprint(x)
		}
		return logs.Redact(x)
	case nil:
		return nil
	}
	if secret { // число или bool под секретным ключом
		return fingerprint(fmt.Sprint(v))
	}
	return v
}

// fingerprint — «[redacted:xxxxxx]», 24 бита HMAC-SHA256 значения.
func fingerprint(s string) string {
	m := hmac.New(sha256.New, previewSecretKey)
	m.Write([]byte(s))
	return "[redacted:" + hex.EncodeToString(m.Sum(nil))[:6] + "]"
}
