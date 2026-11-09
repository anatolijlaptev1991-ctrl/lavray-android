// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

/** Where the core listens and the bearer token that opens it. The token never appears in toString. */
class CoreEndpoint(val socketPath: String, val token: String) {
    override fun toString(): String = "CoreEndpoint(socketPath=$socketPath, token=<redacted>)"
}
