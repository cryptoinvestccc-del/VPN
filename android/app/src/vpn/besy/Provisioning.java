package vpn.besy;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.Charset;

/**
 * Asks the server for this device's own configuration.
 *
 * <p>What is sent is one public key and nothing else. There is no field
 * for a name, a device identifier or an address, and the server refuses
 * a request that carries one, so neither side can start collecting them
 * without the other noticing.
 */
final class Provisioning {

    /**
     * Where the app asks, taken from a resource so a deployment changes
     * one line of XML instead of a line of Java somebody has to find.
     */
    static String endpoint(android.content.Context context) {
        return context.getString(R.string.provision_endpoint);
    }

    private static final int CONNECT_TIMEOUT_MS = 10_000;
    private static final int READ_TIMEOUT_MS    = 15_000;
    private static final int MAX_REPLY_BYTES    = 64 * 1024;

    private Provisioning() {}

    /**
     * Sends the public half and returns the reply.
     *
     * <p>Asking twice with the same key returns the same address rather
     * than a second one, so a retry after a lost reply costs nothing.
     */
    static JSONObject issue(String endpoint, String publicKeyBase64) throws IOException {
        HttpURLConnection http = (HttpURLConnection) new URL(endpoint).openConnection();
        try {
            http.setRequestMethod("POST");
            http.setConnectTimeout(CONNECT_TIMEOUT_MS);
            http.setReadTimeout(READ_TIMEOUT_MS);
            http.setDoOutput(true);
            http.setUseCaches(false);
            http.setRequestProperty("Content-Type", "application/json; charset=utf-8");
            http.setRequestProperty("Accept", "application/json");

            JSONObject body = new JSONObject();
            try {
                body.put("public_key", publicKeyBase64);
            } catch (Exception e) {
                throw new IOException("could not build the request", e);
            }

            OutputStream out = http.getOutputStream();
            try {
                out.write(body.toString().getBytes(Charset.forName("UTF-8")));
            } finally {
                out.close();
            }

            int status = http.getResponseCode();
            String text = read(status >= 400 ? http.getErrorStream() : http.getInputStream());

            if (status == 503) {
                // Full, not broken. Worth saying differently, because one
                // is worth retrying later and the other is not.
                throw new IOException("the server is full; try again later");
            }
            if (status != 200) {
                throw new IOException("the server refused the request (" + status + ")");
            }

            try {
                return new JSONObject(text);
            } catch (Exception e) {
                throw new IOException("the server's reply was not the configuration we expected", e);
            }
        } finally {
            http.disconnect();
        }
    }

    private static String read(InputStream in) throws IOException {
        if (in == null) return "";
        StringBuilder sb = new StringBuilder();
        BufferedReader r = new BufferedReader(new InputStreamReader(in, Charset.forName("UTF-8")));
        try {
            char[] buf = new char[4096];
            int n;
            while ((n = r.read(buf)) > 0) {
                sb.append(buf, 0, n);
                if (sb.length() > MAX_REPLY_BYTES) {
                    throw new IOException("the reply is far larger than a configuration");
                }
            }
        } finally {
            r.close();
        }
        return sb.toString();
    }
}
