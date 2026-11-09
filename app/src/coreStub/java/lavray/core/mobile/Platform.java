// SPDX-License-Identifier: GPL-3.0-only
package lavray.core.mobile;

/** Compile-time stand-in for the gomobile interface Platform (see Mobile.java). */
public interface Platform {
    int openTun(String optionsJSON) throws Exception;

    void protect(int fd) throws Exception;

    int findOwnerUid(int proto, String srcIP, int srcPort, String dstIP, int dstPort) throws Exception;

    String packagesForUid(int uid);

    void startNetworkMonitor(NetworkListener l) throws Exception;

    void closeNetworkMonitor() throws Exception;

    String interfaces() throws Exception;
}
