// SPDX-License-Identifier: GPL-3.0-only
package lavray.core.mobile;

/**
 * Compile-time stand-in for the gomobile class of the same name. Used only while
 * app/libs/lavray-core.aar is absent; the build drops this source set as soon as
 * the AAR exists. Method shapes follow what gomobile generates for package
 * "mobile" with -javapkg=lavray.core.
 */
public final class Mobile {
    private Mobile() {}

    public static void start(Options o, Platform p) throws Exception {
        throw new Exception("lavray-core.aar is not present: core stub in use");
    }

    public static void stop() {}

    public static String version() {
        return "stub";
    }
}
