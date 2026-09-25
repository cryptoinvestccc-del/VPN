import org.json.JSONObject;
import java.lang.reflect.Method;

/**
 * Runs the app's own account code against a server:
 *   AccountCheck <issueEndpoint> <publicKey>
 * issues, reads the status, forgets with the token, forgets again with
 * a wrong token, and prints one line per step.
 */
public class AccountCheck {
    public static void main(String[] a) throws Exception {
        Class<?> p = Class.forName("vpn.besy.Provisioning");
        Method issue = p.getDeclaredMethod("issue", String.class, String.class);
        Method connected = p.getDeclaredMethod("connected", String.class);
        Method forget = p.getDeclaredMethod("forget", String.class, String.class, String.class);
        for (Method m : new Method[] { issue, connected, forget }) m.setAccessible(true);

        JSONObject reply = (JSONObject) issue.invoke(null, a[0], a[1]);
        String token = reply.optString("forget_token", "");
        System.out.println("token " + (token.isEmpty() ? "absent" : "present"));

        JSONObject again = (JSONObject) issue.invoke(null, a[0], a[1]);
        System.out.println("token-on-repeat " + (again.optString("forget_token", "").isEmpty() ? "absent" : "present"));

        System.out.println("connected " + connected.invoke(null, a[0]));
        System.out.println("forget-wrong " + forget.invoke(null, a[0], a[1], "wrong-token"));
        System.out.println("forget-right " + forget.invoke(null, a[0], a[1], token));
        System.out.println("forget-again " + forget.invoke(null, a[0], a[1], token));
    }
}
