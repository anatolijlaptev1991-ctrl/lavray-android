// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ipc;

import android.os.Bundle;
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnObserver;

/** Binder of LavrayVpnService (process :vpn) for the UI process. Call from a background thread. */
interface IVpnService {
    /**
     * Starts the core if needed and blocks until its API answers. Returns a Bundle with
     * "path" (unix socket) and "token" (bearer). Throws IllegalStateException if the core fails to start.
     */
    Bundle endpoint();

    /** profileId <= 0 connects the last server. The service promotes itself to foreground. */
    void connect(long profileId);

    void disconnect();

    /**
     * Rebuilds the tunnel (server switch, new settings): the interface stays blocked, not open, until the new one
     * replaces it, for at most 30 seconds. profileId <= 0 reconnects the last server. Use this instead of
     * disconnect() followed by connect(), which hands every app the direct connection in between.
     */
    void reconnect(long profileId);

    /** Delivers the current status immediately, then every change. */
    void registerObserver(IVpnObserver observer);

    void unregisterObserver(IVpnObserver observer);

    boolean isAutoConnectEnabled();

    void setAutoConnectEnabled(boolean enabled);

    /** Per-app choice as JSON {"mode":"all|only|except","packages":[...]}; applies the next time the tunnel is built. */
    String getAppSelection();

    void setAppSelection(String json);
}
