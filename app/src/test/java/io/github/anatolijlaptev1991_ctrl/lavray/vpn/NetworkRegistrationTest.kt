// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Test

class NetworkRegistrationTest {
    // Regression: the plain default-network callback reports our own tunnel as the default
    // once it is up, so physical network changes were never delivered to the core.
    @Test
    fun modernApisAskForTheBestNonVpnNetwork() {
        assertEquals(NetworkRegistration.BEST_MATCHING, NetworkRegistration.forSdk(31))
        assertEquals(NetworkRegistration.BEST_MATCHING, NetworkRegistration.forSdk(36))
    }

    @Test
    fun androidOreoToElevenUseARequest() {
        for (sdk in 26..30) assertEquals("sdk $sdk", NetworkRegistration.REQUEST, NetworkRegistration.forSdk(sdk))
    }
}
