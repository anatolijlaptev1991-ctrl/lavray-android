// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// scan_settings.go — чтение groups/nekobox.json (DataStore NekoBox).

const dataStoreFile = "nekobox.json"

// scanSettings читает DataStore; нет файла — нет настроек. Битый файл — в отчёт.
func (sc *scanner) scanSettings() error {
	rel := filepath.Join("groups", dataStoreFile)
	b, err := sc.rd.readFile("groups", dataStoreFile)
	if errors.Is(err, ErrLimits) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		sc.plan.Report.skip(rel, 0, "settings", err.Error())
		return nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		sc.plan.Report.skip(rel, 0, "settings", "invalid JSON: "+err.Error())
		return nil
	}
	settings, active, warns := MapSettings(top)
	sc.plan.Settings, sc.plan.ActiveRoute = settings, active
	sc.plan.Report.Warnings = append(sc.plan.Report.Warnings, warns...)
	return nil
}
