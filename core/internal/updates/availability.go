// SPDX-License-Identifier: GPL-3.0-only

// availability.go — решение «есть ли обновление» по версии приложения, а не по
// serial манифеста (serial защищает только от отката метаданных). Платформа
// выбирает актив (platform.go).
package updates

// Причины, по которым обновление недоступно.
const (
	ReasonAvailable     = ""
	ReasonNoApp         = "no_app"              // в манифесте нет секции app
	ReasonUpToDate      = "up_to_date"          // версия манифеста не новее установленной
	ReasonMinUpgrade    = "min_upgrade_from"    // установленная версия ниже minUpgradeFrom
	ReasonNoPlatformAst = "no_platform_asset"   // для этой платформы нет установщика
	ReasonBadAppVersion = "unknown_app_version" // у приложения нет версии
)

// Availability — итог сравнения манифеста с установленной версией.
type Availability struct {
	Available bool
	Reason    string
	Version   string // версия приложения из манифеста
	// MinUpgradeFrom — версия, с которой обновление возможно (причина min_upgrade_from).
	MinUpgradeFrom string
}

// Decide: доступно, если версия манифеста строго новее установленной, установленная
// не ниже minUpgradeFrom и для платформы (assetKey) есть установщик.
func Decide(mf *Manifest, appVersion, assetKey string) Availability {
	if mf == nil || mf.App == nil {
		return Availability{Reason: ReasonNoApp}
	}
	out := Availability{Version: mf.App.Version}
	switch {
	case appVersion == "":
		out.Reason = ReasonBadAppVersion
	case CompareVersions(mf.App.Version, appVersion) <= 0:
		out.Reason = ReasonUpToDate
	case mf.App.MinUpgradeFrom != "" && CompareVersions(appVersion, mf.App.MinUpgradeFrom) < 0:
		out.Reason, out.MinUpgradeFrom = ReasonMinUpgrade, mf.App.MinUpgradeFrom
	default:
		if _, ok := mf.App.Assets[assetKey]; !ok {
			out.Reason = ReasonNoPlatformAst
		} else {
			out.Available = true
		}
	}
	return out
}
