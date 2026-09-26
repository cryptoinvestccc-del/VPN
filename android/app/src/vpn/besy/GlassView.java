package vpn.besy;

import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.Path;
import android.graphics.RadialGradient;
import android.graphics.RectF;
import android.graphics.Shader;
import android.graphics.LinearGradient;
import android.view.MotionEvent;
import android.view.View;

/**
 * The whole screen, drawn by hand: a chrome circle with the logo in it,
 * a lamp under it that says whether the tunnel is up, and the gear.
 *
 * <p>There is no Compose and no AndroidX here: those live on a Maven
 * host this build cannot reach, so every surface is a shape on a Canvas.
 * That turns out to suit the design, which is mostly gradients and one
 * button, and it keeps the APK in the tens of kilobytes.
 *
 * <p>The order of drawing is the design: the light source is painted
 * first, the glass over it. Reversing those two is what makes a frosted
 * surface look like a flat translucent panel instead.
 */
final class GlassView extends View {

    interface OnPowerTap { void onPowerTap(); }
    interface OnSettingsTap { void onSettingsTap(); }

    static final int STATE_OFF  = 0;
    static final int STATE_BUSY = 1;
    static final int STATE_LIVE = 2;

    private final Paint fill   = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint stroke = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint text   = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final RectF rect   = new RectF();
    private final Path  path   = new Path();
    private final Paint picture = new Paint(Paint.ANTI_ALIAS_FLAG | Paint.FILTER_BITMAP_FLAG);
    private android.graphics.Bitmap logo;

    private int state = STATE_OFF;
    private String error;
    private float spin;          // the waiting ring's angle
    private OnPowerTap listener;
    private OnSettingsTap settingsListener;

    private float buttonCx, buttonCy, buttonR;
    private float gearCx, gearCy;

    // What the rows under the button show. Set from the activity.
    private String address, handshake, server;

    GlassView(Context c) {
        super(c);
        setClickable(true);
        stroke.setStyle(Paint.Style.STROKE);
        text.setTextAlign(Paint.Align.LEFT);
    }

    void setOnPowerTap(OnPowerTap l) { listener = l; }
    void setOnSettingsTap(OnSettingsTap l) { settingsListener = l; }

    /** The tunnel's address and how long ago the server last answered. */
    void setInfo(String address, String handshake) {
        if (!eq(address, this.address) || !eq(handshake, this.handshake)) {
            this.address = address;
            this.handshake = handshake;
            invalidate();
        }
    }

    /** One line about the server, shown while disconnected. */
    void setServer(String line) {
        if (!eq(line, server)) { server = line; invalidate(); }
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }

    void setState(int s) {
        if (state != s) {
            state = s;
            invalidate();
            // The power button's label follows the state.
            sendAccessibilityEvent(
                    android.view.accessibility.AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED);
        }
    }

    int getState() { return state; }

    /**
     * Shows why the last attempt failed, until the next one.
     *
     * <p>It was a toast before, which is the wrong shape for this: the
     * message appears for a few seconds and is gone, and a failure that
     * happens while the phone is in somebody's pocket leaves nothing
     * behind. A screen with one button has room to say what went wrong.
     */
    void setError(String message) {
        if (message == null ? error != null : !message.equals(error)) {
            error = message;
            invalidate();
        }
    }

    private float dp(float v) { return v * getResources().getDisplayMetrics().density; }

    @Override protected void onDraw(Canvas canvas) {
        final float w = getWidth(), h = getHeight();
        if (w <= 0 || h <= 0) return;

        buttonCx = w / 2f;
        buttonCy = h * 0.40f;
        buttonR  = Math.min(w * 0.38f, h * 0.24f);

        drawSky(canvas, w, h);
        drawButton(canvas);
        drawLamp(canvas);
        drawGear(canvas, w);
        drawReadout(canvas, w, h);

        if (state == STATE_BUSY) {
            spin += 6f;
            if (spin >= 360f) spin -= 360f;
            invalidate();
        }
    }

