// SPDX-License-Identifier: GPL-3.0-only
package lavray.core.mobile;

/** Compile-time stand-in for the gomobile interface NetworkListener (see Mobile.java). */
public interface NetworkListener {
    /** name "" and index -1 mean no network. */
    void update(String name, int index, boolean metered);
}
