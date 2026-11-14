// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InterfaceInfoTest {
    private val linux = IffMasks.LINUX

    @Test
    fun linuxMasksAreTheKernelValues() {
        assertEquals(0x1, linux.up)
        assertEquals(0x2, linux.broadcast)
        assertEquals(0x10, linux.pointToPoint)
        assertEquals(0x40, linux.running)
        assertEquals(0x1000, linux.multicast)
    }

    @Test
    fun wifiIsBroadcastNotPointToPoint() {
        val f = InterfaceFlags.forType("wifi", linux)
        assertEquals(0x1043, f)
        assertTrue(f and linux.broadcast != 0)
        assertFalse(f and linux.pointToPoint != 0)
        assertEquals(f, InterfaceFlags.forType("ethernet", linux))
    }

    @Test
    fun cellularIsPointToPointNotBroadcast() {
        val f = InterfaceFlags.forType("cellular", linux)
        assertEquals(0x1051, f)
        assertTrue(f and linux.pointToPoint != 0)
        assertFalse(f and linux.broadcast != 0)
    }

    @Test
    fun everyTypeIsUpRunningMulticast() {
        for (type in listOf("wifi", "cellular", "ethernet", "other", "weird")) {
            val f = InterfaceFlags.forType(type, linux)
            assertTrue(type, f and (linux.up or linux.running or linux.multicast) == (linux.up or linux.running or linux.multicast))
        }
        assertEquals(0x1041, InterfaceFlags.forType("other", linux))
    }

    /** The exact text Go's TestParseInterfacesFromKotlin (core/mobile/ifaces_kotlin_test.go) is fed. */
    @Test
    fun goldenJsonForTheGoSide() {
        val list = listOf(
            InterfaceInfo(
                "wlan0", 3, mtu = 1500, addrs = listOf("192.168.1.5/24", "fe80::1/64"),
                flags = InterfaceFlags.forType("wifi", linux), type = "wifi",
                dns = listOf("192.168.1.1"), gateway = listOf("192.168.1.1"),
            ),
            InterfaceInfo(
                "rmnet0", 4, mtu = 1400, addrs = listOf("10.1.2.3/30"),
                flags = InterfaceFlags.forType("cellular", linux), metered = true, type = "cellular",
            ),
        )
        assertEquals(
            """[{"name":"wlan0","index":3,"mtu":1500,"addrs":["192.168.1.5/24","fe80::1/64"],"flags":4163,"metered":false,"type":"wifi","dns":["192.168.1.1"],"gateway":["192.168.1.1"]},""" +
                """{"name":"rmnet0","index":4,"mtu":1400,"addrs":["10.1.2.3/30"],"flags":4177,"metered":true,"type":"cellular","dns":[],"gateway":[]}]""",
            InterfaceInfo.encodeList(list),
        )
    }
}
