// SPDX-License-Identifier: GPL-3.0-only

package mobile

// testfactory.go — фабрика временных инстансов проверки задержки.
//
// api.Server берёт фабрику из неэкспортируемого поля newTestInst, а временный
// инстанс (inst.NewTestInstance) создаёт контекст из контекста запроса. Платформу
// и системный DNS ему нужно передать так, чтобы параллельные проверки не делили
// реестр служб (см. host.go), поэтому фабрика оборачивает NewTestInstance и
// подставляет каждому вызову свой контекст host.boxContext. Поле выставляется
// через reflect до первого запроса; ядро закреплено тегом субмодуля, а тест
// TestInstallTestFactory ловит переименование поля при обновлении.

import (
	"context"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/api"
	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"
)

const testFactoryField = "newTestInst"

func (h *host) testFactory() inst.TestInstanceFactory {
	return func(ctx context.Context, cfg []byte) (inst.TestDialer, error) {
		return inst.NewTestInstance(h.boxContext(ctx), cfg)
	}
}

// installTestFactory записывает фабрику в api.Server.newTestInst.
func installTestFactory(s *api.Server, f inst.TestInstanceFactory) error {
	field := reflect.ValueOf(s).Elem().FieldByName(testFactoryField)
	if !field.IsValid() || field.Type() != reflect.TypeOf(f) || !field.CanAddr() {
		return fmt.Errorf("mobile: у api.Server нет поля %s нужного типа (ядро обновлено?)", testFactoryField)
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(f))
	return nil
}
