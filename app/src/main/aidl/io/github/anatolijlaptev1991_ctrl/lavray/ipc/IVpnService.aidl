// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ipc;

import android.os.Bundle;
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnObserver;

/**
 * Binder of LavrayVpnService (process :vpn) for the UI process. Call from a background thread.
 * New methods are appended at the end: the order of the methods is the order of the transaction codes.
 */
interface IVpnService {
    /**
     * Starts the core if needed and blocks until its API answers. Returns a Bundle with
     * "path" (unix socket) and "token" (bearer). Throws IllegalStateException if the core fails to start.
     */
    Bundle endpoint();

    /** profileId <= 0 connects the last server. The service promotes itself to foreground. */
    void connect(long profileId);

    void disconnect();

    /** Delivers the current status immediately, then every change. */
    void registerObserver(IVpnObserver observer);

    void unregisterObserver(IVpnObserver observer);

    boolean isAutoConnectEnabled();

    /**
     * Writes the "connect at phone start" setting and aligns the boot receiver. Returns false when the setting did not
     * reach storage or the receiver could not be switched on (the stored value is then the old one): the switch must
     * show the old value.
     */
    boolean setAutoConnectEnabled(boolean enabled);

    /** Per-app choice as JSON {"mode":"all|only|except","packages":[...]}; applies the next time the tunnel is built. */
    String getAppSelection();

    void setAppSelection(String json);

    /**
     * Rebuilds the tunnel (server switch, new settings) for the same or another profile without a direct-traffic gap:
     * the interface stays blocked, not open, until the new one replaces it, for at most 30 seconds. profileId <= 0
     * reconnects the last server. Failure is reported through the status (state ERROR, failureKind/failureReason).
     * Use this instead of disconnect() followed by connect(), which hands every app the direct connection in between.
     */
    void reconnect(long profileId);

    /**
     * Keeps the :vpn process (and the core inside it) alive while a long operation that runs in the core is under way,
     * for example a backup restore, even if the UI unbinds. token names the hold; timeoutMs is a hard cap (clamped to
     * 15 minutes). Call endBusy with the same token when done; a hold the UI forgets ends by itself at its deadline.
     * Returns true when the hold is in force; false when the system refused to keep the service in the foreground
     * (the hold was dropped and nothing keeps the process alive). Callers that ignore the result keep working.
     */
    boolean beginBusy(String token, long timeoutMs);

    void endBusy(String token);
}
