// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

// transport_support.go — транспорты, которые профиль может хранить, а ядро
// собрать не умеет. Сейчас это XHTTP (Xray, бывший splithttp): ссылки
// импортируются, но в форке sing-box транспорта нет. Перенос клиентской части
// из Xray — исследование R-2.

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"

// ReasonTransportUnsupported — причина (ErrorInfo.reason) отказа Connect для
// профиля с транспортом, которого нет в ядре.
const ReasonTransportUnsupported = "transport_unsupported"

// unsupportedNetworks — транспорты потока, которые sing-box ядра не поддерживает.
var unsupportedNetworks = map[string]bool{"xhttp": true}

// unsupportedTransport — имя неподдерживаемого транспорта профиля ("xhttp") или
// "" если профиль собирается. Не рекурсивна: звенья цепочки проверяет сборщик.
func unsupportedTransport(p *model.Profile) string {
	if p == nil || p.Stream == nil || !unsupportedNetworks[p.Stream.Network] {
		return ""
	}
	return p.Stream.Network
}

// UnsupportedReason — значение Profile.unsupported_reason: "<транспорт>_unsupported"
// ("xhttp_unsupported") или "" если ядро профиль соберёт. Вычисляется, не хранится.
func UnsupportedReason(p *model.Profile) string {
	if name := unsupportedTransport(p); name != "" {
		return name + "_unsupported"
	}
	return ""
}

// checkTransportSupported — отказ сборки: FAILED_PRECONDITION, reason
// transport_unsupported. Проверяется для каждого звена, цепочка с XHTTP внутри
// не собирается молча без него.
func checkTransportSupported(p *model.Profile) error {
	name := unsupportedTransport(p)
	if name == "" {
		return nil
	}
	e := failedPrecondition("stream.network",
		"profile %d: транспорт %s не поддерживается ядром; выберите другой сервер", p.ID, name)
	e.Reason = ReasonTransportUnsupported
	return e
}
