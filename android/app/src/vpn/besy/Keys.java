package vpn.besy;

import android.content.Context;
import android.content.SharedPreferences;

import java.io.BufferedReader;
import java.io.File;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.Charset;

/**
 * This device's keypair, made on this device.
 *
 * <p>The generation is done by the tunnel engine rather than here, for
 * one reason: it already contains the curve, and a second implementation
 * of key handling in Java would be a second place to get it wrong. Java
 * sees the private key only as a string on its way into a pipe.
 *
 * <p>The key is kept once and reused. Making a new one on every launch
 * would work, but each new key is a new peer on the server, and peers
 * accumulate until they expire.
 */
final class Keys {

    private static final String PREFS   = "besy";
    private static final String PRIVATE = "private_key";
    private static final String PUBLIC  = "public_key";
    private static final String TOKEN   = "forget_token";

    private final String privateKey;
    private final String publicKey;

    private Keys(String priv, String pub) {
        this.privateKey = priv;
        this.publicKey = pub;
    }

    String privateKey() { return privateKey; }
    String publicKey()  { return publicKey; }

    /** Returns the stored pair, generating one the first time. */
    static Keys load(Context context) throws IOException {
        SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        String priv = prefs.getString(PRIVATE, null);
        String pub  = prefs.getString(PUBLIC, null);
        if (priv != null && pub != null) {
            return new Keys(priv, pub);
        }

        Keys made = generate(context);
        // Written now, not eventually. apply() returns before the file
        // is on disk, and a process killed in that window comes back
        // without a key, generates another one, and leaves the old peer
        // on the server to sit there until it expires. This runs off the
        // main thread, so waiting for the write costs nothing.
        if (!prefs.edit()
                .putString(PRIVATE, made.privateKey)
                .putString(PUBLIC, made.publicKey)
                .commit()) {
            throw new IOException("the key could not be stored");
        }
        return made;
    }

    /** The stored pair, or null if none has been made yet. Never makes one. */
    static Keys stored(Context context) {
        SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        String priv = prefs.getString(PRIVATE, null);
        String pub  = prefs.getString(PUBLIC, null);
        return priv != null && pub != null ? new Keys(priv, pub) : null;
    }

    /**
     * Keeps the secret for removing this device's credential.
     *
     * <p>The server sends it once, in the reply that creates the peer, and
     * never again: asked a second time with the same key, it cannot tell
     * this device from someone who has merely seen the public key. So an
     * empty value never overwrites a stored one.
     */
    static void saveForgetToken(Context context, String token) {
        if (token == null || token.length() == 0) return;
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .edit().putString(TOKEN, token).commit();
    }

    static String forgetToken(Context context) {
        return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(TOKEN, null);
    }

    /** Forgets the key, so the next connection starts as a new device. */
    static void forget(Context context) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().clear().commit();
    }

    private static Keys generate(Context context) throws IOException {
        File engine = Engine.binary(context);
        ProcessBuilder pb = new ProcessBuilder(engine.getAbsolutePath(), "genkey");
        pb.redirectErrorStream(false);
        Process p = pb.start();

        String priv, pub;
        BufferedReader r = new BufferedReader(
                new InputStreamReader(p.getInputStream(), Charset.forName("UTF-8")));
        try {
            priv = r.readLine();
            pub  = r.readLine();
        } finally {
            r.close();
        }

        try {
            if (p.waitFor() != 0) {
                throw new IOException("the engine could not generate a key");
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("interrupted while generating a key", e);
        }

        if (priv == null || pub == null || priv.length() < 40 || pub.length() < 40) {
            throw new IOException("the engine returned an unusable key");
        }
        return new Keys(priv, pub);
    }
}
