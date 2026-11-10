// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsSetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch

/** What the service takes from the app: the three commands and the status. The controller in the app, a fake in tests. */
interface TunnelCommands {
    val status: StateFlow<VpnStatus>

    fun connect(profileId: Long)

    fun reconnect(profileId: Long)

    fun disconnect()
}

/** [TunnelCommands] over the real controller. */
class ControllerCommands(private val controller: VpnController) : TunnelCommands {
    override val status: StateFlow<VpnStatus> get() = controller.status

    override fun connect(profileId: Long) = controller.connect(profileId)

    override fun reconnect(profileId: Long) = controller.reconnect(profileId)

    override fun disconnect() = controller.disconnect()
}

/**
 * Connect, switch and disconnect as the person means them. The consent dialog is not here: it needs an
 * activity, so [io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.rememberVpnConnect] asks for it first and then calls [connect].
 *
 * Commands are carried out in the order they were given, one at a time: choosing a server first writes it down for the
 * core (a call that takes a moment), and a second tap or a Disconnect during that moment must not overtake the first.
 */
class VpnActions(
    private val commands: TunnelCommands,
    /** Writes the chosen server as the one to reconnect to; a failure is not a reason to hold the command back. */
    private val remember: suspend (Long) -> Unit,
    scope: CoroutineScope,
    private val onServerChosen: (Long) -> Unit,
) {
    constructor(controller: VpnController, session: CoreSession, scope: CoroutineScope, onServerChosen: (Long) -> Unit) :
        this(ControllerCommands(controller), { id -> session.call { settingsSetSettings(SetSettingsRequest(mapOf(REMEMBER_ID to SettingValue(intValue = id)))) }; Unit }, scope, onServerChosen)

    private sealed interface Command {
        data class Connect(val profileId: Long) : Command
        data object Disconnect : Command
    }

    private val queue = Channel<Command>(Channel.UNLIMITED)

    val status: StateFlow<VpnStatus> get() = commands.status

    init {
        scope.launch {
            for (command in queue) {
                try {
                    carryOut(command)
                } catch (e: CancellationException) {
                    throw e
                } catch (_: Exception) {
                    // One command that fails must not stop the ones behind it.
                }
            }
        }
    }

    /**
     * Connects [profileId]. While a tunnel is up, coming up, waiting for a network or tearing down, the switch goes
     * through the service's `reconnect`: it rebuilds the tunnel without a direct-traffic gap and runs after a
     * disconnect that is still in progress, so a connect is never issued into a tunnel that is going down.
     */
    fun connect(profileId: Long) {
        onServerChosen(profileId)
        queue.trySend(Command.Connect(profileId))
    }

    fun disconnect() {
        queue.trySend(Command.Disconnect)
    }

    private suspend fun carryOut(command: Command) {
        when (command) {
            is Command.Connect -> {
                try {
                    remember(command.profileId)
                } catch (e: CancellationException) {
                    throw e
                } catch (_: Exception) {
                    // The choice could not be written down: the tunnel is still switched, the next start may use the older one.
                }
                // The state is read when the command runs, after the write: that is what the tunnel is doing by then.
                when (SwitchPlan.decide(commands.status.value.state)) {
                    SwitchPlan.Action.CONNECT -> commands.connect(command.profileId)
                    SwitchPlan.Action.RECONNECT -> commands.reconnect(command.profileId)
                }
            }
            Command.Disconnect -> commands.disconnect()
        }
    }

    private companion object {
        const val REMEMBER_ID = "remember.id"
    }
}

/** What choosing a server means in each state of the tunnel. */
object SwitchPlan {
    enum class Action { CONNECT, RECONNECT }

    /** Only a tunnel that is definitely down is connected afresh; anything else is rebuilt by the service. */
    fun decide(state: VpnState): Action = when (state) {
        VpnState.IDLE, VpnState.ERROR, VpnState.NEEDS_PERMISSION -> Action.CONNECT
        VpnState.CONNECTING, VpnState.CONNECTED, VpnState.DISCONNECTING, VpnState.WAITING_NETWORK -> Action.RECONNECT
    }
}
