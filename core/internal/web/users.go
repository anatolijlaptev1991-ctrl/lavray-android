// SPDX-License-Identifier: GPL-3.0-only

package web

// users.go — пользователи панели: политика имени и пароля, первичная настройка,
// проверка входа с перехешированием. Хранилище — за интерфейсом UserRepo
// (store.WebUser через адаптер в api; в тестах — память).

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Политика пароля и имени.
const (
	MinPasswordRunes = 10
	maxPasswordRunes = 128
	maxUsernameRunes = 64
)

// UserRecord — пользователь в терминах пакета.
type UserRecord struct {
	ID           int64
	Username     string
	PasswordHash string
}

// UserRepo — хранилище пользователей. CreateFirst обязан быть атомарным:
// возвращает ErrAdminExists, если пользователь уже есть; Get — ErrUserNotFound.
type UserRepo interface {
	CreateFirst(username, passwordHash string) (UserRecord, error)
	Get(username string) (UserRecord, error)
	Count() (int, error)
	SetPassword(id int64, passwordHash string) error
}

// Users — операции над пользователями панели.
type Users struct {
	repo UserRepo
	// Warn получает сообщения о сбоях, которые не мешают операции (например,
	// не удалось записать перехешированный пароль). nil — молча.
	Warn func(msg string)

	dummyOnce sync.Once
	dummy     string
}

// NewUsers создаёт сервис поверх хранилища.
func NewUsers(repo UserRepo) *Users { return &Users{repo: repo} }

// ValidateUsername возвращает имя без изменений либо ErrBadUsername. Имя не
// обрезается: пробелы по краям — ошибка, чтобы «admin » и «admin» не путались.
func ValidateUsername(name string) (string, error) {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxUsernameRunes || strings.TrimSpace(name) != name {
		return "", fmt.Errorf("%w: длина 1–%d символов, без пробелов по краям", ErrBadUsername, maxUsernameRunes)
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return "", fmt.Errorf("%w: управляющие символы запрещены", ErrBadUsername)
		}
	}
	return name, nil
}

// ValidatePassword проверяет политику: не короче 10 символов (считаются
// символы, а не байты), не длиннее 128, не из одних пробелов.
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < MinPasswordRunes:
		return fmt.Errorf("%w: не короче %d символов", ErrWeakPassword, MinPasswordRunes)
	case n > maxPasswordRunes:
		return fmt.Errorf("%w: не длиннее %d символов", ErrWeakPassword, maxPasswordRunes)
	case strings.TrimSpace(pw) == "":
		return fmt.Errorf("%w: пароль из одних пробелов", ErrWeakPassword)
	}
	return nil
}

// NeedsSetup — администратора ещё нет.
func (u *Users) NeedsSetup() (bool, error) {
	n, err := u.repo.Count()
	return n == 0, err
}

// CreateFirstAdmin создаёт первого администратора; повтор — ErrAdminExists.
func (u *Users) CreateFirstAdmin(username, password string) (UserRecord, error) {
	name, err := ValidateUsername(username)
	if err != nil {
		return UserRecord{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return UserRecord{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return UserRecord{}, err
	}
	return u.repo.CreateFirst(name, hash)
}

// Authenticate проверяет пару имя/пароль. Неизвестное имя тоже проходит
// полный расчёт хэша, чтобы по времени ответа нельзя было отличить его от
// неверного пароля. Старый хэш (pbkdf2 или слабые параметры) перезаписывается.
func (u *Users) Authenticate(username, password string) (UserRecord, error) {
	n, err := u.repo.Count()
	if err != nil {
		return UserRecord{}, err
	}
	if n == 0 {
		return UserRecord{}, ErrNoAdmin
	}
	rec, err := u.repo.Get(username)
	if errors.Is(err, ErrUserNotFound) {
		VerifyPassword(password, u.dummyHash())
		return UserRecord{}, ErrBadCredentials
	}
	if err != nil {
		return UserRecord{}, err
	}
	ok, rehash := VerifyPassword(password, rec.PasswordHash)
	if !ok {
		return UserRecord{}, ErrBadCredentials
	}
	if rehash {
		u.rehash(&rec, password)
	}
	return rec, nil
}

func (u *Users) rehash(rec *UserRecord, password string) {
	hash, err := HashPassword(password)
	if err == nil {
		err = u.repo.SetPassword(rec.ID, hash)
	}
	if err != nil {
		if u.Warn != nil {
			u.Warn("не удалось перехешировать пароль пользователя: " + err.Error())
		}
		return
	}
	rec.PasswordHash = hash
}

// ChangePassword меняет пароль после проверки прежнего.
func (u *Users) ChangePassword(username, oldPassword, newPassword string) error {
	rec, err := u.repo.Get(username)
	if err != nil {
		return err
	}
	if ok, _ := VerifyPassword(oldPassword, rec.PasswordHash); !ok {
		return ErrBadCredentials
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return u.repo.SetPassword(rec.ID, hash)
}

// dummyHash — хэш-приманка для неизвестных имён (считается один раз).
func (u *Users) dummyHash() string {
	u.dummyOnce.Do(func() { u.dummy, _ = HashPassword("lavray-dummy-password") })
	return u.dummy
}
