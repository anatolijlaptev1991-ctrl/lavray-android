// SPDX-License-Identifier: GPL-3.0-only

package api

// share_options.go — настройки share-ссылок (хвосты A).

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/links"

// settingVMessOldFormat — формат vmess:// по умолчанию: true — старый v2rayN
// (base64 JSON, как у NekoBox old_share_link_format), false — URL-формат.
const settingVMessOldFormat = "share.vmess_old_format"

// shareOptions — параметры ссылок FORMAT_STANDARD по настройкам.
func (s *Server) shareOptions() links.Options {
	return links.Options{VMessURLFormat: !s.settingBool(settingVMessOldFormat)}
}