    /** Black, with a faint light behind the circle. */
    private void drawSky(Canvas canvas, float w, float h) {
        fill.setShader(null);
        fill.setColor(Palette.VOID_);
        canvas.drawRect(0, 0, w, h, fill);

        fill.setShader(new RadialGradient(buttonCx, buttonCy, buttonR * 2.2f,
                state == STATE_LIVE ? 0x30FFFFFF : 0x18FFFFFF, 0x00FFFFFF, Shader.TileMode.CLAMP));
        canvas.drawRect(0, 0, w, h, fill);
        fill.setShader(null);
    }

    /**
     * The one control: a chrome ring with the logo inside.
     *
     * <p>The logo's own background is pure black, so the disc under it is
     * black too and the picture sits in it without a visible edge.
     */
    private void drawButton(Canvas canvas) {
        final boolean live = state == STATE_LIVE;
        final float ring = dp(7);

        // chrome ring
        fill.setShader(new android.graphics.SweepGradient(buttonCx, buttonCy,
                new int[] { 0xFFFFFFFF, 0xFF6D7078, 0xFFE9EBEF, 0xFF3B3D43, 0xFFFFFFFF, 0xFF8B8E96, 0xFFFFFFFF },
                null));
        canvas.drawCircle(buttonCx, buttonCy, buttonR, fill);
        fill.setShader(null);
        fill.setColor(0xFF000000);
        canvas.drawCircle(buttonCx, buttonCy, buttonR - ring, fill);

        // the logo, grey and dim while the tunnel is down
        if (logo == null) {
            logo = android.graphics.BitmapFactory.decodeResource(getResources(), R.drawable.logo);
        }
        if (logo != null) {
            final float lw = (buttonR - ring) * 1.78f;
            final float lh = lw * logo.getHeight() / logo.getWidth();
            rect.set(buttonCx - lw / 2f, buttonCy - lh / 2f, buttonCx + lw / 2f, buttonCy + lh / 2f);
            if (live) {
                picture.setColorFilter(null);
                picture.setAlpha(255);
            } else {
                android.graphics.ColorMatrix grey = new android.graphics.ColorMatrix();
                grey.setSaturation(0f);
                picture.setColorFilter(new android.graphics.ColorMatrixColorFilter(grey));
                picture.setAlpha(state == STATE_BUSY ? 190 : 120);
            }
            canvas.save();
            path.reset();
            path.addCircle(buttonCx, buttonCy, buttonR - ring, Path.Direction.CW);
            canvas.clipPath(path);
            canvas.drawBitmap(logo, null, rect, picture);
            canvas.restore();
        }

        if (state == STATE_BUSY) {
            stroke.setShader(null);
            stroke.setColor(0xFFFFFFFF);
            stroke.setStrokeWidth(ring * 0.6f);
            stroke.setStrokeCap(Paint.Cap.ROUND);
            final float rr = buttonR - ring / 2f;
            rect.set(buttonCx - rr, buttonCy - rr, buttonCx + rr, buttonCy + rr);
            canvas.drawArc(rect, spin, 60f, false, stroke);
            stroke.setStrokeCap(Paint.Cap.BUTT);
        }
    }

    /**
     * The small lamp under the circle: green when the tunnel is up, red
     * when it is down, amber and breathing while it is being set up.
     */
    private void drawLamp(Canvas canvas) {
        final float cx = buttonCx, cy = buttonCy + buttonR + dp(30), r = dp(7);
        int color;
        float glow = 1f;
        switch (state) {
            case STATE_LIVE: color = Palette.LAMP_ON; break;
            case STATE_BUSY:
                color = Palette.LAMP_WAIT;
                glow = 0.55f + 0.45f * (float) Math.abs(Math.sin(Math.toRadians(spin * 2)));
                break;
            default: color = Palette.LAMP_OFF; break;
        }
        final int halo = ((int) (0x66 * glow) << 24) | (color & 0x00FFFFFF);
        fill.setShader(new RadialGradient(cx, cy, r * 3.2f, halo, color & 0x00FFFFFF, Shader.TileMode.CLAMP));
        canvas.drawCircle(cx, cy, r * 3.2f, fill);
        fill.setShader(null);
        fill.setColor(color);
        canvas.drawCircle(cx, cy, r, fill);
        fill.setColor(0x66FFFFFF);
        canvas.drawCircle(cx - r * 0.3f, cy - r * 0.3f, r * 0.3f, fill);
    }

