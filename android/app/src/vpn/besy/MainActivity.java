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
public final class MainActivity extends Activity implements GlassView.OnPowerTap {

    private static final int REQUEST_VPN_PERMISSION = 1;
    private static final long POLL_MS = 400;

    private GlassView view;
    private final Handler handler = new Handler(Looper.getMainLooper());

    private final Runnable poll = new Runnable() {
        @Override public void run() {
            view.setState(TunnelService.state);
            // Kept on the screen rather than flashed past: a failure that
            // happens while the phone is in a pocket has to still be
            // there when somebody looks.
            view.setError(TunnelService.lastError);
            handler.postDelayed(this, POLL_MS);
        }
    };

    @Override protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        getWindow().setFlags(
                WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
                WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS);

        view = new GlassView(this);
        view.setOnPowerTap(this);
        view.setSystemUiVisibility(View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN);
        setContentView(view);
    }

    @Override protected void onResume() {
        super.onResume();
        handler.post(poll);
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
