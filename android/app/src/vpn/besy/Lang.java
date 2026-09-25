package vpn.besy;

import android.content.Context;
import android.content.res.Configuration;

import java.util.Locale;

/**
 * Applies the language chosen in the app, independently of the phone's.
 *
 * <p>Every screen and the tunnel service wrap their context with this,
 * so a choice made in settings reaches the notification and the error
 * messages as well as the menus. This works the same on every Android
 * the app supports, which is why it is used instead of the per-app
 * language API that only exists from Android 13.
 */
final class Lang {

    private Lang() {}

    static Context wrap(Context base) {
        String lang = Prefs.language(base);
        if (Prefs.LANG_SYSTEM.equals(lang)) return base;
        Locale locale = new Locale(lang);
        Configuration config = new Configuration(base.getResources().getConfiguration());
        config.setLocale(locale);
        return base.createConfigurationContext(config);
    }
}
