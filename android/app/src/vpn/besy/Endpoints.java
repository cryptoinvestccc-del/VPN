package vpn.besy;

import java.io.IOException;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.UnknownHostException;

/**
 * Turns the server's address into one the engine will accept.
 *
 * <p>The engine takes a literal address and a port, never a name: its
 * parser is {@code netip.ParseAddrPort}, which refuses anything that is
 * not already numeric. A phone reported exactly that —
 * {@code ParseAddr("besyvpn.online"): unexpected character}.
 *
 * <p>The name has to be resolved on this side rather than in the engine,
 * and not by preference. The engine is a static Go binary built without
 * cgo, so its resolver is Go's own, which reads {@code /etc/resolv.conf}.
 * Android has no such file — name lookup there goes through the system
 * resolver, which is reachable from Java and not from a bare binary. So
 * this is the only place the lookup can happen.
 *
 * <p>The lookup runs before the tunnel is established and the app
 * excludes itself from its own tunnel, so it goes out over the ordinary
 * network either way.
 */
final class Endpoints {

    private Endpoints() {}

    /**
     * Returns {@code host:port} with the host as a literal address.
     *
     * <p>An address that is already literal is returned untouched, so a
     * deployment that hands out an address costs no lookup.
     */
    static String resolve(String endpoint) throws IOException {
        if (endpoint == null || endpoint.trim().isEmpty()) {
            throw new IOException("the server did not say where to connect");
        }
        endpoint = endpoint.trim();

        String host;
        String port;
        if (endpoint.startsWith("[")) {
            // [2001:db8::1]:51820 — already literal, and the brackets
            // are how the engine wants to read it.
            int close = endpoint.indexOf(']');
            if (close < 0 || close + 2 > endpoint.length() || endpoint.charAt(close + 1) != ':') {
                throw new IOException("cannot read the server address: " + endpoint);
            }
            return endpoint;
        }
        int colon = endpoint.lastIndexOf(':');
        if (colon < 0) {
            throw new IOException("the server address has no port: " + endpoint);
        }
        host = endpoint.substring(0, colon).trim();
        port = endpoint.substring(colon + 1).trim();
        if (host.isEmpty() || port.isEmpty()) {
            throw new IOException("cannot read the server address: " + endpoint);
        }
        if (host.indexOf(':') >= 0) {
            // A bare IPv6 address with a port cannot be told apart from
            // an address without one, and the engine wants brackets.
            return "[" + host + "]:" + port;
        }
        if (isIPv4Literal(host)) {
            return host + ":" + port;
        }

        InetAddress chosen = firstUsable(host);
        if (chosen instanceof Inet4Address) {
            return chosen.getHostAddress() + ":" + port;
        }
        return "[" + chosen.getHostAddress() + "]:" + port;
    }

    /**
     * Prefers IPv4, because a phone that has an IPv6 address is not
     * necessarily on a network that can carry IPv6 to this server, while
     * one that resolved an A record can reach it.
     */
    private static InetAddress firstUsable(String host) throws IOException {
        InetAddress[] found;
        try {
            found = InetAddress.getAllByName(host);
        } catch (UnknownHostException e) {
            throw new IOException("could not look up " + host, e);
        }
        if (found.length == 0) {
            throw new IOException("could not look up " + host);
        }
        for (InetAddress a : found) {
            if (a instanceof Inet4Address) return a;
        }
        return found[0];
    }

    /** True for text that is already a dotted-quad address. */
    private static boolean isIPv4Literal(String host) {
        int parts = 0;
        for (String part : host.split("\\.", -1)) {
            parts++;
            if (part.isEmpty() || part.length() > 3) return false;
            for (int i = 0; i < part.length(); i++) {
                if (part.charAt(i) < '0' || part.charAt(i) > '9') return false;
            }
            if (Integer.parseInt(part) > 255) return false;
        }
        return parts == 4;
    }
}
