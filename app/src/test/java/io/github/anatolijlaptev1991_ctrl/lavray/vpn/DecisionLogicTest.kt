// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class DecisionLogicTest {
    private val own = "io.github.anatolijlaptev1991_ctrl.lavray"

    @Test
    fun startWithoutOurExtraIsTheSystemsAlwaysOn() {
        assertEquals(StartCommand.Connect(StartSource.SYSTEM, null), StartIntent.parse("android.net.VpnService", null, 0))
        assertEquals(StartCommand.Connect(StartSource.SYSTEM, null), StartIntent.parse(null, null, 0))
        assertTrue(StartSource.SYSTEM.retries)
    }

    @Test
    fun ourExtrasAreHonoured() {
        assertEquals(StartCommand.Connect(StartSource.USER, 7), StartIntent.parse(null, "user", 7))
        assertEquals(StartCommand.Connect(StartSource.BOOT, null), StartIntent.parse(null, "boot", 0))
        assertFalse(StartSource.USER.retries)
        assertEquals(StartCommand.Disconnect, StartIntent.parse(StartIntent.ACTION_DISCONNECT, "user", 3))
    }

    @Test
    fun bootPolicy() {
        assertEquals(BootAction.IGNORE, BootPolicy.decide(autoConnectEnabled = false, consentGranted = true))
        assertEquals(BootAction.IGNORE, BootPolicy.decide(false, false))
        assertEquals(BootAction.NOTIFY_PERMISSION, BootPolicy.decide(true, false))
        assertEquals(BootAction.START_SERVICE, BootPolicy.decide(true, true))
    }

    @Test
    fun processStaysOnlyWhileSomethingNeedsIt() {
        assertFalse(ShutdownPolicy.shouldStop(vpnActive = true, uiBound = false, autoConnectPending = false))
        assertFalse(ShutdownPolicy.shouldStop(false, true, false))
        assertFalse(ShutdownPolicy.shouldStop(false, false, true))
        assertTrue(ShutdownPolicy.shouldStop(false, false, false))
        assertTrue(ShutdownPolicy.shouldLeaveForeground(false, false))
        assertFalse(ShutdownPolicy.shouldLeaveForeground(true, false))
        assertFalse(ShutdownPolicy.shouldLeaveForeground(false, true))
    }

    @Test
    fun coreStateMapping() {
        assertEquals(VpnState.CONNECTED, CoreStatusMapper.fromCore(CoreState.CORE_STATE_CONNECTED, null).state)
        assertEquals(VpnState.CONNECTING, CoreStatusMapper.fromCore(CoreState.CORE_STATE_RECONNECTING, null).state)
        val err = CoreStatusMapper.fromCore(CoreState.CORE_STATE_ERROR, "bad")
        assertEquals(VpnState.ERROR, err.state)
        assertEquals("bad", err.error)
        assertNull(CoreStatusMapper.fromCore(CoreState.CORE_STATE_IDLE, "stale").error)
    }

    @Test
    fun permanentErrorsMatchTheDesktopRules() {
        val permanent = listOf(ConnectCode.FAILED_PRECONDITION, ConnectCode.INVALID_ARGUMENT, ConnectCode.NOT_FOUND, ConnectCode.PERMISSION_DENIED)
        for (code in permanent) assertTrue(code.name, CoreStatusMapper.isPermanent(code))
        val transient = listOf(ConnectCode.UNAVAILABLE, ConnectCode.DEADLINE_EXCEEDED, ConnectCode.INTERNAL, ConnectCode.UNKNOWN)
        for (code in transient) assertFalse(code.name, CoreStatusMapper.isPermanent(code))
    }

    @Test
    fun classifyFailures() {
        assertTrue(ConnectAttempt.classify(NoServerException()).permanent)
        assertTrue(ConnectAttempt.classify(CoreStartException("x")).permanent)
        assertFalse(ConnectAttempt.classify(java.io.IOException("reset")).permanent)
    }

    @Test
    fun cidrParsing() {
        assertEquals(CidrAddress("172.19.0.1", 30), Cidr.parse("172.19.0.1/30"))
        assertEquals(CidrAddress("fdfe:dcba:9876::1", 126), Cidr.parse("fdfe:dcba:9876::1/126"))
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("172.19.0.1") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("172.19.0.1/abc") }
        assertThrows(IllegalArgumentException::class.java) { Cidr.parse("1.2.3.4/200") }
    }

    @Test
    fun tunOptionsParseWithMissingAndUnknownKeys() {
        val o = TunOptions.parse("{\"mtu\":1500,\"inet4\":[\"172.19.0.1/30\"],\"dns\":[\"172.19.0.2\"],\"extra\":1}")
        assertEquals(1500, o.mtu)
        assertEquals(listOf("172.19.0.1/30"), o.inet4)
        assertTrue(o.inet6.isEmpty())
        assertTrue(o.includePackages.isEmpty())
    }

    @Test
    fun ownPackageIsAlwaysOutsideTheTunnel() {
        val rules = AppRules.from(TunOptions(excludePackages = listOf("com.a", "com.a", " ")), own)
        assertEquals(AppRules(emptyList(), listOf("com.a", own)), rules)
        assertEquals(AppRules(emptyList(), listOf(own)), AppRules.from(TunOptions(), own))
    }

    @Test
    fun allowListModeDropsOwnPackageAndNeverMixesBothLists() {
        val rules = AppRules.from(TunOptions(includePackages = listOf("com.b", own), excludePackages = listOf("com.c")), own)
        assertEquals(AppRules(listOf("com.b"), emptyList()), rules)
        assertThrows(IllegalArgumentException::class.java) { AppRules.from(TunOptions(includePackages = listOf(own)), own) }
    }

    @Test
    fun interfaceListJsonWritesDefaultsOut() {
        val json = InterfaceInfo.encodeList(listOf(InterfaceInfo("wlan0", 3, mtu = 1500, addrs = listOf("192.168.1.5/24"), type = "wifi")))
        assertEquals(
            "[{\"name\":\"wlan0\",\"index\":3,\"mtu\":1500,\"addrs\":[\"192.168.1.5/24\"],\"flags\":19,\"metered\":false,\"type\":\"wifi\"}]",
            json,
        )
    }
}
