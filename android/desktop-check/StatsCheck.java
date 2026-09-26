package vpn.besy;

/**
 * Runs the tunnel service's own parsers for the tiles under the button:
 * the byte counters in the engine's "stat" line and the host of the
 * endpoint the app resolved, which is the address the internet sees.
 *
 * <p>Needs the R class aapt generates, so it runs after android/build.sh:
 * javac -d out -cp .toolchain/android.jar app/src/vpn/besy/*.java
 *   .build/gen/vpn/besy/R.java desktop-check/StatsCheck.java
 */
public final class StatsCheck {
    public static void main(String[] args) {
        String line = "stat handshake=1790415679 rx=155189248 tx=12582912";
        same(TunnelService.statField(line, "rx"), 155189248L, "rx");
        same(TunnelService.statField(line, "tx"), 12582912L, "tx");
        same(TunnelService.statField(line, "handshake"), 1790415679L, "handshake");
        same(TunnelService.statField("stat handshake=0", "rx"), -1L, "missing rx");
        same(TunnelService.statField("stat rx=abc tx=1", "rx"), -1L, "unreadable rx");
        same(TunnelService.statHandshake(line), 1790415679L, "statHandshake still works");

        same(TunnelService.hostOf("203.0.113.7:36635"), "203.0.113.7", "IPv4");
        same(TunnelService.hostOf("[2001:db8::1]:36635"), "2001:db8::1", "IPv6");
        same(TunnelService.hostOf(null), null, "null");
        System.out.println("StatsCheck: all passed");
    }

    private static void same(Object got, Object want, String what) {
        if (got == null ? want != null : !got.equals(want)) {
            throw new AssertionError(what + ": got " + got + ", want " + want);
        }
    }
}
