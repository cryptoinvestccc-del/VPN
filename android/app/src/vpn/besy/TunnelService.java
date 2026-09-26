package vpn.besy;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.net.LocalServerSocket;
import android.net.LocalSocket;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileDescriptor;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.Charset;
import java.util.Map;

/**
 * Carries the tunnel.
 *
 * <p>The sequence is: ask the system for an interface, ask the server for
 * a configuration, then hand both to the engine. The order matters —
 * the interface has to exist before the engine starts, because the
 * descriptor is the one thing an app cannot make for itself.
 */
public final class TunnelService extends VpnService {

    static final String ACTION_CONNECT    = "vpn.besy.CONNECT";
    static final String ACTION_DISCONNECT = "vpn.besy.DISCONNECT";

    private static final String TAG = "besy";
    private static final String CHANNEL = "tunnel";
    private static final int NOTIFICATION_ID = 1;

    /** What the app shows; read from the main thread. */
    static volatile int state = GlassView.STATE_OFF;
    static volatile String lastError;

    /** The tunnel address, for the screen; null when there is no tunnel. */
    static volatile String address;

    /**
     * When the last handshake happened, in seconds since the epoch, as
     * the engine reports it every few seconds. Zero means none yet.
     * Kept only for the screen and only while the tunnel is up.
     */
    static volatile long lastHandshake;

    /**
     * For the tiles under the button, and only while the tunnel is up:
     * when it came up (elapsedRealtime, ms; 0 = not up), the address the
     * internet sees (the server's), the byte counters the engine reports
     * every few seconds, and the rates worked out between two reports.
     */
    static volatile long connectedAt;
    static volatile String exitIp;
    static volatile long rxBytes, txBytes;
    static volatile double rxRate, txRate;
    private long statAt;

    @Override protected void attachBaseContext(android.content.Context base) {
        super.attachBaseContext(Lang.wrap(base));
    }

    /**
     * Why the tunnel is being stopped on purpose: null while nobody has
     * asked, empty when the person tapped the button, a sentence when
     * something else took it away.
     */
    private volatile String stopRequested;

    /**
     * How far connecting got.
     *
     * <p>A one-button app that fails has nothing else to say for itself,
     * and "nothing happened" is the least useful thing a screen can
     * show. Naming the step turns a silent failure into one somebody can
     * act on without a cable and a laptop.
     */
    static volatile String stage = "";

