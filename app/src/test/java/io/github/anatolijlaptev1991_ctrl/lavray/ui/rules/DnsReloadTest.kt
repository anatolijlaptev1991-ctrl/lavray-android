// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsReducer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** E-13: the screen reloads on every return to the app; text that was typed and not saved must survive that. */
class DnsReloadTest {
    private val saved = DnsSettings(
        mode = DnsSettings.Mode.MODE_SIMPLE, choice = "custom", remoteDns = "tls://1.1.1.1", advancedJson = "{\"a\":1}",
    )

    private fun loaded(): DnsState = DnsReducer.reduce(DnsState(), DnsAction.Loaded(saved))

    @Test fun aReloadKeepsOwnServersThatWereTypedAndNotSaved() {
        val typing = DnsReducer.reduce(loaded(), DnsAction.Remote("tls://9.9.9.9"))
        val after = DnsReducer.reduce(typing, DnsAction.Loaded(saved))
        assertEquals("tls://9.9.9.9", after.remote)
    }

    @Test fun aReloadKeepsTheJsonThatWasEditedAndTheOpenEditor() {
        val editing = DnsReducer.reduce(DnsReducer.reduce(loaded(), DnsAction.Advanced(true)), DnsAction.Json("{\"b\":2}"))
        val after = DnsReducer.reduce(editing, DnsAction.Loaded(saved))
        assertEquals("{\"b\":2}", after.json)
        assertTrue(after.advanced)
    }

    @Test fun anUntouchedFieldFollowsTheCoreOnReload() {
        val changedElsewhere = saved.copy(remoteDns = "tls://8.8.8.8", advancedJson = "{\"c\":3}")
        val after = DnsReducer.reduce(loaded(), DnsAction.Loaded(changedElsewhere))
        assertEquals("tls://8.8.8.8", after.remote)
        assertEquals("{\"c\":3}", after.json)
        assertFalse(after.advanced)
    }

    @Test fun aValueEqualToTheSavedOneIsNotTreatedAsEdited() {
        val retyped = DnsReducer.reduce(DnsReducer.reduce(loaded(), DnsAction.Remote("x")), DnsAction.Remote("tls://1.1.1.1"))
        val after = DnsReducer.reduce(retyped, DnsAction.Loaded(saved.copy(remoteDns = "tls://8.8.8.8")))
        assertEquals("tls://8.8.8.8", after.remote)
    }

    @Test fun aSaveReplacesTheSavedValueSoTheTextIsNoLongerEdited() {
        val typing = DnsReducer.reduce(loaded(), DnsAction.Remote("tls://9.9.9.9"))
        val applied = DnsReducer.reduce(typing, DnsAction.Applied(saved.copy(remoteDns = "tls://9.9.9.9")))
        val after = DnsReducer.reduce(applied, DnsAction.Loaded(saved.copy(remoteDns = "tls://4.4.4.4")))
        assertEquals("tls://4.4.4.4", after.remote)
    }
}
