// SPDX-License-Identifier: GPL-3.0-only

package api

// Файл копируется scripts/build-core.sh в internal/api/ копии ядра (субмодуль
// не меняется). Он даёт пакету mobile экспортированный сеттер вместо записи в
// неэкспортируемое поле newTestInst через reflect и unsafe.

import "github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"

// SetTestInstanceFactory задаёт фабрику временных инстансов проверки задержки.
// Вызывается до первого запроса; nil возвращает стандартную фабрику.
func SetTestInstanceFactory(s *Server, f inst.TestInstanceFactory) {
	s.newTestInst = f
}
