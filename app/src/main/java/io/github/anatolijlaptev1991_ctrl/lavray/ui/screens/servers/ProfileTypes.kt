// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType

/**
 * Protocol name for badges. Protocol names are technical tokens and stay untranslated (glossary rule 3);
 * only "chain" and "custom JSON" come from the dictionary (desktop lib/profileType.ts).
 */
@Composable
fun ProfileType.label(): String = when (this) {
    ProfileType.PROFILE_TYPE_SOCKS -> "SOCKS"
    ProfileType.PROFILE_TYPE_HTTP -> "HTTP"
    ProfileType.PROFILE_TYPE_SHADOWSOCKS -> "Shadowsocks"
    ProfileType.PROFILE_TYPE_TROJAN -> "Trojan"
    ProfileType.PROFILE_TYPE_VMESS -> "VMess"
    ProfileType.PROFILE_TYPE_VLESS -> "VLESS"
    ProfileType.PROFILE_TYPE_NAIVE -> "Naive"
    ProfileType.PROFILE_TYPE_HYSTERIA2 -> "Hysteria2"
    ProfileType.PROFILE_TYPE_TUIC -> "TUIC"
    ProfileType.PROFILE_TYPE_CHAIN -> stringResource(R.string.ed_t_chain)
    ProfileType.PROFILE_TYPE_CUSTOM_OUTBOUND,
    ProfileType.PROFILE_TYPE_CUSTOM_CONFIG,
    ProfileType.PROFILE_TYPE_CUSTOM_EXTERNAL,
    -> stringResource(R.string.ed_t_json)
    ProfileType.PROFILE_TYPE_UNSPECIFIED -> "—"
}
