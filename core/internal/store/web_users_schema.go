// SPDX-License-Identifier: GPL-3.0-only

package store

// webUsersSchemaVersion — версия схемы с таблицей web_users (T-76).
const webUsersSchemaVersion = 5

// webUsersSchema — миграция v5: пользователи веб-панели. Имя уникально без
// учёта регистра; пароль хранится только хэшем (Argon2id, формат — web/password.go).
var webUsersSchema = []string{
	`CREATE TABLE web_users (
	   id            INTEGER PRIMARY KEY,
	   username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	   password_hash TEXT    NOT NULL,
	   created_at_ms INTEGER NOT NULL,
	   updated_at_ms INTEGER NOT NULL
	 )`,
}
