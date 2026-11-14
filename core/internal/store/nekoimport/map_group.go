// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ---- маппинг NekoBox → модель LAVRay ----

// nekoLastupMs: NekoBox хранит lastup в СЕКУНДАХ (GroupUpdater.cpp:564 —
// currentMSecsSinceEpoch()/1000); модель — в мс. Эвристика на стык эпох:
// значение < 1e11 (≈5137 год в мс) считаем секундами.
func nekoLastupMs(sec int64) int64 {
	if sec > 0 && sec < 1_00_000_000_000 {
		return sec * 1000
	}
	return sec
}

// toGroup конвертирует группу; вторым значением — front_proxy_id (-1 = нет).
func toGroup(ng *nekoGroup) (*model.Group, int64) {
	name := ng.Name
	if name == "" { // хранилище отвергает группу без имени
		name = fmt.Sprintf("Group %d", ng.ID)
	}
	g := &model.Group{
		ID:              ng.ID,
		Name:            name,
		Kind:            model.GroupBasic,
		SubscriptionURL: ng.URL,
		Archive:         ng.Archive,
		SkipAutoUpdate:  ng.SkipAutoUpdate,
		LastUpdateMs:    nekoLastupMs(ng.Lastup),
	}
	if ng.URL != "" {
		g.Kind = model.GroupSubscription
	}
	g.SubscriptionInfo = parseSubscriptionUserInfo(ng.Info)
	if ng.ManuallyColumnWidth || len(ng.ColumnWidth) > 0 {
		ui := map[string]any{}
		if ng.ManuallyColumnWidth {
			ui["manually_column_width"] = true
		}
		if len(ng.ColumnWidth) > 0 {
			ui["column_width"] = ng.ColumnWidth
		}
		if b, err := json.Marshal(ui); err == nil {
			g.UIState = string(b)
		}
	}
	return g, ng.FrontProxyID
}

// oldFmtGroup — группа старого формата: profiles/<id>.json с type=group
// (bean несёт поля группы; имя файла — id группы).
func oldFmtGroup(ent *nekoEntity) *nekoGroup {
	b := &nekoBean{}
	if len(ent.Bean) > 0 {
		_ = json.Unmarshal(ent.Bean, b) // лучшие усилия: что распарсилось — то перенесём
	}
	g := &nekoGroup{
		ID:           ent.ID,
		Name:         b.Name,
		URL:          b.URL,
		Info:         b.Info,
		Lastup:       b.Lastup,
		Order:        b.Order,
		FrontProxyID: -1,
	}
	if g.Name == "" {
		g.Name = fmt.Sprintf("Group %d", ent.ID)
	}
	if b.Archive != nil {
		g.Archive = *b.Archive
	}
	if b.SkipAutoUpdate != nil {
		g.SkipAutoUpdate = *b.SkipAutoUpdate
	}
	if b.FrontProxyID != nil {
		g.FrontProxyID = *b.FrontProxyID
	}
	return g
}

// parseSubscriptionUserInfo разбирает заголовок Subscription-UserInfo
// («upload=…; download=…; total=…; expire=<unix-секунды>») — так NekoBox
// хранит его в Group.info (sub/GroupUpdater.cpp:565).
func parseSubscriptionUserInfo(info string) *model.SubscriptionInfo {
	if strings.TrimSpace(info) == "" {
		return nil
	}
	si := &model.SubscriptionInfo{}
	found := false
	for _, part := range strings.Split(info, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "upload":
			upload := n
			si.UploadBytes = &upload
			found = true
		case "download":
			download := n
			si.DownloadBytes = &download
			found = true
		case "total":
			total := n
			si.TotalBytes = &total
			found = true
		case "expire":
			expire := n * 1000 // unix-секунды → мс
			si.ExpireMs = &expire
			found = true
		}
	}
	if !found {
		return nil
	}
	return si
}
