// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import kotlinx.serialization.KSerializer
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonPrimitive
import java.util.Base64

/**
 * proto3 JSON configuration shared by every call: defaults are omitted on
 * output, unknown keys and unknown enum names from a newer core are ignored.
 */
val ProtoJson: Json = Json {
    ignoreUnknownKeys = true
    coerceInputValues = true
    encodeDefaults = false
    explicitNulls = false
}

/** int64 travels as a JSON string; a bare number is accepted on input too. */
object Int64StringSerializer : KSerializer<Long> {
    override val descriptor: SerialDescriptor = PrimitiveSerialDescriptor("Int64", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: Long) = encoder.encodeString(value.toString())

    override fun deserialize(decoder: Decoder): Long {
        val text = if (decoder is JsonDecoder) {
            (decoder.decodeJsonElement() as JsonPrimitive).content
        } else {
            decoder.decodeString()
        }
        return text.toLong()
    }
}

/** uint64 travels as a JSON string; held in an unsigned Kotlin type. */
object UInt64StringSerializer : KSerializer<ULong> {
    override val descriptor: SerialDescriptor = PrimitiveSerialDescriptor("UInt64", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: ULong) = encoder.encodeString(value.toString())

    override fun deserialize(decoder: Decoder): ULong {
        val text = if (decoder is JsonDecoder) {
            (decoder.decodeJsonElement() as JsonPrimitive).content
        } else {
            decoder.decodeString()
        }
        return text.toULong()
    }
}

/** bytes travel as standard base64 (padding accepted or absent on input). */
object Base64BytesSerializer : KSerializer<ByteArray> {
    override val descriptor: SerialDescriptor = PrimitiveSerialDescriptor("Bytes", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: ByteArray) =
        encoder.encodeString(Base64.getEncoder().encodeToString(value))

    override fun deserialize(decoder: Decoder): ByteArray = Base64.getDecoder().decode(decoder.decodeString())
}
