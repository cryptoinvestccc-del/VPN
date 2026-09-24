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

    private float buttonCx, buttonCy, buttonR;

    GlassView(Context c) {
        super(c);
        setClickable(true);
        stroke.setStyle(Paint.Style.STROKE);
        text.setTextAlign(Paint.Align.LEFT);
    }

    void setOnPowerTap(OnPowerTap l) { listener = l; }

    void setState(int s) {
        if (state != s) { state = s; invalidate(); }
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

    @Override public boolean onTouchEvent(MotionEvent e) {
        if (e.getAction() == MotionEvent.ACTION_UP) {
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
