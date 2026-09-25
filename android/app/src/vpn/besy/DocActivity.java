package vpn.besy;

import android.app.Activity;
import android.content.Context;
import android.os.Bundle;
import android.text.method.LinkMovementMethod;
import android.text.util.Linkify;
import android.widget.LinearLayout;
import android.widget.TextView;

import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.nio.charset.Charset;

/**
 * A document inside the app: the privacy policy, the terms, the
 * licences. Shipped with it rather than fetched, so each is readable
 * offline and is exactly the version that came with this build.
 */
public final class DocActivity extends Activity {

    static final String EXTRA_TITLE = "title";
    static final String EXTRA_TEXT  = "text";

    @Override protected void attachBaseContext(Context base) {
        super.attachBaseContext(Lang.wrap(base));
    }

    @Override protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        int title = getIntent().getIntExtra(EXTRA_TITLE, R.string.app_name);
        int text  = getIntent().getIntExtra(EXTRA_TEXT, 0);

        LinearLayout col = Ui.screen(this, getString(title));

        TextView body = new TextView(this);
        body.setText(read(text));
        body.setTextSize(14.5f);
        body.setTextColor(Palette.INK_2);
        body.setLineSpacing(0, 1.3f);
        body.setTextIsSelectable(true);
        body.setPadding(Ui.dp(this, 8), Ui.dp(this, 12), Ui.dp(this, 8), 0);
        Linkify.addLinks(body, Linkify.WEB_URLS);
        body.setMovementMethod(LinkMovementMethod.getInstance());
        body.setLinkTextColor(Palette.LIVE);
        col.addView(body);
    }

    private String read(int id) {
        if (id == 0) return "";
        StringBuilder sb = new StringBuilder();
        try {
            InputStream in = getResources().openRawResource(id);
            BufferedReader r = new BufferedReader(new InputStreamReader(in, Charset.forName("UTF-8")));
            try {
                String line;
                while ((line = r.readLine()) != null) sb.append(line).append('\n');
            } finally {
                r.close();
            }
        } catch (Exception e) {
            return "";
        }
        return sb.toString().trim();
    }
}
