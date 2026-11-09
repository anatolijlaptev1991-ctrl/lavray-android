// SPDX-License-Identifier: GPL-3.0-only
package lavray.core.mobile;

/** Compile-time stand-in for the gomobile struct Options (see Mobile.java). */
public final class Options {
    private String dataDir = "";
    private String tempDir = "";
    private String socketPath = "";
    private String authToken = "";
    private int memoryLimitMB;
    private String timeZone = "";

    public Options() {}

    public String getDataDir() { return dataDir; }
    public void setDataDir(String v) { dataDir = v; }
    public String getTempDir() { return tempDir; }
    public void setTempDir(String v) { tempDir = v; }
    public String getSocketPath() { return socketPath; }
    public void setSocketPath(String v) { socketPath = v; }
    public String getAuthToken() { return authToken; }
    public void setAuthToken(String v) { authToken = v; }
    public String getTimeZone() { return timeZone; }
    public void setTimeZone(String v) { timeZone = v; }
    public int getMemoryLimitMB() { return memoryLimitMB; }
    public void setMemoryLimitMB(int v) { memoryLimitMB = v; }
}
