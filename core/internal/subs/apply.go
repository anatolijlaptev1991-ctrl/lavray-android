// SPDX-License-Identifier: GPL-3.0-only

package subs

// Дифф-флоу обновления подписки (FR-UI-258, LNB-29): разбор — в промежуточном
// виде; Snapshot группы ДО применения; проверки «0 профилей» и «удаление
// > 50 %» → ConfirmRequiredError (подтверждение — confirm_destructive);
// Rollback восстанавливает группу из снапшота. Эквивалентность профилей —
// ключ (type, address, port) (FR-UI-249, индекс дедупа store); совпавший
// профиль ОБНОВЛЯЕТСЯ на месте с сохранением id/статистики/избранного
// (исправление LNB-29 «первый совпавший терял статистику»); порядок группы =
// порядок ответа.

import (
	"errors"
	"fmt"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// MaxDeletionFraction — порог деструктивного обновления (>50 % — подтверждение).
const MaxDeletionFraction = 0.5

// ConfirmRequiredError — обновление не применено, нужно подтверждение
// (PHASE_CONFIRM_REQUIRED плана §4.1; подтверждение — confirm_destructive).
type ConfirmRequiredError struct {
	Reason string
	Diff   *Diff
}

func (e *ConfirmRequiredError) Error() string {
	return "обновление требует подтверждения: " + e.Reason
}

// DiffItem — изменение по одному профилю.
type DiffItem struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Detail string `json:"detail,omitempty"`
}

// Diff — предпросмотр изменений (UI рисует до подтверждения).
type Diff struct {
	Added            []DiffItem `json:"added"`
	Deleted          []DiffItem `json:"deleted"`
	Updated          int        `json:"updated"` // совпавших (обновляются на месте)
	Kept             int        `json:"kept"`
	DeletionFraction float64    `json:"deletion_fraction"`
	ConfirmRequired  bool       `json:"confirm_required"`
	Reason           string     `json:"reason,omitempty"`
}

// profileKey — ключ эквивалентности профилей (FR-UI-249).
type profileKey struct {
	typ     model.ProfileType
	address string
	port    int
	// L15 аудита №1: без креденшелов два разных аккаунта на одном хосте и
	// порту (разные uuid/пароли) считались одним профилем — второй терялся
	// при применении подписки, а обновление давало ложный CONFIRM_REQUIRED.
	credentials string
}

func keyOf(p *model.Profile) profileKey {
	return profileKey{p.Type, p.Address, p.Port, credentialKey(p)}
}

// endpointKey — «мягкий» ключ: тот же сервер, креденшелы могут смениться
// (провайдер ротирует пароль/uuid). Используется вторым проходом, чтобы
// обновление на месте сохраняло id/избранное/статистику (FR-UI-249), и
// только если точного совпадения не нашлось.
func endpointKey(p *model.Profile) profileKey {
	return profileKey{typ: p.Type, address: p.Address, port: p.Port}
}

// credentialKey — стабильная часть секрета профиля, участвующая в
// идентичности (uuid/пароль/метод), без раскрытия значения в логах.
func credentialKey(p *model.Profile) string {
	switch d := p.Detail.(type) {
	case *model.VMessDetail:
		return "vmess:" + d.UUID + ":" + d.Security
	case *model.TrojanVlessDetail:
		return "tv:" + d.Password + ":" + d.Flow
	case *model.ShadowsocksDetail:
		return "ss:" + d.Method + ":" + d.Password
	case *model.SocksHTTPDetail:
		return "sh:" + d.Proto + ":" + d.Username + ":" + d.Password
	case *model.Hysteria2Detail:
		return "hy2:" + d.Password
	case *model.TUICDetail:
		return "tuic:" + d.UUID + ":" + d.Password
	}
	return ""
}

