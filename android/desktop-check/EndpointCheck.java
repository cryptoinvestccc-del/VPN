import java.lang.reflect.Method;

/** Prints what the app would give the engine as the server address. */
public class EndpointCheck {
    public static void main(String[] a) throws Exception {
        Method m = Class.forName("vpn.besy.Endpoints")
                .getDeclaredMethod("resolve", String.class);
        m.setAccessible(true);
        try {
            System.out.println(m.invoke(null, a[0]));
        } catch (java.lang.reflect.InvocationTargetException e) {
            System.out.println("REFUSED: " + e.getCause().getMessage());
        }
    }
}
