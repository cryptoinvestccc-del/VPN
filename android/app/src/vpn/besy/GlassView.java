package vpn.besy;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.graphics.Canvas;
import android.graphics.LinearGradient;
import android.graphics.Paint;
import android.graphics.Path;
import android.graphics.RadialGradient;
import android.graphics.RectF;
import android.graphics.Shader;
import android.graphics.Typeface;
import android.os.Build;
import android.os.SystemClock;
import android.view.MotionEvent;
import android.view.View;

/**
 * The main screen, drawn by hand, after the approved design.
 *
 * <p>The top is the handoff's dark card: the BESY VPN wordmark, the gear
 * for settings, the connection status and a full-width pill button. Below
 * it, as in the HTML prototype: "Overview", a wide speed card with a dial,
 * two cards for session time and connection state, and the fixed location.
 *
 * <p>There is no Compose and no AndroidX here: those live on a Maven host
 * this build did not reach, so every surface is a shape on a Canvas. It
 * keeps the APK small and puts all motion under one clock.
 *
 * <p>Motion is driven by elapsed time rather than frames, so a 120 Hz
 * screen moves at the speed a 60 Hz one does, and every change of state
 * eases in over about a quarter of a second, as the handoff asks ("soft
 * scale and glow", nothing pulsing hard). Shaders are built when the
 * layout changes, never per frame. When nothing moves the view asks for no
 * frames; while connected and idle it wakes once a second, on the second,
 * for the timer.
 */
final class GlassView extends View {

    interface OnPowerTap { void onPowerTap(); }
    interface OnSettingsTap { void onSettingsTap(); }

    static final int STATE_OFF  = 0;
    static final int STATE_BUSY = 1;
    static final int STATE_LIVE = 2;

    // ---- Colours ----------------------------------------------------------
    // Top, from the handoff JSON.
    private static final int BG       = 0xFFF5F6F8;
    private static final int HERO_A   = 0xFF1A1D24;
    private static final int HERO_B   = 0xFF111318;
    private static final int ACCENT   = 0xFF7C6CFF;
    private static final int SUCCESS  = 0xFF58D68D;
    private static final int BUSY_BTN = 0xFF2A2D36;
    private static final int INK      = 0xFF111318;
    private static final int GREY_DOT = 0xFF6F737C;
    private static final int WAIT     = 0xFFF2B33D;
    // Bottom, from the HTML prototype.
    private static final int CARD         = 0xFFFFFFFF;
    private static final int CARD_BORDER  = 0xFFE6EBED;
    private static final int CARD_HEAD    = 0xFF747D84;
    private static final int UNIT         = 0xFF818B92;
    private static final int ICON         = 0xFF8C989E;
    private static final int DIAL_LABEL   = 0xFF8C969B;
    private static final int TRACK        = 0xFFEEF0F4;
    private static final int TRACK_DARK   = 0xFFC9CDD6;
    private static final int GAUGE_FROM   = 0xFFB3A9FF;
    private static final int LIVE_BG      = 0xFFE6F4F1;
    private static final int LIVE_BORDER  = 0xFFD6EAE4;
    private static final int LIVE_HEAD    = 0xFF648C83;
    private static final int LIVE_INK     = 0xFF23786D;
    private static final int WAIT_ICON    = 0xFFB68D49;
    private static final int NOTE         = 0xFF808B92;
    private static final int NOTE_ICON    = 0xFF97A1A7;
    private static final int ERROR        = 0xFFC0392B;

    private final Paint fill    = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint stroke  = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint text    = new Paint(Paint.ANTI_ALIAS_FLAG | Paint.SUBPIXEL_TEXT_FLAG);
    private final Paint picture = new Paint(Paint.ANTI_ALIAS_FLAG | Paint.FILTER_BITMAP_FLAG);
    private final RectF rect    = new RectF();
    private final Path  path    = new Path();

    private int state = STATE_OFF;
    private long leavingUntil;       // "disconnecting" is shown until then
    private String error;
    private OnPowerTap listener;
    private OnSettingsTap settingsListener;
    // Kept for the activity's calls; the design does not show them.
    private String address, handshake, server;

    private Bitmap logo;
    private final Typeface regular, medium, semibold;

    GlassView(Context c) {
        super(c);
        setClickable(true);
        stroke.setStyle(Paint.Style.STROKE);
        regular  = face(400);
        medium   = face(500);
        semibold = face(620);
    }

    /** A system sans-serif at the given weight; before Android 9 only medium and bold exist. */
    private static Typeface face(int weight) {
        if (Build.VERSION.SDK_INT >= 28) return NewerApi.weight(Typeface.DEFAULT, weight);
        if (weight >= 600) return Typeface.create("sans-serif-medium", Typeface.BOLD);
        if (weight >= 500) return Typeface.create("sans-serif-medium", Typeface.NORMAL);
        return Typeface.create("sans-serif", Typeface.NORMAL);
    }

