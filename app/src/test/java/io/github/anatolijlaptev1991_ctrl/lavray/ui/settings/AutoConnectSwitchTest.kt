// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AutoConnectSwitch
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AutoConnectUi
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.IOException

/** M-4: the switch shows what the service really stored, and says so when a write was rolled back. */
class AutoConnectSwitchTest {
    private class Store(var stored: Boolean = false, var answer: Boolean? = true, var readFails: Boolean = false) {
        fun read(): Boolean = if (readFails) throw IOException("dead") else stored
        fun write(on: Boolean): Boolean = when (answer) {
            true -> { stored = on; true }
            false -> false
            null -> throw IOException("dead")
        }
    }

    private fun switch(s: Store) = AutoConnectSwitch({ s.read() }, { s.write(it) })

    @Test fun theSwitchShowsTheStoredValueOnceRead() = runBlocking {
        val sw = switch(Store(stored = true))
        assertEquals(AutoConnectUi(value = null), sw.state.value)
        sw.load()
        assertEquals(AutoConnectUi(value = true), sw.state.value)
    }

    @Test fun anUnreadableValueIsNotGuessed() = runBlocking {
        val sw = switch(Store(readFails = true))
        sw.load()
        assertEquals(AutoConnectUi(value = null, readFailed = true), sw.state.value)
    }

    @Test fun aWriteThatTookMovesTheSwitch() = runBlocking {
        val store = Store(stored = false)
        val sw = switch(store)
        sw.load()
        sw.set(true)
        assertEquals(AutoConnectUi(value = true), sw.state.value)
        assertEquals(true, store.stored)
    }

    @Test fun aRolledBackWriteLeavesTheOldValueAndSaysSo() = runBlocking {
        val store = Store(stored = false, answer = false)
        val sw = switch(store)
        sw.load()
        sw.set(true)
        assertEquals(AutoConnectUi(value = false, writeFailed = true), sw.state.value)
    }

    @Test fun aBinderErrorReadsTheStoredValueAgain() = runBlocking {
        val store = Store(stored = false, answer = null)
        val sw = switch(store)
        sw.load()
        sw.set(true)
        assertEquals("the value is what the service still holds", AutoConnectUi(value = false, writeFailed = true), sw.state.value)
        // The write may have got through before the binder died: then the switch shows it and no failure.
        val other = Store(stored = true, answer = null)
        val sw2 = switch(other)
        sw2.load()
        sw2.set(true)
        assertEquals(AutoConnectUi(value = true), sw2.state.value)
    }

    @Test fun aFailureIsForgottenByTheNextSuccessfulWrite() = runBlocking {
        val store = Store(stored = false, answer = false)
        val sw = switch(store)
        sw.load()
        sw.set(true)
        store.answer = true
        sw.set(true)
        assertEquals(AutoConnectUi(value = true), sw.state.value)
    }
}
