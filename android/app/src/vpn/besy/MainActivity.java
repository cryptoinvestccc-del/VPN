package vpn.besy;

import android.app.Activity;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.View;
import android.view.WindowManager;

/**
 * The whole app, as far as a person sees it: one screen with one button.
 *
 * <p>The connection itself is not wired up yet — this build draws the
 * screen and runs its states so the design can be seen on a real device.
 * What it does not do, it does not pretend to do: nothing here claims a
 * tunnel exists.
 */
public final class MainActivity extends Activity implements GlassView.OnPowerTap {

    private GlassView view;
    private final Handler handler = new Handler(Looper.getMainLooper());

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

    @Override public void onPowerTap() {
        if (view.getState() == GlassView.STATE_OFF) {
            view.setState(GlassView.STATE_BUSY);
            handler.postDelayed(new Runnable() {
                @Override public void run() { view.setState(GlassView.STATE_LIVE); }
            }, 1800);
        } else {
            handler.removeCallbacksAndMessages(null);
            view.setState(GlassView.STATE_OFF);
        }
    }

    @Override protected void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        super.onDestroy();
    }
}
