// SPDX-License-Identifier: GPL-3.0-only

package gateway

// watchdog_state.go — состояние watchdog для статуса шлюза и принудительные
// переходы: немедленное снятие правил при падении инстанса и разоружение.

// State — вооружён ли watchdog и установлены ли правила.
func (w *Watchdog) State() (armed, rulesUp bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.armed, w.rulesUp
}

// Tripped — правила сняты watchdog'ом (интернет идёт напрямую), ждём связи.
func (w *Watchdog) Tripped() bool {
	armed, up := w.State()
	return armed && !up
}

// Trip снимает правила немедленно (инстанс упал — ждать порога незачем) и
// оставляет watchdog вооружённым: Beat после восстановления вернёт правила.
// false — правила не сняты (их не было либо Remove не удался).
func (w *Watchdog) Trip() (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.armed || !w.rulesUp {
		return false, nil
	}
	if err := w.Applier.Remove(); err != nil {
		return false, err
	}
	w.rulesUp = false
	if w.OnFailover != nil {
		w.OnFailover(true)
	}
	return true, nil
}

// Reapply заново применяет правила с текущим списком политик под замком watchdog,
// если они сейчас установлены (applied=false — нет, ничего не делалось). Так
// смена политики не обгоняет Beat и Tick: правила, снятые watchdog'ом, обратно
// не возвращаются.
func (w *Watchdog) Reapply() (applied bool, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.armed || !w.rulesUp {
		return false, nil
	}
	return true, w.Applier.Apply(w.Renderer.Render(w.Config, w.Policies(), w.Config.SelfUser))
}

// Disarm снимает правила и разоружает watchdog (шлюз выключен: отключение,
// выход процесса). Beat после этого правила не вернёт, пока не вызван StartArmed.
func (w *Watchdog) Disarm() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	wasArmed := w.armed
	w.armed, w.rulesUp = false, false
	if !wasArmed {
		return nil
	}
	return w.Applier.Remove()
}
