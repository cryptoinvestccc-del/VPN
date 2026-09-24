package android.util;
/** Desktop stand-in: android.jar's own Base64 only throws. */
public final class Base64 {
    public static final int DEFAULT = 0;
    public static final int NO_WRAP = 2;
    public static byte[] decode(String s, int flags) { return java.util.Base64.getDecoder().decode(s.trim()); }
    public static String encodeToString(byte[] b, int flags) { return java.util.Base64.getEncoder().encodeToString(b); }
}
