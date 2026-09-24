import org.json.JSONObject;
import java.lang.reflect.Method;

/**
 * Runs the app's own networking and reply parsing against a server,
 * and prints what the tunnel would be built from.
 *
 *   ReplyCheck <endpoint> <publicKeyBase64>
 */
public class ReplyCheck {
    public static void main(String[] a) throws Exception {
        Class<?> prov = Class.forName("vpn.besy.Provisioning");
        Method issue = prov.getDeclaredMethod("issue", String.class, String.class);
        issue.setAccessible(true);
        JSONObject reply = (JSONObject) issue.invoke(null, a[0], a[1]);

        Class<?> r = Class.forName("vpn.besy.Reply");
        Method dns = r.getDeclaredMethod("dnsServers", JSONObject.class);
        dns.setAccessible(true);
        Object servers = dns.invoke(null, reply);

        JSONObject out = new JSONObject();
        out.put("address", reply.optString("address"));
        out.put("endpoint", reply.optString("endpoint"));
        out.put("server_public_key", reply.optString("server_public_key"));
        out.put("dns", servers);
        System.out.println(out.toString());
    }
}
