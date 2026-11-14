// SPDX-License-Identifier: GPL-3.0-only

package store

// customhash.go — подтверждение custom-полей в хранилище (T-08B). Инвариант:
// в profiles.custom_hash лежит либо пустое значение, либо хэш, совпадающий с
// текущим содержимым профиля. Хэш пишет только ConfirmCustomFields (и
// ImportProfile — восстановление снимка подписки); CreateProfile его
// стирает, UpdateProfile берёт из БД. Устаревший хэш при записи стирается,
// поэтому правка A→B→A подтверждение не возвращает, а CustomFieldsConfirmed
// всегда вычисляется, а не берётся из модели.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

var (
	// ErrNoCustomContent — у профиля нет custom-полей, подтверждать нечего.
	ErrNoCustomContent = errors.New("store: у профиля нет custom-полей")
	// ErrCustomHashStale — содержимое изменилось: хэш подтверждения устарел.
	ErrCustomHashStale = errors.New("store: custom-поля изменились после предпросмотра")
)

// normalizeCustomHash приводит модель к инварианту перед записью и
// выставляет вычисленный CustomFieldsConfirmed.
func normalizeCustomHash(p *model.Profile) {
	p.CustomFieldsConfirmed = model.CustomConfirmed(p)
	if !p.CustomFieldsConfirmed {
		p.CustomHash = ""
	}
}

// withoutCustomHash — CreateProfile: новый профиль (в том числе клон) не
// подтверждён, какой бы хэш ни пришёл с моделью.
func withoutCustomHash(p *model.Profile) *model.Profile {
	p.CustomHash = ""
	return p
}

// loadGroupAndCustomHash — UpdateProfile: группа и хэш подтверждения из БД в
// той же транзакции. Хэш вызывающего не используется: устаревшая копия
// профиля (прочитанная до чужой правки) не вернёт снятое подтверждение.
func loadGroupAndCustomHash(tx *sql.Tx, p *model.Profile) (int64, error) {
	var group int64
	var hash sql.NullString
	err := tx.QueryRow(`SELECT group_id, custom_hash FROM profiles WHERE id = ?`, p.ID).Scan(&group, &hash)
	p.CustomHash = hash.String
	return group, err
}

// profileData — канонический JSON профиля для profiles.data; перед этим
// модель приводится к инварианту custom_hash.
func profileData(p *model.Profile) ([]byte, error) {
	normalizeCustomHash(p)
	saved := *p
	data, err := json.Marshal(&saved)
	if err != nil {
		return nil, fmt.Errorf("store: marshal profile: %w", err)
	}
	return data, nil
}

// applyScannedCustomHash — то же для прочитанного профиля: колонка
// custom_hash → CustomHash, флаг — по текущему содержимому.
func applyScannedCustomHash(p *model.Profile, hash sql.NullString) {
	p.CustomHash = hash.String
	normalizeCustomHash(p)
}

// ConfirmCustomFields записывает подтверждение ровно для содержимого с
// хэшем hash. Чтение, сверка и запись — одна транзакция: правка, успевшая
// между предпросмотром и подтверждением, даёт ErrCustomHashStale.
func (s *Store) ConfirmCustomFields(id int64, hash string) (*model.Profile, error) {
	var out *model.Profile
	err := s.withTx(func(tx *sql.Tx) error {
		p, err := scanProfile(tx.QueryRow(`SELECT `+profileCols+` FROM profiles WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !model.HasCustomContent(p) {
			return ErrNoCustomContent
		}
		current, err := model.CustomFieldsHash(p)
		if err != nil || current != hash {
			return ErrCustomHashStale
		}
		if _, err := tx.Exec(`UPDATE profiles SET custom_hash = ? WHERE id = ?`, hash, id); err != nil {
			return fmt.Errorf("store: confirm custom fields: %w", err)
		}
		p.CustomHash = hash
		normalizeCustomHash(p)
		out = p
		return nil
	})
	return out, err
}