    private Thread worker;
    private Process engine;
    private ParcelFileDescriptor tun;

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        final String action = intent == null ? ACTION_CONNECT : String.valueOf(intent.getAction());
        if (ACTION_DISCONNECT.equals(action)) {
            stopRequested = "";
            stopTunnel();
            stopSelf();
            return START_NOT_STICKY;
        }
        startTunnel();
        return START_STICKY;
    }

    private synchronized void startTunnel() {
        if (worker != null) return;

        state = GlassView.STATE_BUSY;
        lastError = null;
        stopRequested = null;
        // Below Android 14 as it has always run, and as it runs on the
        // phone this was tested on. From 14 on, a foreground service must
        // qualify for its declared type at the moment it starts, and
        // systemExempted is not something to bet a crash on before the
        // tunnel even exists. The official WireGuard app never calls
        // startForeground at all: a VPN service the system has bound is
        // kept alive by the system. From 14 on this does the same.
        if (Build.VERSION.SDK_INT < 34) {
            startForeground(NOTIFICATION_ID, notification());
        }

        worker = new Thread(new Runnable() {
            @Override public void run() {
                try {
                    connect();
                } catch (Throwable t) {
                    // Stopping kills the engine, and the thread reading
                    // it then fails with "read interrupted". That is the
                    // stop working, not a fault, and it used to be shown
                    // on screen as one.
                    String reason = stopRequested;
                    if (reason != null) {
                        lastError = reason.isEmpty() ? null : reason;
                        state = GlassView.STATE_OFF;
                        stopTunnel();
                        return;
                    }
                    Log.e(TAG, "tunnel failed", t);
                    // Never depend on getMessage(): plenty of exceptions
                    // carry none, and the screen then said nothing at
                    // all — which is how this failure first looked.
                    lastError = describe(t);
                    state = GlassView.STATE_OFF;
                    stopTunnel();
                }
            }
        }, "besy-tunnel");
        worker.start();
    }

    /** A description that is never empty, whatever the exception. */
    private static String describe(Throwable t) {
        StringBuilder sb = new StringBuilder();
        if (stage != null && stage.length() > 0) {
            sb.append(stage).append(": ");
        }
        String message = t.getMessage();
        if (message != null && message.trim().length() > 0) {
            sb.append(message.trim());
        } else {
            sb.append(t.getClass().getSimpleName());
        }
        Throwable cause = t.getCause();
        if (cause != null && cause != t) {
            String c = cause.getMessage();
            sb.append(" (").append(c != null && c.length() > 0 ? c : cause.getClass().getSimpleName()).append(")");
        }
        return sb.toString();
    }

    private void connect() throws Exception {
        stage = getString(R.string.stage_key);
        Keys keys = Keys.load(this);
        stage = getString(R.string.stage_config);
        JSONObject issued = Provisioning.issue(Provisioning.endpoint(this), keys.publicKey());
        // Present only in the reply that created this device's peer.
        Keys.saveForgetToken(this, issued.optString("forget_token", ""));

        stage = getString(R.string.stage_interface);
        Builder builder = new Builder();
        builder.setSession("BESY");
        builder.setMtu(issued.optInt("mtu", 1280));

        String address = issued.getString("address");     // "10.8.1.42/32"
        String[] parts = address.split("/");
        builder.addAddress(parts[0], parts.length > 1 ? Integer.parseInt(parts[1]) : 32);
        TunnelService.address = parts[0];

        String allowed = issued.optString("allowed_ips", "0.0.0.0/0, ::/0");
        for (String route : allowed.split(",")) {
            route = route.trim();
            if (route.isEmpty()) continue;
            String[] r = route.split("/");
            builder.addRoute(r[0], r.length > 1 ? Integer.parseInt(r[1]) : 0);
        }

        for (String server : Reply.dnsServers(issued)) {
            builder.addDnsServer(server);
        }

        // The engine's own socket must not be sent through the tunnel it
        // is carrying. Android gives one call for this and it is the whole
        // answer on this platform: the routing loop that needs a host
        // route on a desktop simply cannot arise once the process is
        // excluded by name.
        builder.addDisallowedApplication(getPackageName());

        tun = builder.establish();
        if (tun == null) {
            throw new IOException("the system did not grant a tunnel interface");
        }

        // The engine takes a literal address; a name never reaches it.
        // This is also the last moment the lookup can happen over the
        // ordinary network in a way that is obvious from the code.
        stage = getString(R.string.stage_server);
        final String endpoint = Endpoints.resolve(issued.optString("endpoint"));
        issued.put("endpoint", endpoint);
        exitIp = hostOf(endpoint);

        stage = getString(R.string.stage_engine);
        String config = Uapi.build(keys.privateKey(), issued);

        // The descriptor cannot be handed over as a number. ProcessBuilder
        // closes every descriptor above the standard three in the child,
        // so whatever number this one has here names nothing over there —
        // which is exactly what a phone reported: the engine asked the
        // kernel about a descriptor it did not have, and was told so.
        //
        // A descriptor crosses a process boundary by being sent. Attached
        // to a message on a local socket, the kernel installs a copy in
        // the other process and gives it a number of its own choosing.
        // The configuration travels on the same socket, so the private
        // key never touches the filesystem and never appears in the
        // process table either.
        String socketName = "besy-tun-" + android.os.Process.myPid() + "-" + System.nanoTime();
        LocalServerSocket listener = new LocalServerSocket(socketName);
        try {
            File binary = Engine.binary(this);
            ProcessBuilder pb = new ProcessBuilder(binary.getAbsolutePath(), "run");
            Map<String, String> env = pb.environment();
            // The leading @ is how the abstract namespace is spelled
            // outside Android's own API.
            env.put("WG_TUN_SOCKET", "@" + socketName);
            pb.redirectErrorStream(true);

            engine = pb.start();

            LocalSocket peer = acceptOurOwn(listener);
            try {
                peer.setFileDescriptorsForSend(
                        new FileDescriptor[] { tun.getFileDescriptor() });
                OutputStream toEngine = peer.getOutputStream();
                // The attachment rides with the first write, so the
                // configuration and the descriptor arrive together.
                toEngine.write(config.getBytes(Charset.forName("UTF-8")));
                toEngine.flush();
                peer.setFileDescriptorsForSend(null);
                peer.shutdownOutput();
            } finally {
                peer.close();
            }
        } finally {
            listener.close();
        }

        // "ready" is printed once the tunnel is actually carrying
        // traffic, so the screen stops saying "connecting" on evidence
        // rather than on a timer.
        BufferedReader out = new BufferedReader(
                new InputStreamReader(engine.getInputStream(), Charset.forName("UTF-8")));

        // What the engine says is where the reason lives when it refuses
        // to start, and it was going only to logcat — which needs a
        // cable and a laptop to read. The last few lines are kept so the
        // screen can show them instead.
        java.util.ArrayDeque<String> said = new java.util.ArrayDeque<String>();

        String line;
        boolean up = false;
        while ((line = out.readLine()) != null) {
            line = line.trim();
            if (line.isEmpty()) continue;

            // The engine's periodic report: shown, never kept.
            if (line.startsWith("stat ")) {
                lastHandshake = statHandshake(line);
                countTraffic(statField(line, "rx"), statField(line, "tx"));
                continue;
            }

            if (!up && "ready".equals(line)) {
                // Only on the device's own log: one line saying the server
                // answered, so a test run can tell a working tunnel from
                // one that was switched off before the engine gave up.
                Log.i(TAG, "tunnel up: the server answered the handshake");
                up = true;
                stage = "";
                lastError = null;
                connectedAt = android.os.SystemClock.elapsedRealtime();
                rxBytes = txBytes = 0;
                rxRate = txRate = 0;
                statAt = 0;
                state = GlassView.STATE_LIVE;
                continue;
            }

            Log.i(TAG, "engine: " + line);
            said.addLast(line);
            while (said.size() > 3) {
                said.removeFirst();
            }
        }

        // The engine only returns when the tunnel is over. Reaching here
        // without ever seeing "ready" means it refused the configuration
        // and said why on the same stream.
        if (!up && stopRequested == null) {
            StringBuilder why = new StringBuilder(getString(R.string.stage_engine)).append(": ");
            if (said.isEmpty()) {
                why.append(getString(R.string.engine_silent));
            } else {
                boolean first = true;
                for (String s : said) {
                    if (!first) why.append(" · ");
                    why.append(s);
                    first = false;
                }
            }
            lastError = why.toString();
        }
        state = GlassView.STATE_OFF;
        stopTunnel();
    }

    /**
     * Reads the DNS servers, which arrive as a list.
     *
     * <p>They were read as a comma-separated string once, and the first
     * run on a phone put the literal {@code ["1.1.1.1"]} — brackets,
     * quotes and all — where an address belonged. The two sides of this
     * reply were written separately and drifted; a string is accepted
     * here as well so that a future server spelling it either way still
     * works.
     */
    /**
     * Accepts until the caller is this app, closing anyone else.
     *
     * <p>A socket in the abstract namespace has no owner and no
     * permissions: any app on the phone may connect to a name it can
     * guess. What travels on this one is the tunnel descriptor and the
     * private key, so the caller is checked before a single byte is
     * written. The kernel fills the credentials in when the connection
     * is made and neither side can forge them.
     */
    private static LocalSocket acceptOurOwn(LocalServerSocket listener) throws IOException {
        int mine = android.os.Process.myUid();
        for (int attempt = 0; attempt < 8; attempt++) {
            LocalSocket peer = listener.accept();
            int theirs;
            try {
                theirs = peer.getPeerCredentials().getUid();
            } catch (IOException e) {
                peer.close();
                continue;
            }
            if (theirs == mine) {
                return peer;
            }
            Log.w(TAG, "refused a connection from uid " + theirs);
            peer.close();
        }
        throw new IOException("something else on this phone kept answering for the engine");
    }


    private synchronized void stopTunnel() {
        if (engine != null) {
            engine.destroy();
            engine = null;
        }
        if (tun != null) {
            try { tun.close(); } catch (IOException ignored) { }
            tun = null;
        }
        worker = null;
        state = GlassView.STATE_OFF;
        address = null;
        lastHandshake = 0;
        connectedAt = 0;
        exitIp = null;
        rxBytes = txBytes = 0;
        rxRate = txRate = 0;
        stopForeground(true);
    }

    @Override public void onRevoke() {
        // The system, or another VPN app, took the interface away.
        stopRequested = getString(R.string.revoked);
        lastError = stopRequested;
        stopTunnel();
        stopSelf();
    }

    @Override public void onDestroy() {
        if (stopRequested == null) stopRequested = "";
        stopTunnel();
        super.onDestroy();
    }

    private Notification notification() {
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && nm != null
                && nm.getNotificationChannel(CHANNEL) == null) {
            NotificationChannel c = new NotificationChannel(
                    CHANNEL, getString(R.string.app_name), NotificationManager.IMPORTANCE_LOW);
            c.setShowBadge(false);
            nm.createNotificationChannel(c);
        }

        PendingIntent open = PendingIntent.getActivity(
                this, 0, new Intent(this, MainActivity.class),
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);

        Notification.Builder b = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL)
                : new Notification.Builder(this);

        return b.setContentTitle(getString(R.string.app_name))
                .setContentText(getString(R.string.live_title))
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentIntent(open)
                .setOngoing(true)
                .build();
    }

    /**
     * Takes the engine's running totals and works out bytes per second
     * since the previous report. A total that went backwards (the engine
     * restarted its counters) resets the rate instead of showing a
     * negative one.
     */
    private void countTraffic(long rx, long tx) {
        if (rx < 0 || tx < 0) return;
        final long now = android.os.SystemClock.elapsedRealtime();
        if (statAt > 0 && now > statAt && rx >= rxBytes && tx >= txBytes) {
            final double dt = (now - statAt) / 1000.0;
            rxRate = (rx - rxBytes) / dt;
            txRate = (tx - txBytes) / dt;
        } else {
            rxRate = txRate = 0;
        }
        statAt = now;
        rxBytes = rx;
        txBytes = tx;
    }

    /** One numeric field of a "stat" line; -1 if it is missing or unreadable. */
    static long statField(String line, String name) {
        final String key = name + "=";
        for (String field : line.split(" ")) {
            if (field.startsWith(key)) {
                try {
                    return Long.parseLong(field.substring(key.length()));
                } catch (NumberFormatException e) {
                    return -1;
                }
            }
        }
        return -1;
    }

    /** The host of a literal "host:port" or "[v6]:port". */
    static String hostOf(String endpoint) {
        if (endpoint == null) return null;
        if (endpoint.startsWith("[")) {
            final int close = endpoint.indexOf(']');
            return close > 1 ? endpoint.substring(1, close) : null;
        }
        final int colon = endpoint.lastIndexOf(':');
        return colon > 0 ? endpoint.substring(0, colon) : endpoint;
    }

    /** Reads "stat handshake=N rx=… tx=…"; zero if it cannot. */
    static long statHandshake(String line) {
        for (String field : line.split(" ")) {
            if (field.startsWith("handshake=")) {
                try {
                    return Long.parseLong(field.substring("handshake=".length()));
                } catch (NumberFormatException e) {
                    return 0;
                }
            }
        }
        return 0;
    }
}