    void setOnPowerTap(OnPowerTap l) { listener = l; }
    void setOnSettingsTap(OnSettingsTap l) { settingsListener = l; }

    void setInfo(String address, String handshake) { this.address = address; this.handshake = handshake; }
    void setServer(String line) { server = line; }

    /** Shows "disconnecting" for a moment; the tunnel itself goes down at once. */
    void showLeaving() {
        leavingUntil = SystemClock.uptimeMillis() + 1200;
        invalidate();
    }

    private boolean leaving() {
        return state == STATE_OFF && SystemClock.uptimeMillis() < leavingUntil;
    }

    void setState(int s) {
        if (s != STATE_OFF) leavingUntil = 0;
        if (state != s) {
            state = s;
            invalidate();
            sendAccessibilityEvent(android.view.accessibility.AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED);
        }
    }

    int getState() { return state; }

    /** Why the last attempt failed, kept on screen until the next one. */
    void setError(String message) {
        if (message == null ? error != null : !message.equals(error)) {
            error = message;
            invalidate();
        }
    }

    private float dp(float v) { return v * getResources().getDisplayMetrics().density; }
    private float sp(float v) { return v * getResources().getDisplayMetrics().scaledDensity; }
    private String str(int id) { return getContext().getString(id); }

    // ---- Layout -----------------------------------------------------------

    private final RectF hero = new RectF(), button = new RectF(), speedCard = new RectF(),
            timeCard = new RectF(), stateCard = new RectF(), logoBox = new RectF(), overview = new RectF();
    private float gearCx, gearCy, statusY, dotX, dotY, headingY, noteY, dialCx, dialCy, dialR, k = 1f;
    private int laidTop = -1, laidBottom = -1, laidW, laidH;
    private Shader heroShade, haloGreen, haloAmber, gaugeShade;

    /** Places everything and builds the shaders; redone only when the size or the bars change. */
    private void layout(int w, int h) {
        android.view.WindowInsets in = getRootWindowInsets();
        final int top = in != null ? in.getSystemWindowInsetTop() : (int) dp(24);
        final int bottom = in != null ? in.getSystemWindowInsetBottom() : 0;
        if (top == laidTop && bottom == laidBottom && w == laidW && h == laidH) return;
        laidTop = top; laidBottom = bottom; laidW = w; laidH = h;

        // At most 480dp wide, centred, as the handoff asks for large screens.
        final float content = Math.min(w, dp(480));
        final float side = w / getResources().getDisplayMetrics().density < 360 ? dp(14) : dp(16);
        final float left = (w - content) / 2f + side, right = (w + content) / 2f - side;

        // The column at the design's sizes is about 750dp tall; a shorter
        // screen scales the heights (never the widths) to fit.
        final float avail = h - top - bottom - dp(24);
        k = Math.max(0.72f, Math.min(1f, avail / dp(752)));

        float y = top + dp(12);
        hero.set(left, y, right, y + dp(300) * k);
        final float pad = dp(20);
        final float logoW = Math.min(dp(232), hero.width() - 2 * pad - dp(44));
        final float logoH = logoW * 308f / 1000f;
        logoBox.set(hero.left + pad - dp(6), hero.top + pad, hero.left + pad - dp(6) + logoW, hero.top + pad + logoH);
        gearCx = hero.right - pad - dp(11);
        gearCy = logoBox.centerY();
        final float btnH = Math.max(dp(56), dp(64) * k);
        button.set(hero.left + pad, hero.bottom - pad - btnH, hero.right - pad, hero.bottom - pad);
        statusY = button.top - dp(18) * Math.max(k, 0.85f);
        text.setTypeface(medium);
        text.setTextSize(sp(16) * Math.max(k, 0.9f));
        dotX = hero.left + pad + dp(4);
        dotY = statusY + (text.ascent() + text.descent()) / 2f;

        y = hero.bottom + dp(24) * k;
        headingY = y + dp(18) * k;
        y = headingY + dp(14) * k;

        speedCard.set(left, y, right, y + dp(180) * k);
        dialR = Math.min(dp(64) * k, (speedCard.height() - dp(44)) / 2f);
        dialCx = speedCard.right - dp(20) - dialR;
        dialCy = speedCard.centerY() - dp(4);

        y = speedCard.bottom + dp(12);
        final float half = (right - left - dp(12)) / 2f;
        timeCard.set(left, y, left + half, y + dp(160) * k);
        stateCard.set(right - half, y, right, y + dp(160) * k);
        noteY = timeCard.bottom + dp(18) * k + dp(12);
        overview.set(left, hero.bottom, right, noteY);

        final android.graphics.SweepGradient sweep = new android.graphics.SweepGradient(dialCx, dialCy,
                new int[] { GAUGE_FROM, ACCENT, ACCENT }, new float[] { 0f, 264f / 360f, 1f });
        final android.graphics.Matrix turn = new android.graphics.Matrix();
        turn.setRotate(138f, dialCx, dialCy);
        sweep.setLocalMatrix(turn);
        gaugeShade = sweep;
        heroShade = new LinearGradient(hero.left, hero.top, hero.right, hero.bottom,
                HERO_A, HERO_B, Shader.TileMode.CLAMP);
        haloGreen = new RadialGradient(dotX, dotY, dp(13), (0x8C << 24) | (SUCCESS & 0xFFFFFF),
                SUCCESS & 0xFFFFFF, Shader.TileMode.CLAMP);
        haloAmber = new RadialGradient(dotX, dotY, dp(13), (0x8C << 24) | (WAIT & 0xFFFFFF),
                WAIT & 0xFFFFFF, Shader.TileMode.CLAMP);
    }

