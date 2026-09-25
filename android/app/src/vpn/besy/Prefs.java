package vpn.besy;

import android.content.Context;
import android.content.SharedPreferences;

/**
 * The person's choices, kept apart from the key.
 *
 * <p>A separate file on purpose: "delete key and data" clears the key's
 * file and leaves these, because a language and a switch are settings,
 * not data about anyone.
 */
final class Prefs {

    private static final String FILE = "settings";
    static final String LANG_SYSTEM = "system";

    private Prefs() {}

    private static SharedPreferences p(Context c) {
        return c.getSharedPreferences(FILE, Context.MODE_PRIVATE);
    }

    static String language(Context c) { return p(c).getString("language", LANG_SYSTEM); }

    static void setLanguage(Context c, String lang) {
        p(c).edit().putString("language", lang).commit();
    }

    static boolean autoConnect(Context c) { return p(c).getBoolean("auto_connect", false); }

    static void setAutoConnect(Context c, boolean on) {
        p(c).edit().putBoolean("auto_connect", on).commit();
    }
}
