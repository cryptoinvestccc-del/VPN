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
 * The whole screen, drawn by hand: the name top left, a circle with the
 * logo's planet in it,
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

    private int state = STATE_OFF;
    /** Until when "disconnecting" is shown after the button turned the tunnel off. */
    private long leavingUntil;
    private String error;
    private float spin;          // the waiting ring's angle
    private float spinSlow = 20f; // the planet's turn
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

    /**
     * Shows "disconnecting" for a moment. The tunnel itself goes down at
     * once, so without this the word would never be seen.
     */
    void showLeaving() {
        leavingUntil = android.os.SystemClock.uptimeMillis() + 1200;
        invalidate();
    }

    private boolean leaving() {
        return state == STATE_OFF && android.os.SystemClock.uptimeMillis() < leavingUntil;
    }

    void setState(int s) {
        if (s != STATE_OFF) leavingUntil = 0;
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
        drawTitle(canvas);
        drawButton(canvas);
        drawLamp(canvas);
        drawGear(canvas, w);
        drawReadout(canvas, w, h);

        if (state == STATE_BUSY || leaving()) {
            spin += 6f;
            if (spin >= 360f) spin -= 360f;
        }
        if (state != STATE_OFF || leaving()) {
            // the planet keeps turning, slowly, while the tunnel is up
            spinSlow += state == STATE_LIVE ? 0.35f : 1.2f;
            if (spinSlow >= 360f) spinSlow -= 360f;
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
     * The one control: a dark orb with the logo's planet in it — the globe
     * and its orbit, drawn in chrome — and a thin ring around it.
     */
    private void drawButton(Canvas canvas) {
        final boolean live = state == STATE_LIVE, moving = state == STATE_BUSY || leaving();

        // the orb, lit from inside when the tunnel is up
        fill.setShader(new RadialGradient(buttonCx, buttonCy - buttonR * 0.1f, buttonR,
                new int[] { live ? 0x44DDE6FF : 0x1CFFFFFF, live ? 0x1A8FA3D9 : 0x0AFFFFFF, 0x00000000 },
                new float[] { 0f, 0.6f, 1f }, Shader.TileMode.CLAMP));
        canvas.drawCircle(buttonCx, buttonCy, buttonR, fill);
        fill.setShader(null);

        // the ring: faint at rest, whole and bright when live, a running arc while switching
        stroke.setShader(null);
        stroke.setStrokeCap(Paint.Cap.ROUND);
        stroke.setStrokeWidth(dp(2));
        stroke.setColor(live ? 0xCCFFFFFF : 0x2EFFFFFF);
        canvas.drawCircle(buttonCx, buttonCy, buttonR, stroke);
        if (moving) {
            stroke.setColor(0xFFFFFFFF);
            stroke.setStrokeWidth(dp(2.6f));
            rect.set(buttonCx - buttonR, buttonCy - buttonR, buttonCx + buttonR, buttonCy + buttonR);
            canvas.drawArc(rect, spin, 70f, false, stroke);
            canvas.drawArc(rect, spin + 180f, 30f, false, stroke);
        }
        stroke.setStrokeCap(Paint.Cap.BUTT);

        drawPlanet(canvas, buttonCx, buttonCy, buttonR * 0.46f, live, moving);
    }

    /**
     * The globe with its orbit, as in the logo: a wireframe sphere tilted
     * a little, the orbit's far half behind it and its near half in front,
     * and the sparkle on the orbit. Grey while the tunnel is down, chrome
     * when it is up; it turns slowly while connected or connecting.
     */
    private void drawPlanet(Canvas canvas, float cx, float cy, float r, boolean live, boolean moving) {
        final float turn = (float) Math.toRadians(live || moving ? spinSlow : 20f);
        final Shader chrome = new LinearGradient(0, cy - r * 1.25f, 0, cy + r * 1.25f,
                new int[] { 0xFFFFFFFF, 0xFFDCDEE3, 0xFF7A7D85, 0xFF3A3C42, 0xFFB7BAC1, 0xFFF4F5F7, 0xFF9295A0 },
                new float[] { 0f, 0.22f, 0.42f, 0.5f, 0.6f, 0.8f, 1f }, Shader.TileMode.CLAMP);
        final int alpha = live ? 255 : moving ? 200 : 110;

        canvas.save();
        canvas.rotate(-16f, cx, cy);
        stroke.setStrokeCap(Paint.Cap.ROUND);
        if (live || moving) { stroke.setColor(0xFFFFFFFF); stroke.setShader(chrome); }
        else { stroke.setShader(null); stroke.setColor(0xFF8A8D95); }

        // orbit, far half (behind the globe)
        final float orx = r * 1.72f, ory = r * 0.46f;
        rect.set(cx - orx, cy - ory, cx + orx, cy + ory);
        stroke.setAlpha(alpha * 7 / 10);
        stroke.setStrokeWidth(r * 0.1f);
        canvas.drawArc(rect, 180f, 180f, false, stroke);

        // a dark disc so the far half of the orbit hides behind the sphere
        fill.setShader(null);
        fill.setColor(0xFF08090B);
        canvas.drawCircle(cx, cy, r, fill);

        // meridians: the far ones faint, the near ones full
        stroke.setStrokeWidth(r * 0.035f);
        for (int i = 0; i < 12; i++) {
            final double lam = i * Math.PI / 6 + turn;
            final float rx = (float) Math.abs(Math.sin(lam)) * r;
            if (rx < 0.5f) continue;
            stroke.setAlpha(Math.cos(lam) > 0 ? alpha : alpha / 4);
            rect.set(cx - rx, cy - r, cx + rx, cy + r);
            canvas.drawArc(rect, Math.sin(lam) > 0 ? -90f : 90f, 180f, false, stroke);
        }
        // parallels: near half full, far half faint
        for (int ph : new int[] { -55, -25, 5, 35 }) {
            final double a = Math.toRadians(ph);
            final float y = cy - r * (float) Math.sin(a), rx = r * (float) Math.cos(a), ry = rx * 0.2f;
            rect.set(cx - rx, y - ry, cx + rx, y + ry);
            stroke.setAlpha(alpha);
            canvas.drawArc(rect, 0f, 180f, false, stroke);
            stroke.setAlpha(alpha / 4);
            canvas.drawArc(rect, 180f, 180f, false, stroke);
        }
        stroke.setAlpha(alpha);
        stroke.setStrokeWidth(r * 0.07f);
        canvas.drawCircle(cx, cy, r, stroke);

        // orbit, near half (in front of the globe)
        rect.set(cx - orx, cy - ory, cx + orx, cy + ory);
        stroke.setStrokeWidth(r * 0.12f);
        canvas.drawArc(rect, 0f, 180f, false, stroke);
        stroke.setShader(null);
        stroke.setAlpha(255);
        stroke.setStrokeCap(Paint.Cap.BUTT);

        // the sparkle sits on the orbit's left end, as in the logo
        final float sx = cx - orx * 0.93f, sy = cy + ory * 0.35f;
        drawSparkle(canvas, sx, sy, r * (live ? 0.26f : 0.18f), live ? 0xFFFFFFFF : 0x99A0A3AB, live);
        canvas.restore();
    }

    /** The four-point star from the logo. */
    private void drawSparkle(Canvas canvas, float x, float y, float r, int color, boolean glow) {
        if (glow) {
            fill.setShader(new RadialGradient(x, y, r * 1.8f, 0x88FFFFFF, 0x00FFFFFF, Shader.TileMode.CLAMP));
            canvas.drawCircle(x, y, r * 1.8f, fill);
            fill.setShader(null);
        }
        final float k = r / 12f;
        path.reset();
        path.moveTo(x, y - 12 * k);
        path.cubicTo(x + .8f * k, y - 4 * k, x + 4 * k, y - .8f * k, x + 12 * k, y);
        path.cubicTo(x + 4 * k, y + .8f * k, x + .8f * k, y + 4 * k, x, y + 12 * k);
        path.cubicTo(x - .8f * k, y + 4 * k, x - 4 * k, y + .8f * k, x - 12 * k, y);
        path.cubicTo(x - 4 * k, y - .8f * k, x - .8f * k, y - 4 * k, x, y - 12 * k);
        path.close();
        fill.setColor(color);
        canvas.drawPath(path, fill);
    }

    /**
     * The name, top left, lettered like the logo: heavy italic capitals,
     * black inside a chrome outline.
     */
    private void drawTitle(Canvas canvas) {
        int top = (int) dp(24);
        android.view.WindowInsets insets = getRootWindowInsets();
        if (insets != null) top = insets.getSystemWindowInsetTop();
        final float x = dp(22), base = top + dp(40);
        final String name = "BESY VPN";

        text.setTypeface(android.graphics.Typeface.create("sans-serif-black", android.graphics.Typeface.BOLD));
        text.setTextSkewX(-0.22f);
        text.setTextSize(dp(26));
        text.setTextAlign(Paint.Align.LEFT);
        text.setLetterSpacing(0.03f);

        text.setStyle(Paint.Style.STROKE);
        text.setStrokeJoin(Paint.Join.ROUND);
        text.setStrokeWidth(dp(4f));
        text.setShader(new LinearGradient(0, base - dp(20), 0, base + dp(2),
                new int[] { 0xFFFFFFFF, 0xFFC4C7CE, 0xFFFFFFFF, 0xFFB9BCC4 },
                new float[] { 0f, 0.45f, 0.55f, 1f }, Shader.TileMode.CLAMP));
        canvas.drawText(name, x, base, text);

        text.setShader(null);
        text.setStyle(Paint.Style.FILL);
        text.setColor(0xFF050506);
        canvas.drawText(name, x, base, text);

        text.setTypeface(null);
        text.setTextSkewX(0f);
        text.setLetterSpacing(0f);
        text.setStrokeWidth(0f);
    }

    /**
     * The small lamp under the circle: green when the tunnel is up, red
     * when it is down, amber and breathing while it is being set up.
     */
    private void drawLamp(Canvas canvas) {
        final float cx = buttonCx, cy = buttonCy + buttonR + dp(30), r = dp(7);
        int color;
        float glow = 1f;
        if (state == STATE_BUSY || leaving()) {
            color = Palette.LAMP_WAIT;
            glow = 0.55f + 0.45f * (float) Math.abs(Math.sin(Math.toRadians(spin * 2)));
        } else {
            color = state == STATE_LIVE ? Palette.LAMP_ON : Palette.LAMP_OFF;
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
        // Words only while the switch is moving; at rest the lamp says it.
        final String title = state == STATE_BUSY ? str(R.string.busy_title)
                : leaving() ? str(R.string.leaving_title) : null;
        text.setTextAlign(Paint.Align.CENTER);
        if (title != null) {
            text.setColor(Palette.INK);
            text.setTextSize(dp(18));
            canvas.drawText(title, w / 2f, buttonCy + buttonR + dp(72), text);
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
