// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// Миграции схемы. Версия фиксируется в таблице schema_version (W1);
// каждая миграция применяется один раз в отдельной транзакции — повторный
// запуск идемпотентен (проверяется тестом).
//
// W1-состав: profiles, groups, settings, meta (core-port-plan §5.2).
// Таблицы tags/profile_tags, routing_presets, sessions добавляются
// позже (W3/W7) миграциями 2+; теги/заметки W1 живут в profiles.data.

type migration struct {
	version int64
	stmts   []string
}

var migrations = []migration{
	{
		version: 1,
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS meta (
			   key   TEXT PRIMARY KEY,
			   value TEXT NOT NULL
			 )`,
			`CREATE TABLE IF NOT EXISTS groups (
			   id                  INTEGER PRIMARY KEY,
			   kind                INTEGER NOT NULL DEFAULT 1, -- 1 basic | 2 subscription
			   name                TEXT    NOT NULL,
			   subscription_url    TEXT,
			   archive             INTEGER NOT NULL DEFAULT 0,
			   skip_auto_update    INTEGER NOT NULL DEFAULT 0,
			   front_proxy_id      INTEGER REFERENCES profiles(id) ON DELETE SET NULL,
			   subscription_info   TEXT,   -- JSON SubscriptionInfo (FR-UI-262)
			   update_interval_min INTEGER, -- per-sub (FR-UI-263); NULL = глобальный
			   user_agent          TEXT,
			   http_headers        TEXT,   -- JSON map (без секретов)
			   http_basic_user     TEXT,
			   last_working_url    TEXT,   -- «последний рабочий источник»
			   last_update_ms      INTEGER,
			   tab_order           INTEGER NOT NULL,
			   ui_state            TEXT,   -- opaque (column_width и пр.)
			   created_at_ms       INTEGER NOT NULL,
			   updated_at_ms       INTEGER NOT NULL
			 )`,
			`CREATE TABLE IF NOT EXISTS profiles (
			   id               INTEGER PRIMARY KEY,
			   group_id         INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			   type             TEXT    NOT NULL, -- 'socks'...'chain'
			   name             TEXT    NOT NULL DEFAULT '',
			   address          TEXT    NOT NULL DEFAULT '',
			   port             INTEGER NOT NULL DEFAULT 0,
			   sort_key         INTEGER NOT NULL, -- канонический порядок (FR-UI-074); шаг 1024
			   favorite         INTEGER NOT NULL DEFAULT 0,
			   notes            TEXT,
			   latency_ms       INTEGER,           -- NULL=не тестировали; <0=unavailable («yc»)
			   full_report      TEXT,              -- JSON FullTestReport
			   tested_at_ms     INTEGER,
			   uplink_total     INTEGER NOT NULL DEFAULT 0,
			   downlink_total   INTEGER NOT NULL DEFAULT 0,
			   data             TEXT    NOT NULL,  -- канонический JSON модели Profile
			   custom_confirmed INTEGER NOT NULL DEFAULT 0, -- FR-UI-257
			   legacy_bean      TEXT,               -- исходный bean NekoBox (на время миграции)
			   created_at_ms    INTEGER NOT NULL,
			   updated_at_ms    INTEGER NOT NULL
			 )`,
			`CREATE INDEX IF NOT EXISTS idx_profiles_group_order ON profiles(group_id, sort_key)`,
			`CREATE INDEX IF NOT EXISTS idx_profiles_dedup ON profiles(type, address, port)`,
			`CREATE INDEX IF NOT EXISTS idx_profiles_favorite ON profiles(favorite) WHERE favorite <> 0`,
			`CREATE TABLE IF NOT EXISTS settings (
			   key           TEXT PRIMARY KEY,
			   value         TEXT NOT NULL, -- JSON значения (тип — из дескриптора)
			   updated_at_ms INTEGER NOT NULL
			 )`,
			// F4 (аудит №7): группа «Default» создаётся сразу — без неё
			// импорт профилей/подписок падает NOT_FOUND (пользователь не
			// может добавить ни одного сервера). ID — из резервного
			// диапазона DefaultGroupID: маленькие id (0,1,2…) занимает
			// импорт каталога NekoBox, и совпадение уничтожало бы группу
			// пользователя при миграции.
			`INSERT OR IGNORE INTO groups (id, kind, name, archive, skip_auto_update, tab_order, created_at_ms, updated_at_ms)
			 VALUES (` + strconv.FormatInt(DefaultGroupID, 10) + `, 1, 'Default', 0, 0, 0, strftime('%s','now')*1000, strftime('%s','now')*1000)`,
		},
	},
	{
		// L19 аудита №1: правка уже применённой миграции v1 не доезжает до
		// существующих БД (schema_version=1 → v1 пропускается). Поэтому
		// гарантия «Default-группа существует» и исправление дефолта
		// активного пресета вынесены в отдельную миграцию v2.
		version: 2,
		stmts: []string{
			`INSERT OR IGNORE INTO groups (id, kind, name, archive, skip_auto_update, tab_order, created_at_ms, updated_at_ms)
			 VALUES (` + strconv.FormatInt(DefaultGroupID, 10) + `, 1, 'Default', 0, 0, 0, strftime('%s','now')*1000, strftime('%s','now')*1000)`,
			// L20: дефолт дескриптора указывал на несуществующий builtin-id
			// "lav-global" — активный пресет не находился, и ядро собирало
			// нейтральный конфиг без block-списков и geoip:private.
			`UPDATE settings SET value = '"global"'
			   WHERE key = 'routing.active_preset' AND value = '"lav-global"'`,
		},
	},
	{
		// T-08B: подтверждение custom-полей привязано к хэшу содержимого
		// (model/customhash.go). Прежний флаг custom_confirmed мог выставить
		// клиент сам, поэтому ему не доверяем: все подтверждения сбрасываются,
		// колонка больше не читается и не пишется.
		version: 3,
		stmts: []string{
			`ALTER TABLE profiles ADD COLUMN custom_hash TEXT`,
			`UPDATE profiles SET custom_confirmed = 0`,
		},
	},
	{
		// T-79: сессии подключения для сводки трафика (sessions_schema.go).
		version: sessionsSchemaVersion,
		stmts:   sessionsSchema,
	},
	{
		// T-76: пользователи веб-панели (web_users_schema.go).
		version: webUsersSchemaVersion,
		stmts:   webUsersSchema,
	},
	{
		// T-78: что уже перенесено из NekoBox (importmap_schema.go).
		version: importMapSchemaVersion,
		stmts:   importMapSchema,
	},
}

// schemaVersion читает текущую версию схемы (0 — свежая база).
func schemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version       INTEGER PRIMARY KEY,
		applied_at_ms INTEGER NOT NULL
	)`); err != nil {
		return 0, fmt.Errorf("create schema_version: %w", err)
	}
	var v int64
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	return v, err
}

// migrate применяет все неприменённые миграции; идемпотентно.
// Контекст с таймаутом страхует от вечного ожидания соединения пула
// (SetMaxOpenConns(1)) при незакрытых ресурсах — ошибка вместо зависания.
func migrate(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	current, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	// T-50: БД «из будущего» не принимается — старая сборка не должна
	// молча писать в схему новой (миграции m.version <= current пропускались).
	if latest := migrations[len(migrations)-1].version; current > latest {
		return &SchemaTooNewError{Found: current, Supported: latest}
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		err := func() error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			for _, stmt := range m.stmts {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("migration %d: %w", m.version, err)
				}
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO schema_version (version, applied_at_ms) VALUES (?, ?)`,
				m.version, nowMs()); err != nil {
				_ = tx.Rollback()
				return err
			}
			return tx.Commit()
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion — текущая версия схемы (для диагностики/тестов).
func (s *Store) SchemaVersion() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return schemaVersion(ctx, s.db)
}