    // ---- Motion -----------------------------------------------------------

    private long lastFrame;
    private float offW = 1, busyW, onW;     // how much of each state the button shows
    private float live;                     // the connected colours, 0..1
    private float press = 1;                // button scale while pressed
    private boolean pressed;
    private float spin, pulse;              // spinner angle, amber dot breathing
    private float shownMbps, angle = -132f; // the number and the pointer, eased
    private float scaleMax = 50f;           // the dial's top value
    private long lowSince;                  // since when the speed sits well under the scale

    private static float approach(float v, float target, float rate, float dt) {
        return target + (v - target) * (float) Math.exp(-rate * dt);
    }

    /** Download speed through the tunnel, Mbit/s. */
    private float mbps() {
        return state == STATE_LIVE ? (float) (TunnelService.rxRate * 8 / 1e6) : 0f;
    }

    /** Moves everything on to now; says whether the next frame should come at once. */
    private boolean step() {
        final long now = SystemClock.uptimeMillis();
        final float dt = lastFrame == 0 ? 0f : Math.min(0.05f, (now - lastFrame) / 1000f);
        lastFrame = now;
        final boolean busy = state == STATE_BUSY || leaving(), on = state == STATE_LIVE && !busy;
        final float offT = !busy && !on ? 1 : 0, busyT = busy ? 1 : 0, onT = on ? 1 : 0;

        offW  = approach(offW,  offT,  12, dt);
        busyW = approach(busyW, busyT, 12, dt);
        onW   = approach(onW,   onT,   12, dt);
        live  = approach(live,  onT,    9, dt);
        press = approach(press, pressed ? 0.97f : 1f, 18, dt);
        if (busy) {
            spin = (spin + 400f * dt) % 360f;
            pulse = (pulse + 4f * dt) % (float) (2 * Math.PI);
        }

        // The scale grows as soon as the speed needs it and shrinks only
        // after the speed has stayed well under it for ten seconds, so it
        // does not hop back and forth on a bursty connection.
        final float v = mbps();
        final float want = scaleFor(v);
        if (want > scaleMax) { scaleMax = want; lowSince = 0; }
        else if (want < scaleMax) {
            if (lowSince == 0) lowSince = now;
            else if (now - lowSince > 10000) { scaleMax = want; lowSince = 0; }
        } else lowSince = 0;
        shownMbps = approach(shownMbps, v, 4, dt);
        final float target = -132f + Math.min(1f, v / scaleMax) * 264f;
        angle = approach(angle, target, 3.5f, dt);

        return busy || pressed
                || Math.abs(offW - offT) > 0.002f || Math.abs(onW - onT) > 0.002f
                || Math.abs(busyW - busyT) > 0.002f || Math.abs(live - onT) > 0.002f
                || Math.abs(press - (pressed ? 0.97f : 1f)) > 0.001f
                || Math.abs(shownMbps - v) > 0.05f
                || Math.abs(angle - target) > 0.2f;
    }

    private static float scaleFor(float mbps) {
        final float[] steps = { 50, 100, 200, 500, 1000, 2000 };
        for (float s : steps) if (mbps * 1.15f <= s) return s;
        return steps[steps.length - 1];
    }

    @Override protected void onDraw(Canvas canvas) {
        final int w = getWidth(), h = getHeight();
        if (w <= 0 || h <= 0) return;
        layout(w, h);
        final boolean again = step();

        canvas.drawColor(BG);
        drawHero(canvas);
        drawOverview(canvas);

        if (again) {
            postInvalidateOnAnimation();
        } else {
            lastFrame = 0;
            if (state == STATE_LIVE && TunnelService.connectedAt > 0) {
                // wake on the next whole second, for the timer, and no sooner
                final long ms = SystemClock.elapsedRealtime() - TunnelService.connectedAt;
                postInvalidateDelayed(1000 - ms % 1000 + 5);
            } else if (leavingUntil > SystemClock.uptimeMillis()) {
                postInvalidateDelayed(leavingUntil - SystemClock.uptimeMillis() + 5);
            }
        }
    }

