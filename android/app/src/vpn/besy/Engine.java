package vpn.besy;

import android.content.Context;

import java.io.File;
import java.io.IOException;

/**
 * Finds the tunnel engine inside the installed app.
 *
 * <p>It ships as {@code lib/<abi>/libawg.so} and Android unpacks it into
 * the app's native library directory. That directory is the one place an
 * app may still execute a file from: execution from the data directory
 * has been refused since API 29, which is why the engine travels as a
 * library rather than as an asset the app would have to copy out.
 *
 * <p>The manifest sets {@code extractNativeLibs="true"} for the same
 * reason. Left at its modern default the file stays compressed inside
 * the APK, where it can be loaded but never run.
 */
final class Engine {

    private Engine() {}

    static File binary(Context context) throws IOException {
        File f = new File(context.getApplicationInfo().nativeLibraryDir, "libawg.so");
        if (!f.exists()) {
            throw new IOException("the tunnel engine is missing from this build (" + f + ")");
        }
        if (!f.canExecute()) {
            throw new IOException("the tunnel engine is not executable (" + f + ")");
        }
        return f;
    }
}