    private void drawReadout(Canvas canvas, float w, float h) {
        final String title, sub;
        switch (state) {
            case STATE_LIVE:
                title = str(R.string.live_title);
                sub = handshake != null ? str(R.string.row_handshake) + ": " + handshake : "";
                break;
            case STATE_BUSY: title = str(R.string.busy_title); sub = TunnelService.stage; break;
            default:         title = str(R.string.idle_title); sub = server != null ? server : str(R.string.idle_sub); break;
        }

        float y = buttonCy + buttonR + dp(72);
        text.setTextAlign(Paint.Align.CENTER);
        text.setColor(Palette.INK);
        text.setTextSize(dp(20));
        canvas.drawText(title, w / 2f, y, text);
        if (sub != null && sub.length() > 0) {
            text.setColor(Palette.INK_3);
            text.setTextSize(dp(13));
            canvas.drawText(sub, w / 2f, y + dp(24), text);
        }

        // The navigation bar is drawn over this view on phones that use
        // gesture navigation — a Galaxy A13 in Test Lab put its back
        // arrow across the last line — so everything anchored to the
        // bottom is measured from above it.
        android.view.WindowInsets insets = getRootWindowInsets();
        final float bottom = h - (insets != null ? insets.getSystemWindowInsetBottom() : 0);

        if (error != null && error.length() > 0) {
            text.setColor(Palette.EMBER);
            text.setTextSize(dp(12.5f));
            java.util.List<String> lines = wrap(error, w - dp(44), text);
            float ey = bottom - dp(24) - (lines.size() - 1) * dp(16);
            for (String piece : lines) {
                canvas.drawText(piece, w / 2f, ey, text);
                ey += dp(16);
            }
        } else {
            text.setColor(Palette.INK_3);
            text.setTextSize(dp(11.5f));
            canvas.drawText(str(R.string.key_local), w / 2f, bottom - dp(24), text);
        }
        text.setTextAlign(Paint.Align.LEFT);
    }

    /**
     * Settings, top right, below the status bar.
     *
     * <p>Drawn as a lobed wheel with a hole: eight teeth with flat tops.
     * The first version of the mockup drew rays around a circle and was
     * read as a sun, so the teeth are wide and short.
     */
    private void drawGear(Canvas canvas, float w) {
        int top = (int) dp(24);
        android.view.WindowInsets insets = getRootWindowInsets();
        if (insets != null) top = insets.getSystemWindowInsetTop();

        gearCx = w - dp(34);
        gearCy = top + dp(30);
        final float outer = dp(11), inner = dp(8.2f);

        path.reset();
        for (int i = 0; i < 8; i++) {
            double a = Math.toRadians(i * 45);
            double[] offs = { -16, -9, 9, 16 };
            float[] radii = { inner, outer, outer, inner };
            for (int k = 0; k < 4; k++) {
                double t = a + Math.toRadians(offs[k]);
                float x = gearCx + (float) (radii[k] * Math.cos(t));
                float y = gearCy + (float) (radii[k] * Math.sin(t));
                if (i == 0 && k == 0) path.moveTo(x, y); else path.lineTo(x, y);
            }
        }
        path.close();

        stroke.setColor(Palette.INK_2);
        stroke.setStrokeWidth(dp(1.5f));
        stroke.setStrokeJoin(Paint.Join.ROUND);
        canvas.drawPath(path, stroke);
        canvas.drawCircle(gearCx, gearCy, dp(3.2f), stroke);
        stroke.setStrokeJoin(Paint.Join.MITER);
    }

    private String str(int id) { return getContext().getString(id); }

    /**
     * Breaks a message into lines that fit.
     *
     * <p>Clipped text is worse than no text: it stops exactly where the
     * useful part of an error usually begins.
     */
    private static java.util.List<String> wrap(String message, float width, Paint paint) {
        java.util.List<String> lines = new java.util.ArrayList<String>();
        StringBuilder line = new StringBuilder();

        for (String word : message.split("\\s+")) {
            String candidate = line.length() == 0 ? word : line + " " + word;
            if (paint.measureText(candidate) <= width || line.length() == 0) {
                line.setLength(0);
                line.append(candidate);
            } else {
                lines.add(line.toString());
                line.setLength(0);
                line.append(word);
            }
            if (lines.size() >= 3) break;
        }
        if (line.length() > 0 && lines.size() < 3) {
            lines.add(line.toString());
        }
        return lines;
    }

