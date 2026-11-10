// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingDescriptor
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetsEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingsMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.savedOrPending
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingKeys
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingsState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingsWrite
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** E-02, E-19, E-20: which saved settings wait for the next tunnel, and what the core's answer means. */
class SettingsRestartTest {
    @Test fun everySettingTheTunnelIsBuiltFromIsOfferedAReconnect() {
        listOf(
            SettingKeys.TUN_MTU, SettingKeys.TUN_IPV6, SettingKeys.TUN_FAKE_DNS, SettingKeys.TUN_EXCLUDE_ADDRESSES,
            SettingKeys.MUX_PROTOCOL, SettingKeys.MUX_CONCURRENCY, SettingKeys.MUX_DEFAULT_ON, SettingKeys.INSECURE_TLS,
        ).forEach { assertTrue("$it", SettingKeys.appliesOnNextTunnel(it)) }
    }

    @Test fun theHealthCheckAndUnknownKeysAreNot() {
        assertFalse(SettingKeys.appliesOnNextTunnel(SettingKeys.HEALTH_CHECK))
        assertFalse(SettingKeys.appliesOnNextTunnel("log.level"))
        // Everything this screen shows is either in the list or the health check: a new key must be decided on purpose.
        assertEquals(setOf(SettingKeys.HEALTH_CHECK), SettingKeys.SHOWN.toSet() - SettingKeys.APPLY_ON_NEXT_TUNNEL)
    }

    @Test fun aKeyThatIsNotInAcceptedKeysWasNotTaken() {
        assertEquals(
            SettingsWrite.Outcome.Accepted(coreAskedForRestart = false),
            SettingsWrite.outcome("tun.mtu", SetSettingsResponse(acceptedKeys = listOf("tun.mtu"))),
        )
        assertEquals(
            SettingsWrite.Outcome.Accepted(coreAskedForRestart = true),
            SettingsWrite.outcome("tun.mtu", SetSettingsResponse(acceptedKeys = listOf("tun.mtu"), needsProfileRestart = true)),
        )
        assertEquals(SettingsWrite.Outcome.Rejected, SettingsWrite.outcome("tun.mtu", SetSettingsResponse()))
        assertEquals(SettingsWrite.Outcome.Rejected, SettingsWrite.outcome("tun.mtu", SetSettingsResponse(acceptedKeys = listOf("other"))))
    }

    @Test fun builtInDefaultsAreNotShownAsSavedValuesBeforeTheCoreWasRead() {
        assertFalse(SettingsState().ready)
        assertFalse(SettingsState(loaded = true, readFailed = true).ready)
        assertTrue(SettingsState(loaded = true).ready)
    }

    @Test fun aToastFollowsTheAnswerToTheActionNotAGlobalFlag() {
        assertEquals(R.string.toast_savedRestart, savedOrPending(true, R.string.toast_presetActivated))
        assertEquals(R.string.toast_presetActivated, savedOrPending(false, R.string.toast_presetActivated))
    }

    @Test fun anEventCarriesTheCoresAnswerForItsOwnChange() {
        assertEquals(R.string.toast_presetDeleted, savedOrPending(PresetsEvent.Deleted(needsRestart = false).needsRestart, R.string.toast_presetDeleted))
        assertEquals(R.string.toast_savedRestart, savedOrPending(PresetsEvent.Copied(needsRestart = true).needsRestart, R.string.toast_presetCopied))
        assertEquals(R.string.toast_saved, savedOrPending(DnsEvent.Saved(needsRestart = false).needsRestart, R.string.toast_saved))
    }

    @Test fun aDescriptorDefaultIsComparedWithTheStoredValue() {
        val mtu = SettingDescriptor(key = SettingKeys.TUN_MTU, defaultValueJson = "9000")
        val flag = SettingDescriptor(key = SettingKeys.TUN_IPV6, defaultValueJson = "false")
        val mux = SettingDescriptor(key = SettingKeys.MUX_PROTOCOL, defaultValueJson = "\"h2mux\"")
        assertEquals(SettingValue(intValue = 9000), SettingsMapper.defaultValue(mtu))
        assertEquals(SettingValue(boolValue = false), SettingsMapper.defaultValue(flag))
        assertEquals(SettingValue(stringValue = "h2mux"), SettingsMapper.defaultValue(mux))
        assertEquals(null, SettingsMapper.defaultValue(null))
        val state = SettingsState(loaded = true, descriptors = mapOf(SettingKeys.TUN_MTU to mtu), values = mapOf(SettingKeys.TUN_IPV6 to SettingValue(boolValue = true)))
        assertEquals("never set equals set to the default", SettingValue(intValue = 9000), state.effective(SettingKeys.TUN_MTU))
        assertEquals(SettingValue(boolValue = true), state.effective(SettingKeys.TUN_IPV6))
    }

    @Test fun aRestartNeededByTheCoreForAnotherKeyIsAProfileRestartNotARebuild() {
        assertTrue(PendingReason.Core.coreRestartSuffices)
        assertFalse(PendingReason.Tunnel.coreRestartSuffices)
        assertFalse(PendingReason.Apps.coreRestartSuffices)
    }
}
