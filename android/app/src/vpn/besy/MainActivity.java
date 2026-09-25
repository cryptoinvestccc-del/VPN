package vpn.besy;

import android.app.Activity;
import android.content.Intent;
import android.net.VpnService;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.View;
import android.view.WindowManager;

/**
 * The whole app, as far as a person sees it: one screen, one button.
 *
 * <p>The activity owns no tunnel state of its own. It asks the service
 * to start or stop and shows what the service reports, so what is on the
 * screen is what is actually happening rather than what was requested.
 */
public final class MainActivity extends Activity
        implements GlassView.OnPowerTap, GlassView.OnSettingsTap {

    private static final int REQUEST_VPN_PERMISSION = 1;
    private static final long POLL_MS = 400;

    private GlassView view;
    private final Handler handler = new Handler(Looper.getMainLooper());

    /** The language this screen was built in; a change means rebuild. */
    private String builtIn;

    @Override protected void attachBaseContext(android.content.Context base) {
        super.attachBaseContext(Lang.wrap(base));
    }

    private final Runnable poll = new Runnable() {
        @Override public void run() {
            view.setState(TunnelService.state);
            // Kept on the screen rather than flashed past: a failure that
            // happens while the phone is in a pocket has to still be
            // there when somebody looks.
            view.setError(TunnelService.lastError);
            view.setInfo(TunnelService.address, handshakeAge(TunnelService.lastHandshake));
            handler.postDelayed(this, POLL_MS);
        }
    };

    @Override protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        // Under the status bar, which is made transparent, so the light
        // reaches the top edge. Not under the navigation bar: that used
        // FLAG_LAYOUT_NO_LIMITS once, which also made Android report no
        // bottom inset at all, and on a Galaxy A13 the gesture bar's back
        // arrow sat across the last line of text. The navigation bar gets
        // the background's own colour instead.
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_DRAWS_SYSTEM_BAR_BACKGROUNDS);
        getWindow().setStatusBarColor(android.graphics.Color.TRANSPARENT);
        getWindow().setNavigationBarColor(Palette.VOID_);

        builtIn = Prefs.language(this);
        view = new GlassView(this);
        view.setOnPowerTap(this);
        view.setOnSettingsTap(this);
        view.setSystemUiVisibility(View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN);
        setContentView(view);

        // Only on a fresh start, and only when the system would not have
        // to ask: a permission dialog appearing unbidden on launch is
        // not what "connect on launch" means.
        if (saved == null && Prefs.autoConnect(this)
                && TunnelService.state == GlassView.STATE_OFF
                && VpnService.prepare(this) == null) {
            send(TunnelService.ACTION_CONNECT);
        }
    }

    @Override public void onSettingsTap() {
        startActivity(new Intent(this, SettingsActivity.class));
    }

    /** "12 s ago", "3 min ago", or null before the first handshake. */
    private String handshakeAge(long at) {
        if (at <= 0) return null;
        long age = Math.max(0, System.currentTimeMillis() / 1000 - at);
        return age < 120
                ? getString(R.string.handshake_sec, age)
                : getString(R.string.handshake_min, age / 60);
    }

    /** Asks the server how busy it is, off the main thread. */
    private void refreshServer() {
        final String endpoint = Provisioning.endpoint(this);
        new Thread(new Runnable() {
            @Override public void run() {
                final int n = Provisioning.connected(endpoint);
                handler.post(new Runnable() {
                    @Override public void run() {
                        if (isFinishing()) return;
                        view.setServer(n >= 0
                                ? getString(R.string.server_ok, n)
                                : getString(R.string.server_unreachable));
                    }
                });
            }
        }, "besy-status").start();
    }

    @Override protected void onResume() {
        super.onResume();
        if (!Prefs.language(this).equals(builtIn)) {
            recreate();
            return;
        }
        handler.post(poll);
        refreshServer();
    }

    @Override protected void onPause() {
        handler.removeCallbacks(poll);
        super.onPause();
    }

    @Override public void onPowerTap() {
        if (TunnelService.state == GlassView.STATE_OFF) {
            // The system asks the person to allow a VPN the first time,
            // and only the first time.
            Intent consent = VpnService.prepare(this);
            if (consent != null) {
                startActivityForResult(consent, REQUEST_VPN_PERMISSION);
                return;
            }
            send(TunnelService.ACTION_CONNECT);
        } else {
            send(TunnelService.ACTION_DISCONNECT);
        }
    }

    @Override protected void onActivityResult(int request, int result, Intent data) {
        super.onActivityResult(request, result, data);
        if (request == REQUEST_VPN_PERMISSION && result == RESULT_OK) {
            send(TunnelService.ACTION_CONNECT);
        }
    }

    private void send(String action) {
        if (TunnelService.ACTION_CONNECT.equals(action)) {
            TunnelService.lastError = null;
            view.setError(null);
        }
        Intent i = new Intent(this, TunnelService.class).setAction(action);
        startService(i);
        view.setState(TunnelService.ACTION_CONNECT.equals(action)
                ? GlassView.STATE_BUSY : GlassView.STATE_OFF);
    }

    @Override protected void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        super.onDestroy();
    }
}
