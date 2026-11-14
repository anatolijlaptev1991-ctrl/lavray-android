// SPDX-License-Identifier: GPL-3.0-only

// errors_apply.go — ошибки применения пресетов и гео-набора (T-75/7.5);
// api-слой раскладывает их по Connect-кодам (update_comp_errors.go).
package updates

import "errors"

var (
	// ErrNoPrevious — откатываться не к чему: прошлой ревизии нет.
	ErrNoPrevious = errors.New("updates: прошлой ревизии нет")
	// ErrPresetRejected — набор из апдейта не принят (id, ревизия, схема, builtin).
	ErrPresetRejected = errors.New("updates: набор из обновления отвергнут")
	// ErrPresetNotNewer — ревизия набора не выше установленной.
	ErrPresetNotNewer = errors.New("updates: ревизия набора не выше установленной")
	// ErrGeoNotNewer — ревизия гео-набора не выше установленной (откат назад).
	ErrGeoNotNewer = errors.New("updates: ревизия гео-набора не выше установленной")
	// ErrArchiveEntry — запись архива недопустима: путь, ссылка, каталог, шифрование.
	ErrArchiveEntry = errors.New("updates: недопустимая запись архива")
	// ErrArchiveLimit — архив превышает лимиты (записей, размера, сжатия): возможная zip-бомба.
	ErrArchiveLimit = errors.New("updates: архив превышает лимиты распаковки")
	// ErrGeoIncomplete — в гео-архиве нет файлов встроенного набора.
	ErrGeoIncomplete = errors.New("updates: гео-архив неполон")
	// ErrGeoInvalid — файл архива не является корректным .srs.
	ErrGeoInvalid = errors.New("updates: файл гео-архива не является .srs")
	// ErrDirBusy — каталог нельзя переименовать (занят работающим соединением).
	// Сообщение общее, без путей: подробности — в ErrorDetail, для журнала.
	ErrDirBusy = errors.New("updates: каталог занят и не может быть заменён")
)
