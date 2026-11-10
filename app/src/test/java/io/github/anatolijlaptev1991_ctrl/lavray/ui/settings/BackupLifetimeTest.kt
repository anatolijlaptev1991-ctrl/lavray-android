// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.hasTunnel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** E-07, E-17, E-24: the backup model outlives its screen; what it keeps and when the restore is refused. */
class BackupLifetimeTest {
    @Test fun aFinishedRestoreSurvivesLeavingTheScreen() {
        val done = BackupState(restored = true, needsRestart = true)
        assertEquals(done, done.afterRelease())
    }

    @Test fun aRestoreThatIsRunningIsNotDroppedWhenTheScreenIsLeft() {
        val running = BackupState(busy = true, pickedName = "copy.lavbk")
        assertEquals(running, running.afterRelease())
    }

    @Test fun aPickedFileWithoutItsBytesIsForgottenOnLeaving() {
        val picked = BackupState(pickedName = "copy.lavbk", error = null)
        val after = picked.afterRelease()
        assertNull(after.pickedName)
        assertNull(after.summary)
        assertFalse(after.saved)
    }

    @Test fun theCoreRefusesARestoreWhileAnyConnectionExists() {
        assertTrue(Phase.On.hasTunnel)
        assertTrue(Phase.Connecting.hasTunnel)
        assertTrue(Phase.WaitingNetwork.hasTunnel)
        assertTrue(Phase.Disconnecting.hasTunnel)
        assertFalse(Phase.Off.hasTunnel)
        assertFalse(Phase.Error.hasTunnel)
        assertFalse(Phase.NeedsPermission.hasTunnel)
    }
}
