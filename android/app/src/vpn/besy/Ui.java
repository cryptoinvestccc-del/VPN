package vpn.besy;

import android.app.Activity;
import android.content.Context;
import android.content.res.ColorStateList;
import android.graphics.Typeface;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/**
 * The pieces the secondary screens are built from.
 *
 * <p>Ordinary Android views rather than a hand-drawn canvas. The main
 * screen is drawn because its design is light and glass; lists of
 * settings are what these widgets already do well, with scrolling, focus
 * and accessibility that would otherwise have to be written again.
 */
final class Ui {

    private Ui() {}

    static int dp(Context c, float v) {
        return Math.round(TypedValue.applyDimension(
                TypedValue.COMPLEX_UNIT_DIP, v, c.getResources().getDisplayMetrics()));
    }

    /** The light background of the app. */
    static GradientDrawable sky() {
        return new GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM,
                new int[] { Palette.VOID_, Palette.VOID_ });
    }

    /** A screen: scrolling column under a header with a back arrow. */
    static LinearLayout screen(final Activity a, String title) {
        ScrollView scroll = new ScrollView(a);
        scroll.setBackground(sky());
        scroll.setFillViewport(true);
        scroll.setFitsSystemWindows(true);

        LinearLayout col = new LinearLayout(a);
        col.setOrientation(LinearLayout.VERTICAL);
        col.setPadding(dp(a, 16), dp(a, 8), dp(a, 16), dp(a, 32));
        scroll.addView(col, new ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        LinearLayout header = new LinearLayout(a);
        header.setGravity(Gravity.CENTER_VERTICAL);

        TextView back = new TextView(a);
        back.setText("‹");
        back.setTextSize(30);
        back.setTextColor(Palette.INK);
        back.setGravity(Gravity.CENTER);
        back.setContentDescription(a.getString(R.string.back));
        back.setBackground(ripple(0));
        back.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { a.finish(); }
        });
        // 48dp: a finger's size, whatever the glyph's.
        header.addView(back, new LinearLayout.LayoutParams(dp(a, 48), dp(a, 48)));

        TextView t = new TextView(a);
        t.setText(title);
        t.setTextSize(22);
        t.setTextColor(Palette.INK);
        t.setTypeface(Typeface.create("sans-serif-light", Typeface.NORMAL));
        t.setPadding(dp(a, 4), 0, 0, 0);
        header.addView(t);

        col.addView(header);
        a.setContentView(scroll);
        return col;
    }

    static TextView section(Context c, String name) {
        TextView t = new TextView(c);
        t.setText(name.toUpperCase());
        t.setTextSize(11);
        t.setLetterSpacing(0.13f);
        t.setTextColor(Palette.INK_3);
        t.setPadding(dp(c, 8), dp(c, 22), 0, dp(c, 8));
        return t;
    }

    /** A rounded white card to put rows in. */
    static LinearLayout card(Context c) {
        LinearLayout card = new LinearLayout(c);
        card.setOrientation(LinearLayout.VERTICAL);
        GradientDrawable bg = new GradientDrawable();
        bg.setColor(Palette.SURFACE);
        bg.setCornerRadius(dp(c, 18));
        bg.setStroke(Math.max(1, dp(c, 1)), Palette.RIM_SOFT);
        card.setBackground(bg);
        card.setClipToOutline(true);
        return card;
    }

    static RippleDrawable ripple(int radius) {
        GradientDrawable mask = new GradientDrawable();
        mask.setColor(0xFFFFFFFF);
        mask.setCornerRadius(radius);
        return new RippleDrawable(ColorStateList.valueOf(0x1A111318), null, mask);
    }

    /**
     * A row: title and optional line under it on the left, an optional
     * value or control on the right.
     */
    static LinearLayout row(Context c, String title, String sub, View right) {
        LinearLayout row = new LinearLayout(c);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setMinimumHeight(dp(c, 56));
        row.setPadding(dp(c, 16), dp(c, 10), dp(c, 16), dp(c, 10));

        LinearLayout text = new LinearLayout(c);
        text.setOrientation(LinearLayout.VERTICAL);
        TextView t = new TextView(c);
        t.setText(title);
        t.setTextSize(16);
        t.setTextColor(Palette.INK);
        text.addView(t);
        if (sub != null) {
            TextView s = new TextView(c);
            s.setText(sub);
            s.setTextSize(12.5f);
            s.setTextColor(Palette.INK_3);
            s.setPadding(0, dp(c, 2), 0, 0);
            text.addView(s);
        }
        row.addView(text, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f));
        if (right != null) {
            LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            lp.leftMargin = dp(c, 12);
            row.addView(right, lp);
        }
        return row;
    }

    static TextView value(Context c, String v) {
        TextView t = new TextView(c);
        t.setText(v);
        t.setTextSize(14);
        t.setTextColor(Palette.INK_2);
        return t;
    }

    static TextView chevron(Context c) {
        TextView t = value(c, "›");
        t.setTextSize(20);
        t.setTextColor(Palette.INK_3);
        return t;
    }

    static void divider(Context c, LinearLayout card) {
        View d = new View(c);
        d.setBackground(new ColorDrawable(Palette.RIM_SOFT));
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, Math.max(1, dp(c, 1) / 2));
        lp.leftMargin = dp(c, 16);
        card.addView(d, lp);
    }

    static void clickable(View v, View.OnClickListener l) {
        v.setBackground(ripple(0));
        v.setClickable(true);
        v.setFocusable(true);
        v.setOnClickListener(l);
    }
}
