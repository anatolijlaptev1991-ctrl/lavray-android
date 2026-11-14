// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"path/filepath"
	"strings"
)

// detect_scan.go — вспомогательное для Detect: каталоги профиля пользователя и
// подсчёт записей config по именам файлов (файлы не читаются).

type profileSource struct{ dir, source string }

// profileSources — каталоги config в профиле пользователя (режим -appdata).
func profileSources(env Env) []profileSource {
	var out []profileSource
	for _, c := range []struct{ root, source string }{{env.AppData, SourceAppData}, {env.LocalAppData, SourceLocalAppData}} {
		if c.root != "" {
			out = append(out, profileSource{filepath.Join(c.root, "nekoray", "config"), c.source})
		}
	}
	return out
}

// profileConfigDirs — ключи каталогов профиля: как бы их ни назвали, это
// установки в режиме -appdata.
func profileConfigDirs(env Env) map[string]bool {
	out := map[string]bool{}
	for _, c := range profileSources(env) {
		out[pathKey(c.dir)] = true
	}
	return out
}

// countEntries считает файлы по именам, не читая их: групп, профилей, маршрутов.
func countEntries(dir string) (groups, profiles, routes int) {
	rd := newReader(dir, DefaultLimits)
	count := func(sub string) []string {
		names, err := rd.names(sub)
		if err != nil {
			return nil
		}
		return names
	}
	groups = len(intJSONFiles(count("groups")))
	profiles = len(intJSONFiles(count("profiles")))
	seen := map[string]bool{}
	for _, sub := range routeDirs {
		for _, n := range count(sub) {
			seen[strings.TrimSuffix(n, ".json")] = true
		}
	}
	return groups, profiles, len(seen)
}
