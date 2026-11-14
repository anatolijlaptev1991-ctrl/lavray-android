// SPDX-License-Identifier: GPL-3.0-only

//go:build !singbox_run

package inst

import "context"

// NewTestInstance без тега singbox_run поднять инстанс не может: рабочее ядро
// собирается с тегом (core/BUILD_TAGS).
func NewTestInstance(context.Context, []byte) (TestDialer, error) {
	return nil, ErrTestInstanceUnsupported
}
