package com.termux.app.minemux;

import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.util.Log;

import com.termux.app.TermuxService;
import com.termux.shared.termux.TermuxConstants;
import com.termux.shared.termux.TermuxConstants.TERMUX_APP.TERMUX_SERVICE;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;

public final class MineMuxRuntime {

    public static final String DASHBOARD_URL = "http://127.0.0.1:8787";
    private static final String TAG = "MineMuxRuntime";

    private MineMuxRuntime() {}

    public static void ensureInstalled(Context context) {
        File daemonDir = new File(TermuxConstants.TERMUX_HOME_DIR, "minemux/daemon");
        File webuiDir = new File(TermuxConstants.TERMUX_HOME_DIR, "minemux/webui");
        File bootDir = new File(TermuxConstants.TERMUX_BOOT_SCRIPTS_DIR_PATH);
        File bin = new File(daemonDir, "minemux-daemon");
        File webui = new File(webuiDir, "index.html");
        File startScript = new File(daemonDir, "start-minemux.sh");
        File bootScript = new File(bootDir, "00-minemux-daemon");

        //noinspection ResultOfMethodCallIgnored
        daemonDir.mkdirs();
        //noinspection ResultOfMethodCallIgnored
        webuiDir.mkdirs();
        //noinspection ResultOfMethodCallIgnored
        bootDir.mkdirs();

        copyAssetIfChanged(context, "minemux/minemux-daemon", bin, true);
        copyAssetIfChanged(context, "minemux/webui/index.html", webui, false);
        writeExecutable(startScript,
            "#!/data/data/" + TermuxConstants.TERMUX_PACKAGE_NAME + "/files/usr/bin/sh\n" +
            "set -u\n" +
            "export MINEMUX_HOME=\"$HOME/minemux\"\n" +
            "export MINEMUX_WEBUI_INDEX=\"$MINEMUX_HOME/webui/index.html\"\n" +
            "export CRAFTNODE_HOME=\"$MINEMUX_HOME\"\n" +
            "export CRAFTNODE_WEBUI_INDEX=\"$MINEMUX_WEBUI_INDEX\"\n" +
            "mkdir -p \"$MINEMUX_HOME/logs\"\n" +
            "cd \"$HOME/minemux/daemon\"\n" +
            "if ! command -v java >/dev/null 2>&1; then\n" +
            "  echo \"[$(date -Is)] Installing OpenJDK for MineMux...\"\n" +
            "  pkg install -y openjdk-25 || pkg install -y openjdk-21 || true\n" +
            "fi\n" +
            "if command -v update-alternatives >/dev/null 2>&1; then\n" +
            "  update-alternatives --set java /data/data/" + TermuxConstants.TERMUX_PACKAGE_NAME + "/files/usr/opt/openjdk-25/bin/java || true\n" +
            "fi\n" +
            "exec ./minemux-daemon >> \"$MINEMUX_HOME/logs/daemon.log\" 2>&1\n");
        writeExecutable(bootScript,
            "#!/data/data/" + TermuxConstants.TERMUX_PACKAGE_NAME + "/files/usr/bin/sh\n" +
            "exec \"$HOME/minemux/daemon/start-minemux.sh\"\n");
    }

    public static void startDaemon(Context context) {
        ensureInstalled(context);
        File script = new File(TermuxConstants.TERMUX_HOME_DIR, "minemux/daemon/start-minemux.sh");
        Intent executeIntent = new Intent(TERMUX_SERVICE.ACTION_SERVICE_EXECUTE, new Uri.Builder()
            .scheme(TERMUX_SERVICE.URI_SCHEME_SERVICE_EXECUTE)
            .path(script.getAbsolutePath())
            .build());
        executeIntent.setClass(context, TermuxService.class);
        executeIntent.putExtra(TERMUX_SERVICE.EXTRA_BACKGROUND, true);
        executeIntent.putExtra(TERMUX_SERVICE.EXTRA_COMMAND_LABEL, "MineMux daemon");
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            context.startForegroundService(executeIntent);
        } else {
            context.startService(executeIntent);
        }
    }

    static void runBootScript(Context context, String scriptPath) {
        Intent executeIntent = new Intent(TERMUX_SERVICE.ACTION_SERVICE_EXECUTE, new Uri.Builder()
            .scheme(TERMUX_SERVICE.URI_SCHEME_SERVICE_EXECUTE)
            .path(scriptPath)
            .build());
        executeIntent.setClass(context, TermuxService.class);
        executeIntent.putExtra(TERMUX_SERVICE.EXTRA_BACKGROUND, true);
        executeIntent.putExtra(TERMUX_SERVICE.EXTRA_COMMAND_LABEL, "MineMux boot");
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            context.startForegroundService(executeIntent);
        } else {
            context.startService(executeIntent);
        }
    }

    private static void copyAssetIfChanged(Context context, String asset, File target, boolean executable) {
        try (InputStream in = context.getAssets().open(asset)) {
            byte[] data = readAll(in);
            if (target.exists() && target.length() == data.length) {
                if (executable) //noinspection ResultOfMethodCallIgnored
                    target.setExecutable(true);
                return;
            }
            try (FileOutputStream out = new FileOutputStream(target)) {
                out.write(data);
            }
            if (executable) //noinspection ResultOfMethodCallIgnored
                target.setExecutable(true);
        } catch (IOException e) {
            Log.e(TAG, "Failed to copy asset " + asset + " to " + target, e);
        }
    }

    private static byte[] readAll(InputStream in) throws IOException {
        byte[] buffer = new byte[8192];
        int read;
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        while ((read = in.read(buffer)) >= 0) {
            out.write(buffer, 0, read);
        }
        return out.toByteArray();
    }

    private static void writeExecutable(File file, String content) {
        try (FileOutputStream out = new FileOutputStream(file)) {
            out.write(content.getBytes(StandardCharsets.UTF_8));
            //noinspection ResultOfMethodCallIgnored
            file.setReadable(true);
            //noinspection ResultOfMethodCallIgnored
            file.setExecutable(true);
        } catch (IOException e) {
            Log.e(TAG, "Failed to write " + file, e);
        }
    }
}
