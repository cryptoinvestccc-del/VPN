package vpn.besy;

import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Reads the parts of the provisioning reply that the tunnel is built
 * from.
 *
 * <p>Separate from the service that uses it so it can be run without a
 * phone. The reason is specific: the DNS field was once read as a
 * string while the server sent a list, and what reached Android was the
 * text {@code ["1.1.1.1"]}, brackets and quotes included. It refused
 * that, correctly, as not being an address. Nothing here needs Android,
 * so nothing here has to wait for a phone to be checked.
 */
final class Reply {

    private Reply() {}

    /** The name servers to hand the system, in the order given. */
    static java.util.List<String> dnsServers(JSONObject issued) {
        java.util.List<String> out = new java.util.ArrayList<String>();

        JSONArray list = issued.optJSONArray("dns");
        if (list != null) {
            for (int i = 0; i < list.length(); i++) {
                add(out, list.optString(i, ""));
            }
            return out;
        }

        // A single address, or several separated by commas, is what an
        // older server sent and what a hand-written one might send.
        for (String server : issued.optString("dns", "").split(",")) {
            add(out, server);
        }
        return out;
    }

    private static void add(java.util.List<String> out, String server) {
        server = server.trim();
        if (server.isEmpty()) return;
        // Whatever arrives, it goes to a call that takes an address and
        // nothing else. A value carrying the punctuation of the format
        // it travelled in is not an address.
        if (server.indexOf('[') >= 0 || server.indexOf('"') >= 0) return;
        out.add(server);
    }
}
