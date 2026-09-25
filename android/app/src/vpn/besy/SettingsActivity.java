package vpn.besy;

import android.app.Activity;
import android.app.AlertDialog;
import android.app.ProgressDialog;
import android.content.Context;
import android.content.DialogInterface;
import android.content.Intent;
import android.content.res.ColorStateList;
import android.os.Bundle;
import android.provider.Settings;
import android.view.View;
import android.widget.CompoundButton;
import android.widget.LinearLayout;
import android.widget.Switch;

/**
 * Settings: what the mockup agreed, and nothing it did not.
 *
 * <p>No server choice and no protocol choice, because there is one of
 * each. No traffic history, because a history of when the tunnel was
 * used is a log of who was online when.
 */
public final class SettingsActivity extends Activity {

    private static final int DIALOG = android.R.style.Theme_DeviceDefault_Dialog_Alert;

    @Override protected void attachBaseContext(Context base) {
        super.attachBaseContext(Lang.wrap(base));
    }

    @Override protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        build();
    }

    private void build() {
        LinearLayout col = Ui.screen(this, getString(R.string.settings));

        // App
        col.addView(Ui.section(this, getString(R.string.section_app)));
        LinearLayout app = Ui.card(this);

        LinearLayout lang = Ui.row(this, getString(R.string.language), null,
                Ui.value(this, languageName(Prefs.language(this))));
        Ui.clickable(lang, new View.OnClickListener() {
            @Override public void onClick(View v) { chooseLanguage(); }
        });
        app.addView(lang);
        Ui.divider(this, app);

        final Switch auto = new Switch(this);
        auto.setChecked(Prefs.autoConnect(this));
        auto.setThumbTintList(new ColorStateList(
                new int[][] { { android.R.attr.state_checked }, {} },
                new int[] { Palette.LIVE, Palette.INK_2 }));
        auto.setTrackTintList(new ColorStateList(
                new int[][] { { android.R.attr.state_checked }, {} },
                new int[] { 0x806FE3C0, 0x40FFFFFF }));
        auto.setOnCheckedChangeListener(new CompoundButton.OnCheckedChangeListener() {
            @Override public void onCheckedChanged(CompoundButton b, boolean on) {
                Prefs.setAutoConnect(SettingsActivity.this, on);
            }
        });
        LinearLayout autoRow = Ui.row(this, getString(R.string.autoconnect),
                getString(R.string.autoconnect_sub), auto);
        // The whole row toggles, not only the small switch.
        Ui.clickable(autoRow, new View.OnClickListener() {
            @Override public void onClick(View v) { auto.toggle(); }
        });
        app.addView(autoRow);
        Ui.divider(this, app);

        LinearLayout always = Ui.row(this, getString(R.string.always_on),
                getString(R.string.always_on_sub), Ui.chevron(this));
        Ui.clickable(always, new View.OnClickListener() {
            @Override public void onClick(View v) {
                // Android's own switch: the app cannot set this itself,
                // and the system's screen is where people expect it.
                try {
                    startActivity(new Intent(Settings.ACTION_VPN_SETTINGS));
                } catch (Exception e) {
                    startActivity(new Intent(Settings.ACTION_SETTINGS));
                }
            }
        });
        app.addView(always);
        col.addView(app);

        // Legal
        col.addView(Ui.section(this, getString(R.string.section_legal)));
        LinearLayout legal = Ui.card(this);
        legal.addView(docRow(R.string.privacy, R.raw.privacy));
        Ui.divider(this, legal);
        legal.addView(docRow(R.string.terms, R.raw.terms));
        Ui.divider(this, legal);
        legal.addView(docRow(R.string.licenses, R.raw.licenses));
        col.addView(legal);

        // Data
        col.addView(Ui.section(this, getString(R.string.section_data)));
        LinearLayout data = Ui.card(this);
        LinearLayout delete = Ui.row(this, getString(R.string.delete),
                getString(R.string.delete_sub), null);
        ((android.widget.TextView) ((LinearLayout) delete.getChildAt(0)).getChildAt(0))
                .setTextColor(Palette.EMBER);
        Ui.clickable(delete, new View.OnClickListener() {
            @Override public void onClick(View v) { confirmDelete(); }
        });
        data.addView(delete);
        col.addView(data);

        // About
        col.addView(Ui.section(this, getString(R.string.section_about)));
        LinearLayout about = Ui.card(this);
        about.addView(Ui.row(this, getString(R.string.version), null, Ui.value(this, version())));
        Ui.divider(this, about);
        about.addView(Ui.row(this, getString(R.string.server), null,
                Ui.value(this, getString(R.string.server_host))));
        Ui.divider(this, about);
        LinearLayout contact = Ui.row(this, getString(R.string.contact),
                getString(R.string.contact_email), Ui.chevron(this));
        Ui.clickable(contact, new View.OnClickListener() {
            @Override public void onClick(View v) {
                Intent mail = new Intent(Intent.ACTION_SENDTO,
                        android.net.Uri.parse("mailto:" + getString(R.string.contact_email)))
                        .putExtra(Intent.EXTRA_SUBJECT, "BESY " + version());
                try {
                    startActivity(mail);
                } catch (Exception e) {
                    // No mail app: the address is on the row itself.
                }
            }
        });
        about.addView(contact);
        col.addView(about);
    }

    private LinearLayout docRow(final int title, final int text) {
        LinearLayout row = Ui.row(this, getString(title), null, Ui.chevron(this));
        Ui.clickable(row, new View.OnClickListener() {
            @Override public void onClick(View v) {
                startActivity(new Intent(SettingsActivity.this, DocActivity.class)
                        .putExtra(DocActivity.EXTRA_TITLE, title)
                        .putExtra(DocActivity.EXTRA_TEXT, text));
            }
        });
        return row;
    }

    private static final String[] LANGS = { Prefs.LANG_SYSTEM, "ru", "en" };

    private String languageName(String code) {
        if ("ru".equals(code)) return getString(R.string.lang_ru);
        if ("en".equals(code)) return getString(R.string.lang_en);
        return getString(R.string.lang_system);
    }

    private void chooseLanguage() {
        String current = Prefs.language(this);
        int checked = 0;
        String[] names = new String[LANGS.length];
        for (int i = 0; i < LANGS.length; i++) {
            names[i] = languageName(LANGS[i]);
            if (LANGS[i].equals(current)) checked = i;
        }
        new AlertDialog.Builder(this, DIALOG)
                .setTitle(R.string.language)
                .setSingleChoiceItems(names, checked, new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int which) {
                        d.dismiss();
                        if (!LANGS[which].equals(Prefs.language(SettingsActivity.this))) {
                            Prefs.setLanguage(SettingsActivity.this, LANGS[which]);
                            // This screen now, the main one when it
                            // returns: it checks the language on resume.
                            recreate();
                        }
                    }
                })
                .setNegativeButton(R.string.cancel, null)
                .show();
    }

    private void confirmDelete() {
        new AlertDialog.Builder(this, DIALOG)
                .setTitle(R.string.delete_confirm_title)
                .setMessage(R.string.delete_confirm_body)
                .setPositiveButton(R.string.delete_yes, new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) { delete(); }
                })
                .setNegativeButton(R.string.cancel, null)
                .show();
    }

    /**
     * Stops the tunnel, asks the server to remove the peer, then clears
     * the key here — in that order, because the request needs the key
     * and the token, and afterwards nothing on the phone should.
     */
    private void delete() {
        @SuppressWarnings("deprecation")
        final ProgressDialog wait = ProgressDialog.show(this, null, getString(R.string.deleting), true, false);
        final Context app = getApplicationContext();
        final String endpoint = Provisioning.endpoint(this);

        if (TunnelService.state != GlassView.STATE_OFF) {
            startService(new Intent(this, TunnelService.class)
                    .setAction(TunnelService.ACTION_DISCONNECT));
        }

        new Thread(new Runnable() {
            @Override public void run() {
                Keys keys = Keys.stored(app);
                final int result;
                if (keys == null) {
                    result = R.string.deleted_none;
                } else {
                    boolean server = Provisioning.forget(endpoint, keys.publicKey(), Keys.forgetToken(app));
                    Keys.forget(app);
                    result = server ? R.string.deleted_both : R.string.deleted_local;
                }
                runOnUiThread(new Runnable() {
                    @Override public void run() {
                        wait.dismiss();
                        if (isFinishing()) return;
                        new AlertDialog.Builder(SettingsActivity.this, DIALOG)
                                .setMessage(result)
                                .setPositiveButton(R.string.ok, null)
                                .show();
                    }
                });
            }
        }, "besy-forget").start();
    }

    @SuppressWarnings("deprecation")
    private String version() {
        try {
            android.content.pm.PackageInfo p = getPackageManager().getPackageInfo(getPackageName(), 0);
            return p.versionName + " (" + p.versionCode + ")";
        } catch (Exception e) {
            return "—";
        }
    }
}
