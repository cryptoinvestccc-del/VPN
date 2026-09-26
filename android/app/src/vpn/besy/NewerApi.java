package vpn.besy;

import android.graphics.Typeface;
import android.view.Window;

/**
 * The only calls into Android newer than 8.0 (API 26), the app's minimum.
 *
 * <p>build.sh compiles the whole app again against Android 8 to catch any
 * call a phone that old does not have. A call behind a version check still
 * fails that check, because javac cannot see the check; so such calls live
 * here, each behind its own version test, and this one file is compiled
 * against the current platform. Every caller tests the version first.
 */
final class NewerApi {
    private NewerApi() {}

    /** Android 9 (API 28): a typeface at any weight. */
    static Typeface weight(Typeface base, int weight) {
        return Typeface.create(base, weight, false);
    }

    /** Android 11 (API 30): dark icons in the status and navigation bars. */
    static void lightSystemBars(Window w) {
        final int light = android.view.WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
                | android.view.WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
        w.getInsetsController().setSystemBarsAppearance(light, light);
    }
}