    // ---- The dark card ----------------------------------------------------

    private String statusWord(boolean busy) {
        if (busy) return str(leaving() ? R.string.leaving_title : R.string.busy_title);
        return str(state == STATE_LIVE ? R.string.live_title : R.string.idle_title);
    }

    private void drawHero(Canvas canvas) {
        final float r = dp(28);
        fill.setShader(heroShade);
        canvas.drawRoundRect(hero, r, r, fill);
        fill.setShader(null);

        if (logo == null) logo = BitmapFactory.decodeResource(getResources(), R.drawable.logo_wordmark);
        if (logo != null) canvas.drawBitmap(logo, null, logoBox, picture);
        drawGear(canvas);

        // status: a dot and a word
        final boolean busy = state == STATE_BUSY || leaving();
        final float dotR = dp(4);
        if (onW > 0.01f) {
            fill.setShader(haloGreen);
            fill.setAlpha((int) (255 * onW));
            canvas.drawCircle(dotX, dotY, dp(13), fill);
        }
        if (busyW > 0.01f) {
            fill.setShader(haloAmber);
            fill.setAlpha((int) (255 * busyW * (0.45f + 0.4f * (float) Math.abs(Math.sin(pulse)))));
            canvas.drawCircle(dotX, dotY, dp(13), fill);
        }
        fill.setShader(null);
        fill.setAlpha(255);
        fill.setColor(mix3(GREY_DOT, WAIT, SUCCESS));
        canvas.drawCircle(dotX, dotY, dotR, fill);

        text.setTypeface(medium);
        text.setTextSize(sp(16) * Math.max(k, 0.9f));
        text.setTextAlign(Paint.Align.LEFT);
        text.setColor(0xCCFFFFFF);
        canvas.drawText(statusWord(busy), dotX + dotR + dp(8), statusY, text);

        drawButton(canvas, busy);
    }

    /** The pill: violet to connect, dark while working, white to disconnect. */
    private void drawButton(Canvas canvas, boolean busy) {
        canvas.save();
        canvas.scale(press, press, button.centerX(), button.centerY());
        final float r = button.height() / 2f;

        // a soft glow under it: violet while idle, green once connected
        final int glowColor = blend(ACCENT, SUCCESS, onW / Math.max(0.001f, offW + onW));
        final float glowAmount = offW * 0.55f + onW * 0.45f;
        if (glowAmount > 0.01f) {
            fill.setShader(null);
            for (int i = 6; i >= 1; i--) {
                final float grow = dp(2.2f) * i;
                rect.set(button.left - grow, button.top - grow + dp(4), button.right + grow, button.bottom + grow + dp(4));
                fill.setColor(glowColor);
                fill.setAlpha((int) (glowAmount * 22 * (7 - i) / 6f));
                canvas.drawRoundRect(rect, r + grow, r + grow, fill);
            }
            fill.setAlpha(255);
        }

        fill.setColor(mix3(ACCENT, BUSY_BTN, 0xFFFFFFFF));
        canvas.drawRoundRect(button, r, r, fill);

        final String label = busy ? statusWord(true)
                : str(state == STATE_LIVE ? R.string.btn_disconnect : R.string.btn_connect);
        text.setTypeface(semibold);
        text.setTextSize(sp(17));
        text.setTextAlign(Paint.Align.LEFT);
        final float labelW = text.measureText(label);
        final float spinnerW = (dp(18) + dp(10)) * busyW;
        float x = button.centerX() - (labelW + spinnerW) / 2f;
        final float baseline = button.centerY() - (text.descent() + text.ascent()) / 2f;
        if (busyW > 0.01f) {
            final float sr = dp(9), scx = x + sr, scy = button.centerY();
            stroke.setShader(null);
            stroke.setStrokeWidth(dp(2));
            stroke.setStrokeCap(Paint.Cap.ROUND);
            stroke.setColor(0xFFFFFFFF);
            stroke.setAlpha((int) (0x55 * busyW));
            canvas.drawCircle(scx, scy, sr, stroke);
            stroke.setAlpha((int) (255 * busyW));
            rect.set(scx - sr, scy - sr, scx + sr, scy + sr);
            canvas.drawArc(rect, spin, 90, false, stroke);
            stroke.setAlpha(255);
            stroke.setStrokeCap(Paint.Cap.BUTT);
            x += spinnerW;
        }
        text.setColor(blend(0xFFFFFFFF, INK, onW));
        canvas.drawText(label, x, baseline, text);
        canvas.restore();
    }

