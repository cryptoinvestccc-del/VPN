package vpn.besy;

import android.content.Intent;
import android.net.VpnService;

/**
 * Declared and empty on purpose.
 *
 * <p>The manifest entry decides what permissions the app asks for and
 * what Google Play is told about it, so it is worth having from the
 * first build rather than appearing late and changing the app's shape.
 * The tunnel it will drive — amneziawg-go, shipped in the APK and handed
 * the descriptor from {@link VpnService.Builder#establish()} — is the
 * next piece of work, and until it exists this service starts nothing.
 */
public final class TunnelService extends VpnService {
    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        return START_NOT_STICKY;
    }
}
