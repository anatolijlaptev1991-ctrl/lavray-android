// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ipc;

oneway interface IVpnObserver {
    /** state is VpnState.code; error is non-null only for ERROR. */
    void onStatus(int state, String error);
}
