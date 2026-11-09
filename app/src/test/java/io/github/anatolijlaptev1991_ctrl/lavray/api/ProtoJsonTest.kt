// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectMode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreEvent
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ExportBackupResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetTrafficTotalsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ShutdownRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeEventsRequest
import kotlinx.serialization.KSerializer
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The proto3 JSON rules the generated DTOs must honour. */
class ProtoJsonTest {
    private fun <T> decode(s: KSerializer<T>, json: String): T = ProtoJson.decodeFromString(s, json)

    @Test
    fun int64TravelsAsStringAndNumbersAreAcceptedToo() {
        val status = decode(CoreStatus.serializer(), "{\"activeProfileId\":\"5\",\"startedAtMs\":1700000000000}")
        assertEquals(5L, status.activeProfileId)
        assertEquals(1700000000000L, status.startedAtMs)
        assertEquals("{\"profileId\":\"5\"}", ProtoJson.encodeToString(ConnectRequest.serializer(), ConnectRequest(profileId = 5)))
    }

    @Test
    fun enumsAreStringsAndDefaultsAreOmitted() {
        assertEquals("{}", ProtoJson.encodeToString(ConnectRequest.serializer(), ConnectRequest()))
        assertEquals(
            "{\"mode\":\"CONNECT_MODE_TUN\"}",
            ProtoJson.encodeToString(ConnectRequest.serializer(), ConnectRequest(mode = ConnectMode.CONNECT_MODE_TUN)),
        )
        assertEquals(CoreState.CORE_STATE_ERROR, decode(CoreStatus.serializer(), "{\"state\":\"CORE_STATE_ERROR\"}").state)
        assertEquals(CoreState.CORE_STATE_IDLE, decode(CoreStatus.serializer(), "{}").state)
    }

    @Test
    fun explicitOptionalFalseOrZeroIsSentUnlikeAbsent() {
        val json = ProtoJson.encodeToString(ConnectRequest.serializer(), ConnectRequest(validateOnly = false, profileId = 0))
        assertEquals("{\"profileId\":\"0\",\"validateOnly\":false}", json)
    }

    @Test
    fun unknownKeysAndUnknownEnumNamesAreIgnored() {
        val status = decode(
            CoreStatus.serializer(),
            "{\"state\":\"CORE_STATE_FROM_THE_FUTURE\",\"newField\":{\"a\":1},\"coreVersion\":\"1.14.1-lav.2\"}",
        )
        assertEquals(CoreState.CORE_STATE_IDLE, status.state)
        assertEquals("1.14.1-lav.2", status.coreVersion)
    }

    @Test
    fun oneofMembersAreFlatNullableFields() {
        val ev = decode(CoreEvent.serializer(), "{\"tsMs\":\"3\",\"stateChanged\":{\"state\":\"CORE_STATE_CONNECTED\"}}")
        assertEquals(CoreState.CORE_STATE_CONNECTED, ev.stateChanged!!.state)
        assertNull(ev.warningRaised)
        assertNull(ev.shutdownImminent)
        assertEquals("exit", decode(CoreEvent.serializer(), "{\"shutdownImminent\":\"exit\"}").shutdownImminent)
    }

    @Test
    fun oneofScalarCarriesPresence() {
        val reply = decode(
            GetSettingsResponse.serializer(),
            "{\"values\":{\"remember.id\":{\"intValue\":\"42\"},\"x\":{\"boolValue\":false}}}",
        )
        assertEquals(42L, reply.values["remember.id"]!!.intValue)
        assertNull(reply.values["remember.id"]!!.boolValue)
        assertEquals(false, reply.values["x"]!!.boolValue)
    }

    @Test
    fun bytesAreBase64() {
        val data = byteArrayOf(0, 1, 2, -1, 127)
        val json = ProtoJson.encodeToString(ExportBackupResponse.serializer(), ExportBackupResponse(data = data, suggestedName = "a"))
        assertEquals("{\"data\":\"AAEC/38=\",\"suggestedName\":\"a\"}", json)
        assertArrayEquals(data, decode(ExportBackupResponse.serializer(), json).data)
    }

    @Test
    fun mapsWithInt64KeysDecode() {
        val r = decode(
            GetTrafficTotalsResponse.serializer(),
            "{\"totals\":{\"7\":{\"uplinkBytes\":\"10\",\"downlinkBytes\":\"20\"}}}",
        )
        assertEquals(10L, r.totals.getValue(7L).uplinkBytes)
        assertEquals(20L, r.totals.getValue(7L).downlinkBytes)
    }

    @Test
    fun nestedEnumsAndEmptyMessages() {
        assertEquals(
            "{\"reason\":\"REASON_EXIT\"}",
            ProtoJson.encodeToString(ShutdownRequest.serializer(), ShutdownRequest(ShutdownRequest.Reason.REASON_EXIT)),
        )
        assertEquals("{}", ProtoJson.encodeToString(SubscribeEventsRequest.serializer(), SubscribeEventsRequest()))
        assertEquals(Outbound.OUTBOUND_UNSPECIFIED, Outbound.entries.first())
    }
}
