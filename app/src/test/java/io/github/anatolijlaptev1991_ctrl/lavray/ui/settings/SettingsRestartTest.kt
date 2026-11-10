// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsResponse
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

    @Test fun aToastNeverSaysAppliedWhileTheTunnelStillRunsTheOldConfig() {
        assertEquals(R.string.toast_savedRestart, savedOrPending(true, R.string.toast_presetActivated))
        assertEquals(R.string.toast_presetActivated, savedOrPending(false, R.string.toast_presetActivated))
    }
}
