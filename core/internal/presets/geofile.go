// SPDX-License-Identifier: GPL-3.0-only

package presets

// GeoFile возвращает встроенную копию гео-набора (rel вида
// "geosite/category-ru.srs"). Тестер маршрута (T-30) читает её, если файла
// нет в каталоге данных.
func GeoFile(rel string) ([]byte, error) { return geoFS.ReadFile("geo/" + rel) }
