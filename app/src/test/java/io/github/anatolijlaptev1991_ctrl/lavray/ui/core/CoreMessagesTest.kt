// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CoreMessagesTest {
    @Test fun theLineIsTakenFromTheCoresRussianAndEnglishWording() {
        assertEquals(2, CoreMessages.lineOf("не распознано ни одного профиля (первая ошибка: строка 2: ss: некорректный base64)"))
        assertEquals(7, CoreMessages.lineOf("nothing recognised (first error: line 7: bad)"))
    }

    @Test fun noLineMeansNull() {
        assertNull(CoreMessages.lineOf(null))
        assertNull(CoreMessages.lineOf("content пуст"))
        assertNull(CoreMessages.lineOf("строка 0: x"))
    }
}