    // ---- Accessibility ------------------------------------------------
    //
    // The screen is drawn, not built from widgets, so without this it is
    // a blank picture to anything that reads the interface: TalkBack said
    // nothing, and Firebase's crawler reported "outside of app" and never
    // found the button or the gear. Each control is described as a
    // virtual node with its bounds, a label and a click action.

    private static final int NODE_POWER = 1;
    private static final int NODE_GEAR  = 2;

    private final android.view.accessibility.AccessibilityNodeProvider nodes =
            new android.view.accessibility.AccessibilityNodeProvider() {
        @Override public android.view.accessibility.AccessibilityNodeInfo createAccessibilityNodeInfo(int id) {
            android.view.accessibility.AccessibilityNodeInfo info;
            if (id == View.NO_ID) {
                info = android.view.accessibility.AccessibilityNodeInfo.obtain(GlassView.this);
                onInitializeAccessibilityNodeInfo(info);
                info.addChild(GlassView.this, NODE_POWER);
                info.addChild(GlassView.this, NODE_GEAR);
                return info;
            }
            if (id != NODE_POWER && id != NODE_GEAR) return null;

            info = android.view.accessibility.AccessibilityNodeInfo.obtain(GlassView.this, id);
            info.setPackageName(getContext().getPackageName());
            info.setClassName("android.widget.Button");
            info.setParent(GlassView.this);
            info.setContentDescription(label(id));
            android.graphics.Rect r = bounds(id);
            info.setBoundsInParent(r);
            int[] at = new int[2];
            getLocationOnScreen(at);
            r.offset(at[0], at[1]);
            info.setBoundsInScreen(r);
            info.setEnabled(true);
            info.setClickable(true);
            info.setFocusable(true);
            info.setVisibleToUser(true);
            info.addAction(android.view.accessibility.AccessibilityNodeInfo.ACTION_CLICK);
            return info;
        }

        @Override public boolean performAction(int id, int action, android.os.Bundle args) {
            if (action != android.view.accessibility.AccessibilityNodeInfo.ACTION_CLICK) return false;
            if (id == NODE_POWER && listener != null) { listener.onPowerTap(); return true; }
            if (id == NODE_GEAR && settingsListener != null) { settingsListener.onSettingsTap(); return true; }
            return false;
        }
    };

    @Override public android.view.accessibility.AccessibilityNodeProvider getAccessibilityNodeProvider() {
        return nodes;
    }

    private String label(int id) {
        if (id == NODE_GEAR) return str(R.string.settings);
        switch (state) {
            case STATE_LIVE: return str(R.string.disconnect);
            case STATE_BUSY: return str(R.string.wait);
            default:         return str(R.string.connect);
        }
    }

    private android.graphics.Rect bounds(int id) {
        float cx = id == NODE_GEAR ? gearCx : buttonCx;
        float cy = id == NODE_GEAR ? gearCy : buttonCy;
        float r  = id == NODE_GEAR ? dp(24) : Math.max(buttonR, dp(24));
        return new android.graphics.Rect(Math.round(cx - r), Math.round(cy - r),
                Math.round(cx + r), Math.round(cy + r));
    }

    @Override public boolean onTouchEvent(MotionEvent e) {
        if (e.getAction() == MotionEvent.ACTION_UP) {
            // The gear's target is 48dp across, well past what is drawn.
            final float gx = e.getX() - gearCx, gy = e.getY() - gearCy;
            if (gx * gx + gy * gy <= dp(26) * dp(26)) {
                performClick();
                if (settingsListener != null) settingsListener.onSettingsTap();
                return true;
            }
            final float dx = e.getX() - buttonCx, dy = e.getY() - buttonCy;
            // The hit area is grown past the drawn circle: 48dp is what a
            // finger needs, whatever the design wants to look like.
            final float reach = Math.max(buttonR, dp(24));
            if (dx * dx + dy * dy <= reach * reach) {
                performClick();
                if (listener != null) listener.onPowerTap();
                return true;
            }
        }
        return super.onTouchEvent(e);
    }

    @Override public boolean performClick() { return super.performClick(); }
}