// dedupByKey оставляет первое вхождение ключа (type+address+port).
// Один и тот же дедуп — в ComputeDiff и Apply (W3-3, ревью): превью и факт
// применения совпадают: дубль в ответе не создаёт второй профиль, дубль в
// группе (ключ исчез из ответа) удаляется.
func dedupByKey(list []*model.Profile) []*model.Profile {
	seen := make(map[profileKey]bool, len(list))
	out := make([]*model.Profile, 0, len(list))
	for _, p := range list {
		k := keyOf(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}

func item(p *model.Profile) DiffItem {
	return DiffItem{Name: p.Name, Type: string(p.Type), Detail: fmt.Sprintf("%s:%d", p.Address, p.Port)}
}

// ComputeDiff сравнивает текущие профили группы с новым списком.
func ComputeDiff(existing, next []*model.Profile) *Diff {
	existing = managedOnly(existing) // T-51: ручные серверы подписка не трогает
	next = dedupByKey(next)          // W3-3: тот же дедуп, что в Apply
	d := &Diff{}
	nextKeys := make(map[profileKey]bool, len(next))
	for _, p := range next {
		nextKeys[keyOf(p)] = true
	}
	matched := make(map[profileKey]bool)
	// Мягкие совпадения: сервер тот же, креденшелы сменились.
	// Значение «свободно» (false) означает, что endpoint уже израсходован.
	softMatched := make(map[profileKey]bool)
	var softOld []*model.Profile
	var dupes []*model.Profile
	for _, e := range existing {
		k := keyOf(e)
		switch {
		case nextKeys[k] && !matched[k]:
			matched[k] = true
			softMatched[endpointKey(e)] = true
			d.Updated++ // обновится на месте, id/статистика сохранятся
		case nextKeys[k]:
			// Второй профиль с тем же ключом — настоящий дубль (W3-3).
			dupes = append(dupes, e)
		default:
			softOld = append(softOld, e)
		}
	}
	for _, e := range dupes {
		d.Deleted = append(d.Deleted, item(e))
	}
	for _, p := range next {
		k := keyOf(p)
		if matched[k] {
			continue
		}
		ek := endpointKey(p)
		if softMatched[ek] {
			// S1 (аудит round 2): «мягкое» совпадение засчитывается 1:1.
			// Раньше второй новый профиль с тем же host:port молча
			// пропускался — не попадал ни в Added, ни в группу.
			softMatched[ek] = false
			continue
		}
		// Точного совпадения нет — ищем «тот же сервер с новым паролем».
		soft := false
		for i, e := range softOld {
			if endpointKey(e) == ek {
				soft = true
				softMatched[ek] = true
				d.Updated++
				softOld = append(softOld[:i], softOld[i+1:]...)
				break
			}
		}
		if !soft {
			d.Added = append(d.Added, item(p))
		}
	}
	for _, e := range softOld {
		if !softMatched[endpointKey(e)] {
			d.Deleted = append(d.Deleted, item(e))
		}
	}
	// S2 (аудит round 2): Updated и Kept — разные счётчики. Раньше Kept
	// дублировал Updated, и API считал TotalCount дважды.
	d.Kept = 0
	if len(existing) > 0 {
		d.DeletionFraction = float64(len(d.Deleted)) / float64(len(existing))
	}
	if len(next) == 0 && len(existing) > 0 {
		d.ConfirmRequired = true
		d.Reason = "новый список профилей пуст"
	} else if d.DeletionFraction > MaxDeletionFraction && len(existing) > 0 {
		d.ConfirmRequired = true
		d.Reason = fmt.Sprintf("обновление удаляет %d из %d профилей (%.0f%%)", len(d.Deleted), len(existing), d.DeletionFraction*100)
	}
	return d
}

// StoreAPI — поверхность хранилища, нужная дифф-флоу (реализуется *store.Store;
// узкий интерфейс позволяет тестам инъектировать сбои посередине применения).
type StoreAPI interface {
	GetGroup(id int64) (*model.Group, error)
	UpdateGroup(g *model.Group) error
	ListProfiles(groupID int64) ([]*model.Profile, error)
	CreateProfile(p *model.Profile) error
	ImportProfile(p *model.Profile) error
	UpdateProfile(p *model.Profile) error
	DeleteProfile(id int64) error
	ReorderProfiles(groupID int64, orderedIDs []int64) error
}

// Snapshot — снимок группы (профили + сама группа) до применения обновления.
type Snapshot struct {
	GroupID  int64
	Group    *model.Group
	Profiles []*model.Profile
	Order    []int64
}

// TakeSnapshot делает снимок группы из хранилища.
func TakeSnapshot(st StoreAPI, groupID int64) (*Snapshot, error) {
	g, err := st.GetGroup(groupID)
	if err != nil {
		return nil, fmt.Errorf("снапшот: группа: %w", err)
	}
	profiles, err := st.ListProfiles(groupID)
	if err != nil {
		return nil, fmt.Errorf("снапшот: профили: %w", err)
	}
	snap := &Snapshot{GroupID: groupID, Group: g, Profiles: profiles}
	for _, p := range profiles {
		snap.Order = append(snap.Order, p.ID)
	}
	return snap, nil
}

// UpdateResult — что произошло при применении.
type UpdateResult struct {
	Diff    *Diff
	Order   []int64 // фактический порядок группы после применения
	UsedURL string
	Info    *model.SubscriptionInfo
	// SkippedLines — строки ответа, не превратившиеся в сервер (ошибка разбора
	// ссылки); 0.2.1: раньше такие строки пропадали молча.
	SkippedLines int
	// Snapshot — состояние группы ДО применения (для честного отката через
	// RollbackSubscription; A15 аудита round 1: API снимал снапшот после
	// apply, поэтому «откат» возвращал ровно то же состояние).
	Snapshot *Snapshot
}

// ApplyParams — вход применения обновления.
type ApplyParams struct {
	Next      []*model.Profile // разобранные профили (промежуточная копия уже разобрана)
	Confirmed bool             // confirm_destructive (FR-UI-258)
	Clear     bool             // clear_before (FR-UI-124): старые профили не сопоставляются, а удаляются
	UsedURL   string           // успешный URL (→ last_working_url)
	Info      *model.SubscriptionInfo
	NowMs     int64 // для last_update (0 → время вызова)
}

// Apply применяет новый список профилей к группе поверх снапшота.
// При ошибке посередине автоматически откатывает группу (Rollback) и
// возвращает исходную ошибку (FR-UI-258).
func Apply(st StoreAPI, snap *Snapshot, params ApplyParams) (*UpdateResult, error) {
	next := dedupByKey(params.Next) // W3-3: тот же дедуп, что в ComputeDiff
	diff := ComputeDiff(snap.Profiles, next)
	if diff.ConfirmRequired && !params.Confirmed {
		return nil, &ConfirmRequiredError{Reason: diff.Reason, Diff: diff}
	}
	// T-51: все записи — одной транзакцией хранилища; без транзакций
	// (тестовые обёртки) — откат из снимка (apply_tx.go).
	return applyAtomically(st, snap, params, next, diff)
}

// applyWrites — записи Apply (вызывается из applyAtomically). Откат при
// ошибке — забота вызывающего: ROLLBACK транзакции либо Rollback из снимка.
func applyWrites(st StoreAPI, snap *Snapshot, params ApplyParams, next []*model.Profile, diff *Diff) (*UpdateResult, error) {
	// Промежуточное применение: совпавшие — обновление на месте (id,
	// статистика, избранное, тесты — сохраняются), новые — вставка,
	// отсутствующие — удаление; порядок группы = порядок ответа.
	// Дубль ключа в группе (W3-3): в map остаётся первый, остальные —
	// удаляются вместе с непокрытыми (ComputeDiff считает их Deleted).
	existing := make(map[profileKey]*model.Profile, len(snap.Profiles))
	// soft — тот же сервер с другими креденшелами (ротация пароля/uuid):
	// второй проход сопоставления, чтобы профиль обновлялся на месте.
	soft := make(map[profileKey][]*model.Profile, len(snap.Profiles))
	var existingDupes []*model.Profile
	for _, e := range managedOnly(snap.Profiles) { // T-51: ручные серверы не трогаем
		k := keyOf(e)
		if _, dup := existing[k]; dup {
			existingDupes = append(existingDupes, e)
			continue
		}
		existing[k] = e
		ek := endpointKey(e)
		soft[ek] = append(soft[ek], e)
	}
	// takeOld — точное совпадение, иначе мягкое (сервер тот же).
	takeOld := func(p *model.Profile) (*model.Profile, bool) {
		if params.Clear {
			// T-21, clear_before (FR-UI-124): сопоставления нет — каждый профиль
			// ответа создаётся заново, все прежние (existing) удаляются ниже.
			// Порог 50 % (ComputeDiff) и снапшот для отката работают как раньше.
			return nil, false
		}
		k := keyOf(p)
		if old, ok := existing[k]; ok {
			delete(existing, k)
			ek := endpointKey(old)
			for i, cand := range soft[ek] {
				if cand == old {
					soft[ek] = append(soft[ek][:i], soft[ek][i+1:]...)
					break
				}
			}
			return old, true
		}
		ek := endpointKey(p)
		for i, cand := range soft[ek] {
			if keyOf(cand) == k {
				continue
			}
			soft[ek] = append(soft[ek][:i], soft[ek][i+1:]...)
			delete(existing, keyOf(cand))
			return cand, true
		}
		return nil, false
	}
	var order []int64
	err := func() error {
		for _, p := range next {
			// A17 (аудит round 1): подписки — главный источник «ключей по
			// странам»; без DetectGeo здесь группировка по странам работала
			// только для ручного импорта.
			if p.Geo == "" {
				p.Geo = model.DetectGeo(p.Name, p.Address)
			}
			if old, ok := takeOld(p); ok {
				merged := *p
				merged.ID = old.ID
				merged.GroupID = old.GroupID
				merged.Favorite = old.Favorite
				merged.TestResult = old.TestResult
				merged.Traffic = old.Traffic
				// 0.2.1: описание сервера от провайдера (?serverDescription=) кладётся
				// в заметки только пока они пусты — пользовательские не перетираются.
				if old.Notes != "" {
					merged.Notes = old.Notes
				}
				merged.Tags = old.Tags
				// L26 аудита №1: geo — пользовательская метаданная; сохраняем
				// его, если новый разбор страну не определил.
				if merged.Geo == "" {
					merged.Geo = old.Geo
				}
				// T-09 (A1-02): custom-поля пользователь задаёт профилю сам, а
				// подписка их не доставляет — парсер вырезает (stripNekoCustomCore).
				// Берём со старого профиля; подтверждение хранилище оставит само,
				// раз содержимое прежнее (T-08B). Всё, что прислал провайдер, не
				// подтверждено и пользовательское не подменяет (LNB-25).
				merged.CustomOutboundJSON = old.CustomOutboundJSON
				merged.CustomConfigJSON = old.CustomConfigJSON
				if err := st.UpdateProfile(&merged); err != nil {
					return fmt.Errorf("применение: обновление профиля %d: %w", old.ID, err)
				}
				order = append(order, old.ID)
				continue
			}
			p.GroupID = snap.GroupID
			if err := st.CreateProfile(p); err != nil {
				return fmt.Errorf("применение: вставка профиля: %w", err)
			}
			order = append(order, p.ID)
		}
		for _, rest := range existing {
			if err := st.DeleteProfile(rest.ID); err != nil {
				return fmt.Errorf("применение: удаление профиля %d: %w", rest.ID, err)
			}
		}
		for _, dup := range existingDupes { // W3-3
			if err := st.DeleteProfile(dup.ID); err != nil {
				return fmt.Errorf("применение: удаление дубля %d: %w", dup.ID, err)
			}
		}
		if err := st.ReorderProfiles(snap.GroupID, order); err != nil {
			return fmt.Errorf("применение: порядок: %w", err)
		}
		return nil
	}()
	if err != nil {
		return nil, err // откат (FR-UI-258) — у вызывающего, см. applyAtomically
	}

	// last_working_url + last_update + info группы (FR-UI-263/127).
	// Копия: снапшот не мутируем — Rollback восстанавливает исходные поля.
	g := *snap.Group
	if params.UsedURL != "" {
		g.LastWorkingURL = params.UsedURL
	}
	now := params.NowMs
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	g.LastUpdateMs = now
	if params.Info != nil {
		if g.SubscriptionInfo == nil {
			g.SubscriptionInfo = params.Info
		} else {
			// W3-4 (ревью): копия — MergeInfo не мутирует снапшот; при сбое
			// UpdateGroup Rollback восстановит исходный info.
			info := *g.SubscriptionInfo
			MergeInfo(&info, params.Info)
			g.SubscriptionInfo = &info
		}
	}
	if err := st.UpdateGroup(&g); err != nil {
		return nil, fmt.Errorf("применение: группа: %w", err)
	}
	return &UpdateResult{Diff: diff, Order: order, UsedURL: params.UsedURL, Info: params.Info, Snapshot: snap}, nil
}

// Rollback восстанавливает группу из снапшота («Откатить подписку», FR-UI-258):
// текущие профили группы удаляются, профили снапшота восстанавливаются
// с исходными id и порядком; поля группы — из снапшота.
func Rollback(st StoreAPI, snap *Snapshot) error {
	current, err := st.ListProfiles(snap.GroupID)
	if err != nil {
		return fmt.Errorf("откат: чтение текущих: %w", err)
	}
	for _, p := range current {
		if err := st.DeleteProfile(p.ID); err != nil {
			return fmt.Errorf("откат: удаление %d: %w", p.ID, err)
		}
	}
	for _, p := range snap.Profiles {
		restore := *p
		// L16 аудита №1: id из снапшота мог быть переиспользован (старый
		// nextID = MAX+1) — тогда ImportProfile падал на UNIQUE, и группа
		// оставалась ПУСТОЙ. Пробуем исходный id, при конфликте — новый.
		if err := st.ImportProfile(&restore); err != nil {
			restore.ID = 0
			restore.GroupID = snap.GroupID
			if err2 := st.CreateProfile(&restore); err2 != nil {
				return fmt.Errorf("откат: восстановление %d: %v (повтор без id: %v)", p.ID, err, err2)
			}
			restoreID := restore.ID
			for i := range snap.Order {
				if snap.Order[i] == p.ID {
					snap.Order[i] = restoreID
					break
				}
			}
		}
	}
	if err := st.ReorderProfiles(snap.GroupID, snap.Order); err != nil {
		return fmt.Errorf("откат: порядок: %w", err)
	}
	if err := st.UpdateGroup(snap.Group); err != nil {
		return fmt.Errorf("откат: группа: %w", err)
	}
	return nil
}

// ErrEmptyGroupUpdate — применяемый список пуст (LNB-29: не применять).
var ErrEmptyGroupUpdate = errors.New("подписка вернула 0 профилей — обновление не применено")
