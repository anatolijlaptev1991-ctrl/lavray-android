// SPDX-License-Identifier: GPL-3.0-only

// rootdir.go — RootStore на диске: файл «<версия>.root.json» на каждый принятый
// корень. Каталог доступен пользователю на запись, и это не страшно: цепочка
// перепроверяется от вшитого корня (ResolveTrust). Тем же каталогом пользуется
// подкоманда ядра verify-artifact, которую вызывает оболочка.
package updates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxRootFileBytes — документ корня — килобайты; больше — мусор.
const maxRootFileBytes = 64 << 10

// DirRootStore хранит корни файлами в каталоге Dir.
type DirRootStore struct{ Dir string }

// LoadRoots читает все «*.root.json» разумного размера; нечитаемое пропускает.
func (d DirRootStore) LoadRoots() ([][]byte, error) {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out [][]byte
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".root.json") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || !info.Mode().IsRegular() || info.Size() > maxRootFileBytes {
			continue
		}
		if raw, rerr := os.ReadFile(filepath.Join(d.Dir, e.Name())); rerr == nil {
			out = append(out, raw)
		}
	}
	return out, nil
}

// SaveRoot пишет документ атомарно (временный файл и переименование).
func (d DirRootStore) SaveRoot(version int64, raw []byte) error {
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return err
	}
	final := filepath.Join(d.Dir, RootName(version))
	tmp, err := os.CreateTemp(d.Dir, "root-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(raw)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if err := firstOf(werr, serr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("корень %d: %w", version, err)
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstOf(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
