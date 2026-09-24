package vpn.besy;

import android.util.Base64;

import org.json.JSONObject;

/**
 * Turns what the provisioning service hands back into the text the
 * tunnel engine expects.
 *
 * <p>Two traps live here. Keys travel as base64 everywhere a person sees
 * them and as hex everywhere the engine reads them, and a base64 key
 * passed through unconverted is accepted as a string and then fails as a
 * handshake that is never answered. And the obfuscation parameters must
 * be omitted rather than sent as zero: the engine rejects <code>jc=0</code>
 * outright, so a server that does not use junk packets would make the
 * whole configuration invalid if the field were written anyway.
 */
final class Uapi {

    private final StringBuilder out = new StringBuilder();

    private Uapi() {}

    /**
     * Builds the configuration for one tunnel.
     *
     * @param privateKeyBase64 this device's own key, which never leaves it
     * @param issued           the reply from the provisioning service
     */
    static String build(String privateKeyBase64, JSONObject issued) throws Exception {
        Uapi u = new Uapi();

        u.line("private_key", hex(privateKeyBase64));
        u.line("listen_port", "0");
        u.line("replace_peers", "true");

        JSONObject awg = issued.optJSONObject("awg");
        if (awg != null) {
            // Positive-only, in the order the engine documents them.
            u.positive(awg, "jc");
            u.positive(awg, "jmin");
            u.positive(awg, "jmax");
            u.positive(awg, "s1");
            u.positive(awg, "s2");
            u.positive(awg, "s3");
            u.positive(awg, "s4");
            // Headers are required together or not at all: they replace
            // WireGuard's four message types, and a partial set makes a
            // packet the server cannot classify.
            if (hasAll(awg, "h1", "h2", "h3", "h4")) {
                u.positive(awg, "h1");
                u.positive(awg, "h2");
                u.positive(awg, "h3");
                u.positive(awg, "h4");
            }
            for (int i = 1; i <= 5; i++) {
                u.positive(awg, "i" + i);
            }
        }

        u.line("public_key", hex(issued.getString("server_public_key")));
        u.line("endpoint", issued.getString("endpoint"));

        String allowed = issued.optString("allowed_ips", "0.0.0.0/0, ::/0");
        for (String entry : allowed.split(",")) {
            entry = entry.trim();
            if (entry.length() > 0) {
                u.line("allowed_ip", entry);
            }
        }

        int keepalive = issued.optInt("keepalive", 0);
        if (keepalive > 0) {
            u.line("persistent_keepalive_interval", Integer.toString(keepalive));
        }

        return u.out.toString();
    }

    private static boolean hasAll(JSONObject o, String... keys) {
        for (String k : keys) {
            if (o.optString(k, "").length() == 0) return false;
        }
        return true;
    }

    /** Writes a parameter only when it carries a usable value. */
    private void positive(JSONObject o, String key) {
        String raw = o.optString(key, "");
        if (raw.length() == 0) return;
        try {
            if (Long.parseLong(raw.trim()) <= 0) return;
        } catch (NumberFormatException e) {
            return;
        }
        line(key, raw.trim());
    }

    private void line(String key, String value) {
        out.append(key).append('=').append(value).append('\n');
    }

    /** base64 as people write keys, hex as the engine reads them. */
    static String hex(String base64) {
        byte[] raw = Base64.decode(base64, Base64.DEFAULT);
        StringBuilder sb = new StringBuilder(raw.length * 2);
        for (byte b : raw) {
            sb.append(Character.forDigit((b >> 4) & 0xF, 16));
            sb.append(Character.forDigit(b & 0xF, 16));
        }
        return sb.toString();
    }
}
