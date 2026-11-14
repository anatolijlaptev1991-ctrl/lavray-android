// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// detect.go — поиск установок NekoBox. Только чтение: каталоги не меняются,
// NekoBox не запускается и не останавливается, процессы лишь перечисляются.
// Где искать:
//   - пути, которые передал пользователь (ручной выбор — главный способ: NekoBox
//     обычно переносимый, его config лежит рядом с exe, а не в профиле);
//   - config рядом с запущенным nekobox.exe/nekoray.exe;
//   - %APPDATA%\nekoray\config и %LOCALAPPDATA%\nekoray\config (режим -appdata).

// Источники кандидата (Candidate.Source).
const (
	SourceManual       = "manual"
	SourceProcess      = "process"
	SourceAppData      = "appdata"
	SourceLocalAppData = "localappdata"
)

// Candidate — найденный каталог config.
type Candidate struct {
	Path         string
	Source       string
	Groups       int
	Profiles     int
	Routes       int
	LooksRunning bool // NekoBox запущен: перед переносом его лучше закрыть
}

// Process — запущенный процесс NekoBox/nekoray.
type Process struct {
	Name   string
	ExeDir string // каталог exe; пусто — путь недоступен (процесс запущен с повышением)
}

// Env — окружение поиска. Тесты подставляют свои значения.
type Env struct {
	AppData      string
	LocalAppData string
	// Processes перечисляет процессы NekoBox/nekoray; ошибка поиск не прерывает.
	Processes func() ([]Process, error)
}

// DefaultEnv — окружение текущей системы.
func DefaultEnv() Env {
	return Env{
		AppData:      os.Getenv("APPDATA"),
		LocalAppData: os.Getenv("LOCALAPPDATA"),
		Processes:    listProcesses,
	}
}

// IsNekoProcess — имя exe принадлежит NekoBox или nekoray.
func IsNekoProcess(name string) bool {
	switch strings.ToLower(name) {
	case "nekobox.exe", "nekoray.exe":
		return true
	}
	return false
}

// ResolveConfigDir приводит выбранный путь к каталогу config: сам путь, если в
// нём есть groups или profiles, иначе вложенный config (выбрали каталог
// установки). Ссылки не разыменовываются.
func ResolveConfigDir(path string) (string, bool) {
	if hasData(path) {
		return filepath.Clean(path), true
	}
	if nested := filepath.Join(path, "config"); hasData(nested) {
		return filepath.Clean(nested), true
	}
	return "", false
}

func hasData(dir string) bool {
	for _, sub := range []string{"groups", "profiles"} {
		if fi, err := os.Lstat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// pathKey — ключ сравнения путей (Windows не различает регистр).
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// Detect возвращает найденные установки без повторов; при совпадении каталога
// побеждает источник, названный раньше: ручной, процесс, appdata, localappdata.
func Detect(env Env, extra []string) []Candidate {
	procs := nekoProcesses(env)
	profiles := profileConfigDirs(env)
	var out []Candidate
	seen := map[string]bool{}
	add := func(path, source string) {
		dir, ok := ResolveConfigDir(path)
		if !ok || !filepath.IsAbs(dir) || seen[pathKey(dir)] {
			return
		}
		seen[pathKey(dir)] = true
		g, p, r := countEntries(dir)
		out = append(out, Candidate{Path: dir, Source: source, Groups: g, Profiles: p, Routes: r,
			LooksRunning: runningFor(dir, procs, profiles[pathKey(dir)])})
	}
	for _, p := range extra {
		if filepath.IsAbs(p) {
			add(p, SourceManual)
		}
	}
	for _, pr := range procs {
		if pr.ExeDir != "" {
			add(filepath.Join(pr.ExeDir, "config"), SourceProcess)
		}
	}
	for _, c := range profileSources(env) {
		add(c.dir, c.source)
	}
	return out
}

func nekoProcesses(env Env) []Process {
	if env.Processes == nil {
		return nil
	}
	list, err := env.Processes()
	if err != nil {
		return nil
	}
	var out []Process
	for _, p := range list {
		if IsNekoProcess(p.Name) {
			out = append(out, p)
		}
	}
	return out
}

// runningFor: NekoBox работает с этим config, если есть процесс из каталога, в
// котором лежит config, либо процесс с неизвестным путём (может быть любым —
// предупреждаем на всякий случай). Для каталога профиля (режим -appdata, exe
// лежит в другом месте) путь процесса ничего не говорит, поэтому любой
// запущенный NekoBox даёт предупреждение.
func runningFor(configDir string, procs []Process, inProfile bool) bool {
	if inProfile && len(procs) > 0 {
		return true
	}
	parent := pathKey(filepath.Dir(configDir))
	for _, p := range procs {
		if p.ExeDir == "" || pathKey(p.ExeDir) == parent {
			return true
		}
	}
	return false
}
