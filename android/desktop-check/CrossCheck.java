import org.json.JSONObject;
import java.nio.file.*;

/** Prints what the app would hand the engine, for the reply on stdin. */
public class CrossCheck {
    public static void main(String[] a) throws Exception {
        String json = new String(Files.readAllBytes(Paths.get(a[1])), "UTF-8");
        java.lang.reflect.Method m = Class.forName("vpn.besy.Uapi")
            .getDeclaredMethod("build", String.class, JSONObject.class);
        m.setAccessible(true);
        System.out.print((String) m.invoke(null, a[0], new JSONObject(json)));
    }
}
