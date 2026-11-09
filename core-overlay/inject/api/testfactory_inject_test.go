// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"
)

var errSentinel = errors.New("sentinel")

func TestSetTestInstanceFactory(t *testing.T) {
	s := &Server{}
	f := inst.TestInstanceFactory(func(context.Context, []byte) (inst.TestDialer, error) {
		return nil, errSentinel
	})
	SetTestInstanceFactory(s, f)
	if _, err := s.testInstanceFactory()(context.Background(), nil); !errors.Is(err, errSentinel) {
		t.Fatalf("фабрика не установлена: %v", err)
	}
	SetTestInstanceFactory(s, nil)
	if s.newTestInst != nil {
		t.Fatal("nil должен сбрасывать фабрику")
	}
}
