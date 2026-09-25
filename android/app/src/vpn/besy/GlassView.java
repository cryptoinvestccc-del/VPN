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
 * The whole screen, drawn by hand.
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
        buttonCy = h * 0.42f;
        buttonR  = Math.min(w, h) * 0.22f;

        drawSky(canvas, w, h);
        drawCore(canvas, w, h);
        drawDrift(canvas, w, h);
        drawButton(canvas);
        drawGear(canvas, w);
        drawRows(canvas, w);
        drawReadout(canvas, w, h);

        if (state == STATE_BUSY) {
            spin += 6f;
            if (spin >= 360f) spin -= 360f;
            invalidate();
        }
    }

    /** The night the glass sits in front of. */
    private void drawSky(Canvas canvas, float w, float h) {
        fill.setShader(null);
        fill.setColor(Palette.VOID_);
        canvas.drawRect(0, 0, w, h, fill);

        fill.setShader(new RadialGradient(
                w * 0.52f, h * 0.30f, Math.max(w, h) * 0.62f,
                new int[] { 0xB878A8CD, 0x5523394D, 0x000A0F15 },
                new float[] { 0f, 0.55f, 1f }, Shader.TileMode.CLAMP));
        canvas.drawRect(0, 0, w, h, fill);

        // The warm note, kept small and off-centre.
        fill.setShader(new RadialGradient(
                w * 0.72f, h * 0.24f, w * 0.42f,
                0x66C9A88B, 0x00C9A88B, Shader.TileMode.CLAMP));
        canvas.drawRect(0, 0, w, h, fill);

        fill.setShader(new LinearGradient(
                0, h * 0.44f, 0, h,
                new int[] { 0x00182E42, 0xE8182E42, 0xFF0A0F15 },
                new float[] { 0f, 0.5f, 1f }, Shader.TileMode.CLAMP));
        canvas.drawRect(0, h * 0.44f, w, h, fill);
        fill.setShader(null);
    }

    /** The source itself: a small hard centre inside a wide bloom. */
    private void drawCore(Canvas canvas, float w, float h) {
        final float cx = w / 2f, cy = h * 0.36f;
        final int glow = state == STATE_LIVE ? 0x706FE3C0 : 0x70BEDEFF;

        fill.setShader(new RadialGradient(cx, cy, dp(90),
                glow, glow & 0x00FFFFFF, Shader.TileMode.CLAMP));
        canvas.drawCircle(cx, cy, dp(90), fill);

        fill.setShader(null);
        fill.setColor(state == STATE_LIVE ? 0xD9D7FFF3 : 0xD1FFFFFF);
        canvas.drawCircle(cx, cy, dp(4.5f), fill);
    }

    /** Label-free glass. Atmosphere, never a control. */
    private void drawDrift(Canvas canvas, float w, float h) {
        drawBlob(canvas, w * 0.10f, h * 0.52f, w * 0.21f, 0x0BFFFFFF, 0x17FFFFFF);
        drawBlob(canvas, w * 0.88f, h * 0.30f, w * 0.23f, 0x0BFFFFFF, 0x17FFFFFF);
    }

    private void drawBlob(Canvas canvas, float cx, float cy, float r, int body, int rim) {
        fill.setShader(null);
        fill.setColor(body);
        rect.set(cx - r, cy - r * 0.86f, cx + r, cy + r * 0.86f);
        canvas.drawOval(rect, fill);

        stroke.setColor(rim);
        stroke.setStrokeWidth(dp(1));
        canvas.drawOval(rect, stroke);
    }

    /** The one control. */
    private void drawButton(Canvas canvas) {
        final boolean live = state == STATE_LIVE;

        if (live) {
            fill.setShader(new RadialGradient(buttonCx, buttonCy, buttonR * 1.9f,
                    0x446FE3C0, 0x006FE3C0, Shader.TileMode.CLAMP));
            canvas.drawCircle(buttonCx, buttonCy, buttonR * 1.9f, fill);
            fill.setShader(null);
        }

        fill.setColor(live ? 0x226FE3C0 : Palette.GLASS);
        canvas.drawCircle(buttonCx, buttonCy, buttonR, fill);

        stroke.setColor(live ? 0xAE6FE3C0 : Palette.RIM);
        stroke.setStrokeWidth(dp(1));
        canvas.drawCircle(buttonCx, buttonCy, buttonR, stroke);

        drawPowerGlyph(canvas, live);

        if (state == STATE_BUSY) {
            stroke.setColor(Palette.FLARE);
            stroke.setStrokeWidth(dp(1.4f));
            stroke.setStrokeCap(Paint.Cap.ROUND);
            final float rr = buttonR + dp(10);
            rect.set(buttonCx - rr, buttonCy - rr, buttonCx + rr, buttonCy + rr);
            canvas.drawArc(rect, spin, 46f, false, stroke);
            stroke.setStrokeCap(Paint.Cap.BUTT);
        }
    }

    private void drawPowerGlyph(Canvas canvas, boolean live) {
        final float r = buttonR * 0.30f;
        stroke.setColor(live ? Palette.LIVE : Palette.INK);
        stroke.setStrokeWidth(dp(1.6f));
        stroke.setStrokeCap(Paint.Cap.ROUND);

        rect.set(buttonCx - r, buttonCy - r * 0.8f, buttonCx + r, buttonCy + r * 1.2f);
        canvas.drawArc(rect, -60f, 300f, false, stroke);

        path.reset();
        path.moveTo(buttonCx, buttonCy - r * 1.35f);
        path.lineTo(buttonCx, buttonCy + r * 0.05f);
        canvas.drawPath(path, stroke);
        stroke.setStrokeCap(Paint.Cap.BUTT);
    }

    private void drawReadout(Canvas canvas, float w, float h) {
        final String title, sub;
        switch (state) {
            case STATE_LIVE: title = str(R.string.live_title); sub = ""; break;
            case STATE_BUSY: title = str(R.string.busy_title); sub = TunnelService.stage; break;
            default:         title = str(R.string.idle_title); sub = str(R.string.idle_sub); break;
        }

        // The navigation bar is drawn over this view on phones that use
        // gesture navigation — a Galaxy A13 in Test Lab put its back
        // arrow across the last line — so everything anchored to the
        // bottom is measured from above it.
        android.view.WindowInsets insets = getRootWindowInsets();
        final float bottom = insets != null ? insets.getSystemWindowInsetBottom() : 0;
        h -= bottom;

        final float x = dp(22);
        float y = h - (error != null && error.length() > 0 ? dp(118) : dp(92));

        text.setColor(Palette.INK);
        text.setTextSize(dp(30));
        canvas.drawText(title, x, y, text);

        if (sub.length() > 0) {
            y += dp(32);
            text.setColor(Palette.INK_3);
            text.setTextSize(dp(26));
            canvas.drawText(sub, x, y, text);
        }

        if (error != null && error.length() > 0) {
            text.setColor(Palette.EMBER);
            text.setTextSize(dp(12.5f));
            float ey = h - dp(46);
            for (String piece : wrap(error, w - dp(44), text)) {
                canvas.drawText(piece, x, ey, text);
                ey += dp(16);
            }
        } else {
            text.setColor(Palette.INK_3);
            text.setTextSize(dp(11.5f));
            canvas.drawText(str(R.string.key_local), x, h - dp(24), text);
        }
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

    /**
     * Under the button: the connection's facts while connected, the
     * server's state while not.
     */
    private void drawRows(Canvas canvas, float w) {
        float y = buttonCy + buttonR + dp(44);
        final float labelX = w / 2f - dp(120), valueX = w / 2f - dp(10);

        if (state == STATE_LIVE) {
            String[][] rows = {
                    { str(R.string.row_protocol), "AmneziaWG" },
                    { str(R.string.row_handshake), handshake != null ? handshake : str(R.string.handshake_none) },
                    { str(R.string.row_address), address != null ? address : "—" },
            };
            for (String[] row : rows) {
                text.setTextSize(dp(13));
                text.setColor(Palette.INK_3);
                canvas.drawText(row[0], labelX, y, text);
                text.setColor(Palette.INK_2);
                canvas.drawText(row[1], valueX, y, text);
                y += dp(24);
            }
        } else if (state == STATE_OFF && server != null) {
            text.setTextSize(dp(13));
            text.setColor(Palette.INK_2);
            text.setTextAlign(Paint.Align.CENTER);
            canvas.drawText(server, w / 2f, y, text);
            text.setTextAlign(Paint.Align.LEFT);
        }
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