    /** A colour from the three state weights: idle, working, connected. */
    private int mix3(int a, int b, int c) {
        final float t = Math.max(0.001f, offW + busyW + onW);
        final float wa = offW / t, wb = busyW / t, wc = onW / t;
        int out = 0xFF000000;
        for (int shift = 0; shift <= 16; shift += 8) {
            final float v = ((a >> shift) & 0xFF) * wa + ((b >> shift) & 0xFF) * wb + ((c >> shift) & 0xFF) * wc;
            out |= (Math.round(v) & 0xFF) << shift;
        }
        return out;
    }

    private static int blend(int a, int b, float u) {
        u = Math.max(0, Math.min(1, u));
        int out = 0;
        for (int shift = 0; shift <= 24; shift += 8) {
            final int x = (a >>> shift) & 0xFF, y = (b >>> shift) & 0xFF;
            out |= (Math.round(x + (y - x) * u) & 0xFF) << shift;
        }
        return out;
    }

    /** Settings: a lobed wheel with a hole, white on the dark card. */
    private void drawGear(Canvas canvas) {
        final float outer = dp(11), inner = dp(8.2f);
        path.reset();
        for (int i = 0; i < 8; i++) {
            final double a = Math.toRadians(i * 45);
            final double[] offs = { -16, -9, 9, 16 };
            final float[] radii = { inner, outer, outer, inner };
            for (int j = 0; j < 4; j++) {
                final double t = a + Math.toRadians(offs[j]);
                final float x = gearCx + (float) (radii[j] * Math.cos(t));
                final float y = gearCy + (float) (radii[j] * Math.sin(t));
                if (i == 0 && j == 0) path.moveTo(x, y); else path.lineTo(x, y);
            }
        }
        path.close();
        stroke.setShader(null);
        stroke.setColor(0xCCFFFFFF);
        stroke.setStrokeWidth(dp(1.6f));
        stroke.setStrokeJoin(Paint.Join.ROUND);
        canvas.drawPath(path, stroke);
        canvas.drawCircle(gearCx, gearCy, dp(3.2f), stroke);
        stroke.setStrokeJoin(Paint.Join.MITER);
    }

    // ---- The light part ---------------------------------------------------

    private void drawOverview(Canvas canvas) {
        text.setTypeface(semibold);
        text.setTextSize(sp(18) * Math.max(k, 0.9f));
        text.setTextAlign(Paint.Align.LEFT);
        text.setColor(INK);
        canvas.drawText(str(R.string.overview), speedCard.left + dp(3), headingY, text);

        drawCard(canvas, speedCard, CARD, CARD_BORDER);
        drawSpeed(canvas);
        drawCard(canvas, timeCard, CARD, CARD_BORDER);
        drawTime(canvas);
        drawCard(canvas, stateCard, blend(CARD, LIVE_BG, live), blend(CARD_BORDER, LIVE_BORDER, live));
        drawStateCard(canvas);
        drawNote(canvas);
    }

    private void drawCard(Canvas canvas, RectF r, int color, int border) {
        final float radius = dp(23);
        // the faintest shadow, two soft steps under the card
        fill.setShader(null);
        for (int i = 2; i >= 1; i--) {
            rect.set(r.left - i, r.top + dp(3) + i, r.right + i, r.bottom + dp(3) + i * 2);
            fill.setColor(0x06141D26);
            canvas.drawRoundRect(rect, radius + i, radius + i, fill);
        }
        fill.setColor(color);
        canvas.drawRoundRect(r, radius, radius, fill);
        stroke.setShader(null);
        stroke.setColor(border);
        stroke.setStrokeWidth(Math.max(1f, dp(1)));
        rect.set(r.left + 0.5f, r.top + 0.5f, r.right - 0.5f, r.bottom - 0.5f);
        canvas.drawRoundRect(rect, radius, radius, stroke);
    }

    private void heading(Canvas canvas, String s, float x, float y, int color) {
        text.setTypeface(medium);
        text.setTextSize(sp(13));
        text.setTextAlign(Paint.Align.LEFT);
        text.setColor(color);
        canvas.drawText(s, x, y, text);
    }

    private void drawSpeed(Canvas canvas) {
        final RectF c = speedCard;
        final float x = c.left + dp(20);
        // heading, number and unit as one block, centred in the card's height
        final float numSize = sp(52) * Math.max(k, 0.85f);
        final float block = sp(13) + dp(15) + numSize * 0.74f + dp(8) + sp(13);
        float y = c.centerY() - block / 2f + sp(13) * 0.8f;
        heading(canvas, str(R.string.tile_speed), x, y, CARD_HEAD);

        text.setTypeface(semibold);
        text.setFontFeatureSettings("tnum");
        text.setLetterSpacing(-0.045f);
        text.setColor(INK);
        y += dp(15) + numSize * 0.74f;
        final String number = number(shownMbps);
        fit(number, numSize, dialCx - dialR - dp(12) - x);
        canvas.drawText(number, x, y, text);
        text.setLetterSpacing(0f);
        text.setFontFeatureSettings(null);

        text.setTypeface(regular);
        text.setTextSize(sp(13));
        text.setColor(UNIT);
        canvas.drawText(str(R.string.mbps), x, y + dp(8) + sp(13), text);

        drawDial(canvas);
    }

