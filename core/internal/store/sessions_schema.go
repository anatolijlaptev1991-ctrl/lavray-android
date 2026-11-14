// SPDX-License-Identifier: GPL-3.0-only

package store

// sessionsSchema — миграция v4 (T-79): таблица сессий подключения. Сессия
// живёт от запуска инстанса до его остановки; байты копятся в ней и в счётчиках
// профиля. profile_id обнуляется при удалении профиля (ON DELETE SET NULL):
// сводка по группе не теряет уже накопленное. group_id обнуляется при удалении
// группы: groups.id без AUTOINCREMENT, и новая группа с тем же id иначе
// унаследовала бы чужую историю трафика.
var sessionsSchema = []string{
	`CREATE TABLE sessions (
	   id             INTEGER PRIMARY KEY,
	   profile_id     INTEGER REFERENCES profiles(id) ON DELETE SET NULL,
	   group_id       INTEGER REFERENCES groups(id) ON DELETE SET NULL,
	   mode           TEXT,
	   started_at_ms  INTEGER NOT NULL,
	   ended_at_ms    INTEGER,
	   uplink_bytes   INTEGER NOT NULL DEFAULT 0,
	   downlink_bytes INTEGER NOT NULL DEFAULT 0,
	   updated_at_ms  INTEGER NOT NULL
	 )`,
	`CREATE INDEX idx_sessions_group ON sessions(group_id, started_at_ms)`,
}
