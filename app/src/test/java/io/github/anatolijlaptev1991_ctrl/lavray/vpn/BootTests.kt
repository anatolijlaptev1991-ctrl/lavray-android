// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BootTriggerTest {
    @Test
    fun actionsMapToTriggers() {
        assertEquals(BootTrigger.BOOT, BootTrigger.fromAction("android.intent.action.BOOT_COMPLETED"))
        assertEquals(BootTrigger.BOOT, BootTrigger.fromAction("android.intent.action.QUICKBOOT_POWERON"))
        assertEquals(BootTrigger.BOOT, BootTrigger.fromAction("com.htc.intent.action.QUICKBOOT_POWERON"))
        assertEquals(BootTrigger.UPDATE, BootTrigger.fromAction("android.intent.action.MY_PACKAGE_REPLACED"))
        assertNull(BootTrigger.fromAction("android.intent.action.SCREEN_ON"))
        assertNull(BootTrigger.fromAction(null))
    }

    @Test
    fun bootFollowsTheAutoConnectSetting() {
        assertEquals(BootAction.START_SERVICE, BootPolicy.decide(BootTrigger.BOOT, autoConnectEnabled = true, wantConnected = false, consentGranted = true))
        assertEquals(BootAction.IGNORE, BootPolicy.decide(BootTrigger.BOOT, false, true, true))
        assertEquals(BootAction.NOTIFY_PERMISSION, BootPolicy.decide(BootTrigger.BOOT, true, false, false))
    }

    @Test
    fun updateRestoresOnlyAWantedTunnel() {
        assertEquals(BootAction.START_SERVICE, BootPolicy.decide(BootTrigger.UPDATE, autoConnectEnabled = false, wantConnected = true, consentGranted = true))
        assertEquals(BootAction.IGNORE, BootPolicy.decide(BootTrigger.UPDATE, true, false, true))
        assertEquals(BootAction.NOTIFY_PERMISSION, BootPolicy.decide(BootTrigger.UPDATE, false, true, false))
    }

    @Test
    fun staleIntentDiesWithTheRebootUnlessAutoConnectRestoresIt() {
        assertTrue(BootPolicy.dropsOldIntent(BootTrigger.BOOT, autoConnectEnabled = false))
        assertFalse(BootPolicy.dropsOldIntent(BootTrigger.BOOT, true))
        assertFalse(BootPolicy.dropsOldIntent(BootTrigger.UPDATE, false))
    }
}

class BootReceiverSwitchTest {
    private class FakePort(var state: BootSwitch?, var failOn: BootSwitch? = null) : BootSwitchPort {
        val sets = ArrayList<BootSwitch>()
        override fun current() = state
        override fun set(target: BootSwitch) {
            if (target == failOn) throw SecurityException("refused")
            sets += target
            state = target
        }
    }

    @Test
    fun policyIsOnWhenAutoConnectIsOn() {
        assertEquals(BootSwitch.DEFAULT, BootComponentPolicy.desired(true))
        assertEquals(BootSwitch.DISABLED, BootComponentPolicy.desired(false))
    }

    @Test
    fun freshInstallWithAutoConnectOffSwitchesTheReceiverOff() {
        val port = FakePort(BootSwitch.DEFAULT)
        assertEquals(SwitchOutcome.CHANGED, BootReceiverSwitch(port).reconcile(false))
        assertEquals(listOf(BootSwitch.DISABLED), port.sets)
    }

    @Test
    fun updatedInstallWithAutoConnectOnTouchesNothing() {
        val port = FakePort(BootSwitch.DEFAULT)
        assertEquals(SwitchOutcome.UNCHANGED, BootReceiverSwitch(port).reconcile(true))
        assertTrue(port.sets.isEmpty())
    }

    @Test
    fun enablingAfterDisabledGoesBackToTheDefault() {
        val port = FakePort(BootSwitch.DISABLED)
        assertEquals(SwitchOutcome.CHANGED, BootReceiverSwitch(port).reconcile(true))
        assertEquals(listOf(BootSwitch.DEFAULT), port.sets)
    }

    @Test
    fun failureToDisableLeavesTheReceiverOn() {
        val port = FakePort(BootSwitch.DEFAULT, failOn = BootSwitch.DISABLED)
        assertEquals(SwitchOutcome.FAILED_STAYS_ENABLED, BootReceiverSwitch(port).reconcile(false))
        assertEquals(BootSwitch.DEFAULT, port.state)
    }

    @Test
    fun failureToEnableIsReportedAndRetriedByTheNextReconcile() {
        val port = FakePort(BootSwitch.DISABLED, failOn = BootSwitch.DEFAULT)
        val sw = BootReceiverSwitch(port)
        assertEquals(SwitchOutcome.FAILED_STILL_DISABLED, sw.reconcile(true))
        port.failOn = null
        assertEquals(SwitchOutcome.CHANGED, sw.reconcile(true))
        assertEquals(BootSwitch.DEFAULT, port.state)
    }

    @Test
    fun unreadableStateIsWrittenAnyway() {
        val port = FakePort(null)
        assertEquals(SwitchOutcome.CHANGED, BootReceiverSwitch(port).reconcile(true))
    }
}