    /** One decimal below a hundred, whole numbers from a hundred up. */
    private String number(float mbps) {
        final java.util.Locale here = getResources().getConfiguration().locale;
        if (mbps < 0.05f) mbps = 0f;
        return mbps >= 100 ? String.format(here, "%d", Math.round(mbps)) : String.format(here, "%.1f", mbps);
    }

    /** Sets the text size to at most {@code size}, smaller if the text would not fit. */
    private void fit(String s, float size, float room) {
        text.setTextSize(size);
        final float wide = text.measureText(s);
        if (wide > room && room > 0) text.setTextSize(size * room / wide);
    }

    /**
     * The speed gauge: an open arc on the white card, a light track and a
     * violet band that grows with the speed, ending in a small knob. It is
     * the handoff's "minimal, decorative" speedometer; it does not dominate
     * the card, the number does.
     */
    private void drawDial(Canvas canvas) {
        final float cx = dialCx, cy = dialCy, R = dialR;
        final float band = Math.max(dp(7), R * 0.13f);
        final float rr = R - band / 2f;
        rect.set(cx - rr, cy - rr, cx + rr, cy + rr);

        stroke.setShader(null);
        stroke.setStrokeCap(Paint.Cap.ROUND);
        stroke.setStrokeWidth(band);
        stroke.setColor(TRACK);
        canvas.drawArc(rect, 138f, 264f, false, stroke);

        final float part = Math.max(0f, Math.min(1f, (angle + 132f) / 264f));
        if (part > 0.004f) {
            stroke.setShader(gaugeShade);
            stroke.setColor(0xFFFFFFFF);
            canvas.drawArc(rect, 138f, 264f * part, false, stroke);
            stroke.setShader(null);
        }
        stroke.setStrokeCap(Paint.Cap.BUTT);

        // the knob at the end of the band
        final double a = Math.toRadians(138f + 264f * part);
        final float kx = cx + rr * (float) Math.cos(a), ky = cy + rr * (float) Math.sin(a);
        fill.setShader(null);
        fill.setColor(0x1F7C6CFF);
        canvas.drawCircle(kx, ky, band * 1.15f, fill);
        fill.setColor(0xFFFFFFFF);
        canvas.drawCircle(kx, ky, band * 0.62f, fill);
        stroke.setColor(part > 0.004f ? ACCENT : TRACK_DARK);
        stroke.setStrokeWidth(dp(2.2f));
        canvas.drawCircle(kx, ky, band * 0.62f, stroke);

        text.setTypeface(regular);
        text.setTextSize(sp(10));
        text.setColor(DIAL_LABEL);
        text.setTextAlign(Paint.Align.CENTER);
        final double a0 = Math.toRadians(138), a1 = Math.toRadians(402);
        final float ly = cy + rr * (float) Math.sin(a0) + band + sp(10);
        canvas.drawText("0", cx + rr * (float) Math.cos(a0), ly, text);
        canvas.drawText(Integer.toString((int) scaleMax), cx + rr * (float) Math.cos(a1), ly, text);
        text.setTextAlign(Paint.Align.LEFT);
    }

    private void drawTime(Canvas canvas) {
        final RectF c = timeCard;
        final float x = c.left + dp(19);
        heading(canvas, str(R.string.session_time), x, c.top + dp(19) + sp(13) * 0.8f, CARD_HEAD);

        long sec = 0;
        if (state == STATE_LIVE && TunnelService.connectedAt > 0) {
            sec = Math.max(0, (SystemClock.elapsedRealtime() - TunnelService.connectedAt) / 1000);
        }
        final long hh = sec / 3600, mm = sec / 60 % 60, ss = sec % 60;
        final String t = hh > 0
                ? String.format(java.util.Locale.ROOT, "%02d:%02d:%02d", hh, mm, ss)
                : String.format(java.util.Locale.ROOT, "%02d:%02d", mm, ss);

        text.setTypeface(semibold);
        text.setFontFeatureSettings("tnum");
        text.setLetterSpacing(-0.03f);
        text.setColor(INK);
        final float base = c.bottom - dp(19);
        final float size = (hh > 0 ? sp(26) : sp(34)) * Math.max(k, 0.85f);
        fit(t, size, c.width() - dp(38));
        canvas.drawText(t, x, base, text);
        final float cap = text.getTextSize() * 0.72f;
        text.setLetterSpacing(0f);
        text.setFontFeatureSettings(null);

        drawClock(canvas, x - dp(2), base - cap - dp(10) - dp(27), dp(27), ICON);
    }

