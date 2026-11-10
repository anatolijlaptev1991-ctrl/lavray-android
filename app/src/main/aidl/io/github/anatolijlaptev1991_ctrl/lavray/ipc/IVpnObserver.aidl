// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ipc;

oneway interface IVpnObserver {
    /**
     * state is VpnState.code; error is non-null only for ERROR. detail is a JSON object with the additive
     * fields of VpnStatus (healthError, unexpectedStop, stopReason, needsPermission, lastFailure, failureKind,
     * skippedPackages, blackholeUntilMs); absent keys mean the default, unknown keys are ignored.
     * See docs/ARCHITECTURE.md, "Статус службы для интерфейса".
     */
    void onStatus(int state, String error, String detail);
}
