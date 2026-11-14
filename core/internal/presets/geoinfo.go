// SPDX-License-Identifier: GPL-3.0-only

package presets

// geoinfo.go — сведения о гео-наборе для апдейтера (T-75/7.5): что поставляется
// внутри бинарника, какая ревизия записана в каталоге и годится ли файл как .srs.

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"sort"
)

// GeoMarkerFile — имя маркера ревизии в каталоге geo («<ревизия> <время>»).
const GeoMarkerFile = geoMarker

// ValidSRS: файл похож на целый .srs sing-box (магия, версия, читаемый zlib-поток).
func ValidSRS(raw []byte) bool { return validSRS(raw) }

// GeoRevisionOf — ревизия из маркера каталога dir; 0, если маркера нет.
func GeoRevisionOf(dir string) int { return readGeoRevision(dir) }

// EmbeddedGeoFiles — пути файлов встроенного набора («geosite/openai.srs»),
// отсортированные. Каталог поставки обязан содержать их все: иначе пресет,
// ссылающийся на отсутствующий набор, не запустится.
func EmbeddedGeoFiles() ([]string, error) {
	entries, err := geoManifest()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.rel)
	}
	sort.Strings(out)
	return out, nil
}

// MaxSRSInflated — предел разжатого размера одного .srs. Настоящие наборы
// разжимаются в единицы мегабайт; вложенный zlib-поток, раздувающийся дальше,
// считается негодным файлом (проверка идёт и при каждом запуске, и при применении).
const MaxSRSInflated = 64 << 20

var (
	// ErrSRSInvalid — не .srs: нет магии, битый или усечённый zlib-поток.
	ErrSRSInvalid = errors.New("presets: файл не является корректным .srs")
	// ErrSRSTooLarge — zlib-поток .srs разжимается больше допустимого.
	ErrSRSTooLarge = errors.New("presets: .srs разжимается больше допустимого")
)

// InflatedSRSSize проверяет файл как .srs и возвращает его разжатый размер, не
// разжимая больше limit байт (превышение — ErrSRSTooLarge, остальное — ErrSRSInvalid).
func InflatedSRSSize(raw []byte, limit int64) (int64, error) {
	if len(raw) < 5 || !bytes.HasPrefix(raw, []byte("SRS")) {
		return 0, ErrSRSInvalid
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		return 0, ErrSRSInvalid
	}
	n, err := io.CopyN(io.Discard, zr, limit+1)
	switch {
	case n > limit:
		return n, ErrSRSTooLarge
	case err != nil && !errors.Is(err, io.EOF):
		return n, ErrSRSInvalid
	case zr.Close() != nil:
		return n, ErrSRSInvalid
	}
	return n, nil
}