    private void drawStateCard(Canvas canvas) {
        final RectF c = stateCard;
        final float x = c.left + dp(19);
        final boolean busy = state == STATE_BUSY || leaving();
        heading(canvas, str(R.string.tile_connection), x, c.top + dp(19) + sp(13) * 0.8f,
                blend(CARD_HEAD, LIVE_HEAD, live));

        text.setTypeface(semibold);
        text.setTextSize(sp(busy ? 18 : 20) * Math.max(k, 0.85f));
        text.setLetterSpacing(-0.02f);
        text.setColor(blend(INK, LIVE_INK, live));
        final java.util.List<String> lines = wrap(statusWord(busy), c.width() - dp(38), text, 2);
        final float lh = text.getTextSize() * 1.15f;
        final float first = c.bottom - dp(19) - (lines.size() - 1) * lh;
        float y = first;
        for (String line : lines) { canvas.drawText(line, x, y, text); y += lh; }
        final float cap = text.getTextSize() * 0.72f;
        text.setLetterSpacing(0f);

        final int iconColor = blend(busy ? WAIT_ICON : ICON, LIVE_INK, live);
        drawSignal(canvas, x - dp(3), first - cap - dp(12) - dp(27), dp(27), iconColor);
    }

    private void drawNote(Canvas canvas) {
        final String place = str(R.string.location_name);
        text.setTypeface(regular);
        text.setTextSize(sp(12));
        text.setTextAlign(Paint.Align.LEFT);
        text.setColor(NOTE);
        final float iw = dp(14), gap = dp(5), tw = text.measureText(place);
        final float x = speedCard.centerX() - (iw + gap + tw) / 2f;
        drawPin(canvas, x, noteY - iw + dp(2), iw, NOTE_ICON);
        canvas.drawText(place, x + iw + gap, noteY, text);

        if (error != null && error.length() > 0) {
            text.setTextSize(sp(12.5f));
            text.setColor(ERROR);
            text.setTextAlign(Paint.Align.CENTER);
            float y = noteY + dp(22);
            for (String line : wrap(error, speedCard.width() - dp(16), text, 3)) {
                canvas.drawText(line, speedCard.centerX(), y, text);
                y += dp(16);
            }
            text.setTextAlign(Paint.Align.LEFT);
        }
    }

    // ---- Icons: the design's 24-unit outline icons, drawn to size -----------

    private void iconStroke(float size, int color) {
        stroke.setShader(null);
        stroke.setColor(color);
        stroke.setStrokeWidth(1.7f * size / 24f);
        stroke.setStrokeCap(Paint.Cap.ROUND);
        stroke.setStrokeJoin(Paint.Join.ROUND);
    }

    private void drawClock(Canvas canvas, float x, float y, float size, int color) {
        final float u = size / 24f;
        iconStroke(size, color);
        canvas.drawCircle(x + 12 * u, y + 12 * u, 9 * u, stroke);
        path.reset();
        path.moveTo(x + 12 * u, y + 7 * u);
        path.lineTo(x + 12 * u, y + 12 * u);
        path.lineTo(x + 15 * u, y + 14 * u);
        canvas.drawPath(path, stroke);
        stroke.setStrokeCap(Paint.Cap.BUTT);
        stroke.setStrokeJoin(Paint.Join.MITER);
    }

    private void drawSignal(Canvas canvas, float x, float y, float size, int color) {
        final float u = size / 24f;
        iconStroke(size, color);
        rect.set(x + 3 * u, y + 14 * u, x + 6 * u, y + 19 * u);
        canvas.drawRect(rect, stroke);
        rect.set(x + 10.5f * u, y + 10 * u, x + 13.5f * u, y + 19 * u);
        canvas.drawRect(rect, stroke);
        rect.set(x + 18 * u, y + 5 * u, x + 21 * u, y + 19 * u);
        canvas.drawRect(rect, stroke);
        stroke.setStrokeCap(Paint.Cap.BUTT);
        stroke.setStrokeJoin(Paint.Join.MITER);
    }

    private void drawPin(Canvas canvas, float x, float y, float size, int color) {
        final float u = size / 24f;
        iconStroke(size, color);
        stroke.setStrokeWidth(1.6f * u);
        path.reset();
        path.moveTo(x + 20 * u, y + 10 * u);
        path.cubicTo(x + 20 * u, y + 16 * u, x + 12 * u, y + 22 * u, x + 12 * u, y + 22 * u);
        path.cubicTo(x + 12 * u, y + 22 * u, x + 4 * u, y + 16 * u, x + 4 * u, y + 10 * u);
        rect.set(x + 4 * u, y + 2 * u, x + 20 * u, y + 18 * u);
        path.arcTo(rect, 180, 180, false);
        canvas.drawPath(path, stroke);
        canvas.drawCircle(x + 12 * u, y + 10 * u, 2.5f * u, stroke);
        stroke.setStrokeCap(Paint.Cap.BUTT);
        stroke.setStrokeJoin(Paint.Join.MITER);
    }

