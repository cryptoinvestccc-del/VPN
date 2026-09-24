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
        startForeground(NOTIFICATION_ID, notification());

        worker = new Thread(new Runnable() {
            @Override public void run() {
                try {
                    connect();
                } catch (Throwable t) {
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
        stage = "ключ";
        Keys keys = Keys.load(this);
        stage = "конфиг";
        JSONObject issued = Provisioning.issue(Provisioning.endpoint(this), keys.publicKey());

        stage = "интерфейс";
        Builder builder = new Builder();
        builder.setSession("BESY");
        builder.setMtu(issued.optInt("mtu", 1280));

        String address = issued.getString("address");     // "10.8.1.42/32"
        String[] parts = address.split("/");
        builder.addAddress(parts[0], parts.length > 1 ? Integer.parseInt(parts[1]) : 32);

        String allowed = issued.optString("allowed_ips", "0.0.0.0/0, ::/0");
        for (String route : allowed.split(",")) {
            route = route.trim();
            if (route.isEmpty()) continue;
            String[] r = route.split("/");
            builder.addRoute(r[0], r.length > 1 ? Integer.parseInt(r[1]) : 0);
        }

        for (String server : dnsServers(issued)) {
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

        stage = "движок";
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

            LocalSocket peer = listener.accept();
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

            if (!up && "ready".equals(line)) {
                up = true;
                stage = "";
                lastError = null;
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
        if (!up) {
            StringBuilder why = new StringBuilder("движок: ");
            if (said.isEmpty()) {
                why.append("вышел молча, не подняв туннель");
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
    private static java.util.List<String> dnsServers(JSONObject issued) {
        java.util.List<String> out = new java.util.ArrayList<String>();

        org.json.JSONArray list = issued.optJSONArray("dns");
        if (list != null) {
            for (int i = 0; i < list.length(); i++) {
                String server = list.optString(i, "").trim();
                if (!server.isEmpty()) out.add(server);
            }
            return out;
        }

        for (String server : issued.optString("dns", "").split(",")) {
            server = server.trim();
            if (!server.isEmpty()) out.add(server);
        }
        return out;
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
        stopForeground(true);
    }

    @Override public void onRevoke() {
        // The system, or another VPN app, took the interface away.
        stopTunnel();
        stopSelf();
    }

    @Override public void onDestroy() {
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
}
