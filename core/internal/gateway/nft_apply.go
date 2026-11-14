// SPDX-License-Identifier: GPL-3.0-only

package gateway

// nft_apply.go — применение и снятие правил через `nft -f -` (скрипт идёт в
// stdin). Сам запуск процесса спрятан за интерфейсом Runner: тесты подставляют
// фейк и nft не запускают. `nft -f` выполняет файл одной транзакцией, поэтому
// неудавшийся скрипт не оставляет половину правил, а прежний набор сохраняется.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// removeScript снимает таблицу; первая строка создаёт её, если её нет, и
// повторное снятие не считается ошибкой.
const removeScript = "table inet lavray\ndelete table inet lavray\n"

// defaultNftTimeout — предел одного вызова nft.
const defaultNftTimeout = 15 * time.Second

// maxNftOutput — сколько байт вывода nft попадает в сообщение об ошибке.
const maxNftOutput = 512

// Runner выполняет nft-скрипт.
type Runner interface {
	Run(ctx context.Context, script string) error
}

// ExecRunner запускает настоящий `nft -f -`. Создаётся только в сборке для
// роутера (--platform openwrt).
type ExecRunner struct {
	Bin string // путь к nft; пусто — "nft" из PATH
}

// Run исполняет script; при ошибке в сообщение попадает начало вывода nft.
func (r ExecRunner) Run(ctx context.Context, script string) error {
	bin := r.Bin
	if bin == "" {
		bin = "nft"
	}
	cmd := exec.CommandContext(ctx, bin, "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if len(msg) > maxNftOutput {
			msg = msg[:maxNftOutput] + "…"
		}
		return fmt.Errorf("nft: %w: %s", err, msg)
	}
	return nil
}

// NftApplier — Applier поверх Runner.
type NftApplier struct {
	Runner  Runner
	Timeout time.Duration // 0 — 15 с
	// ActiveFile — куда записывать применённый набор правил (tmpfs). Include fw4
	// (ruleset-post) подхватывает файл при перезагрузке файрвола, и таблица
	// inet lavray переживает `fw4 reload`. Пусто — не писать.
	ActiveFile string
}

func (a *NftApplier) run(script string) error {
	d := a.Timeout
	if d <= 0 {
		d = defaultNftTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return a.Runner.Run(ctx, script)
}

// Apply устанавливает набор правил (заменяя прежний целиком). Файл для fw4
// пишется ДО nft: не удалось записать — ничего не применено; nft отказал —
// файл убирается, чтобы fw4 не поднял неприменимый набор.
func (a *NftApplier) Apply(script string) error {
	if err := a.writeActive(script); err != nil {
		return err
	}
	if err := a.run(script); err != nil {
		_ = a.removeActive()
		return err
	}
	return nil
}

// Remove снимает таблицу inet lavray; интернет у LAN идёт напрямую. Файл для
// fw4 убирается первым, чтобы перезагрузка файрвола не вернула правила.
func (a *NftApplier) Remove() error {
	fileErr := a.removeActive()
	if err := a.run(removeScript); err != nil {
		return err
	}
	return fileErr
}

func (a *NftApplier) writeActive(script string) error {
	if a.ActiveFile == "" {
		return nil
	}
	dir := filepath.Dir(a.ActiveFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("gateway: каталог %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".active-*")
	if err != nil {
		return fmt.Errorf("gateway: файл правил для fw4: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(script)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(name, a.ActiveFile)
	}
	if werr != nil {
		_ = os.Remove(name)
		return fmt.Errorf("gateway: файл правил для fw4: %w", werr)
	}
	return nil
}

func (a *NftApplier) removeActive() error {
	if a.ActiveFile == "" {
		return nil
	}
	if err := os.Remove(a.ActiveFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("gateway: удаление файла правил для fw4: %w", err)
	}
	return nil
}

var _ Applier = (*NftApplier)(nil)