    /**
     * Breaks text into lines that fit.
     *
     * <p>Clipped text is worse than no text: it stops exactly where the
     * useful part of an error usually begins.
     */
    private static java.util.List<String> wrap(String message, float width, Paint paint, int max) {
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
            if (lines.size() >= max) break;
        }
        if (line.length() > 0 && lines.size() < max) lines.add(line.toString());
        return lines;
    }

    // ---- Accessibility ----------------------------------------------------
    //
    // The screen is drawn, not built from widgets, so each control is a
    // virtual node with bounds, a label and an action, and the cards are
    // read out as one sentence.

    private static final int NODE_POWER = 1;
    private static final int NODE_GEAR  = 2;
    private static final int NODE_STATS = 3;

    private final android.view.accessibility.AccessibilityNodeProvider nodes =
            new android.view.accessibility.AccessibilityNodeProvider() {
        @Override public android.view.accessibility.AccessibilityNodeInfo createAccessibilityNodeInfo(int id) {
            android.view.accessibility.AccessibilityNodeInfo info;
            if (id == View.NO_ID) {
                info = android.view.accessibility.AccessibilityNodeInfo.obtain(GlassView.this);
                onInitializeAccessibilityNodeInfo(info);
                info.addChild(GlassView.this, NODE_POWER);
                info.addChild(GlassView.this, NODE_GEAR);
                info.addChild(GlassView.this, NODE_STATS);
                return info;
            }
            if (id != NODE_POWER && id != NODE_GEAR && id != NODE_STATS) return null;
            info = android.view.accessibility.AccessibilityNodeInfo.obtain(GlassView.this, id);
            info.setPackageName(getContext().getPackageName());
            info.setParent(GlassView.this);
            android.graphics.Rect r = bounds(id);
            info.setBoundsInParent(r);
            int[] at = new int[2];
            getLocationOnScreen(at);
            r.offset(at[0], at[1]);
            info.setBoundsInScreen(r);
            info.setVisibleToUser(true);
            info.setFocusable(true);
            if (id == NODE_STATS) {
                info.setClassName("android.widget.TextView");
                info.setContentDescription(spoken());
                return info;
            }
            info.setClassName("android.widget.Button");
            info.setContentDescription(label(id));
            info.setEnabled(true);
            info.setClickable(true);
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
            case STATE_LIVE: return str(R.string.btn_disconnect);
            case STATE_BUSY: return str(R.string.busy_title);
            default:         return str(R.string.btn_connect);
        }
    }

    private String spoken() {
        long sec = state == STATE_LIVE && TunnelService.connectedAt > 0
                ? (SystemClock.elapsedRealtime() - TunnelService.connectedAt) / 1000 : 0;
        return str(R.string.tile_speed) + " " + number(mbps()) + " " + str(R.string.mbps) + ". "
                + str(R.string.session_time) + " " + (sec / 60) + ":" + String.format(java.util.Locale.ROOT, "%02d", sec % 60) + ". "
                + statusWord(state == STATE_BUSY || leaving()) + ". " + str(R.string.location_name) + ".";
    }

    private android.graphics.Rect bounds(int id) {
        final RectF r = new RectF();
        if (id == NODE_GEAR) r.set(gearCx - dp(24), gearCy - dp(24), gearCx + dp(24), gearCy + dp(24));
        else if (id == NODE_POWER) r.set(button);
        else r.set(overview);
        final android.graphics.Rect out = new android.graphics.Rect();
        r.round(out);
        return out;
    }

    @Override public boolean onTouchEvent(MotionEvent e) {
        final float x = e.getX(), y = e.getY();
        final boolean onGear = Math.hypot(x - gearCx, y - gearCy) <= dp(26);
        final boolean onButton = x >= button.left - dp(4) && x <= button.right + dp(4)
                && y >= button.top - dp(6) && y <= button.bottom + dp(6);
        switch (e.getActionMasked()) {
            case MotionEvent.ACTION_DOWN:
                if (onButton) { pressed = true; invalidate(); return true; }
                return onGear || super.onTouchEvent(e);
            case MotionEvent.ACTION_MOVE:
                if (pressed && !onButton) { pressed = false; invalidate(); }
                return true;
            case MotionEvent.ACTION_UP: {
                final boolean wasPressed = pressed;
                pressed = false;
                invalidate();
                if (onGear) {
                    performClick();
                    if (settingsListener != null) settingsListener.onSettingsTap();
                } else if (wasPressed && onButton) {
                    performClick();
                    if (listener != null) listener.onPowerTap();
                }
                return true;
            }
            case MotionEvent.ACTION_CANCEL:
                pressed = false;
                invalidate();
                return true;
            default:
                return super.onTouchEvent(e);
        }
    }

    @Override public boolean performClick() { return super.performClick(); }
}
