package com.termux.app.minemux;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.Nullable;

import com.termux.app.TermuxActivity;
import com.termux.app.TermuxInstaller;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.Enumeration;

public class MineMuxActivity extends Activity {

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Runnable pollStatus = new Runnable() {
        @Override
        public void run() {
            refreshStatus();
            mainHandler.postDelayed(this, 3000);
        }
    };

    private TextView daemonState;
    private TextView serverState;
    private TextView joinAddress;
    private TextView version;
    private TextView memory;
    private TextView notice;
    private TextView logs;
    private CheckBox eula;
    private Button setupButton;
    private Button startButton;
    private Button stopButton;
    private Button restartButton;
    private Button webButton;
    private int failedPolls;
    private boolean controllerOnline;

    @Override
    protected void onCreate(@Nullable Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(buildContent());
        startMineMuxDaemon();
    }

    @Override
    protected void onResume() {
        super.onResume();
        pollStatus.run();
    }

    @Override
    protected void onPause() {
        super.onPause();
        mainHandler.removeCallbacks(pollStatus);
    }

    private View buildContent() {
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(0xff0e1110);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(18), dp(16), dp(18), dp(26));
        scroll.addView(root, new ScrollView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        LinearLayout header = new LinearLayout(this);
        header.setGravity(Gravity.CENTER_VERTICAL);
        header.setOrientation(LinearLayout.HORIZONTAL);
        root.addView(header, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        LinearLayout titleBlock = new LinearLayout(this);
        titleBlock.setOrientation(LinearLayout.VERTICAL);
        header.addView(titleBlock, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

        TextView title = text("MineMux", 28, 0xfff4f7f4, true);
        titleBlock.addView(title);

        TextView subtitle = text("Phone Minecraft server", 13, 0xff9aa69d, false);
        titleBlock.addView(subtitle);

        Button terminal = button("Terminal");
        terminal.setOnClickListener(v -> startActivity(new Intent(this, TermuxActivity.class)));
        header.addView(terminal);

        notice = text("Starting local controller...", 14, 0xffffdca3, false);
        notice.setPadding(0, dp(14), 0, dp(12));
        root.addView(notice);

        LinearLayout statusGrid = new LinearLayout(this);
        statusGrid.setOrientation(LinearLayout.VERTICAL);
        root.addView(statusGrid);

        daemonState = addMetric(statusGrid, "Controller", "Starting");
        serverState = addMetric(statusGrid, "Server", "Unknown");
        joinAddress = addMetric(statusGrid, "Join From Minecraft", defaultJoinAddress());
        version = addMetric(statusGrid, "Minecraft", "-");
        memory = addMetric(statusGrid, "Memory", "-");

        TextView setupTitle = sectionTitle("Create Server");
        root.addView(setupTitle);

        eula = new CheckBox(this);
        eula.setText("I accept the Minecraft EULA");
        eula.setTextColor(0xffedf5ef);
        eula.setTextSize(15);
        root.addView(eula);

        setupButton = primaryButton("Setup Paper Server");
        setupButton.setOnClickListener(v -> {
            if (!eula.isChecked()) {
                setNotice("Accept the Minecraft EULA first.", true);
                return;
            }
            postJson("/api/server/setup", "{\"minecraftVersion\":\"latest-compatible\",\"memoryMb\":2048,\"maxPlayers\":8,\"viewDistance\":6,\"simulationDistance\":4,\"acceptEula\":true}", "Setting up Paper. This can take a while...");
        });
        root.addView(setupButton, fullWidthButtonParams());

        TextView controlsTitle = sectionTitle("Server Controls");
        root.addView(controlsTitle);

        LinearLayout controls = new LinearLayout(this);
        controls.setOrientation(LinearLayout.HORIZONTAL);
        controls.setGravity(Gravity.CENTER);
        root.addView(controls);

        startButton = primaryButton("Start");
        startButton.setOnClickListener(v -> postJson("/api/server/start", "{}", "Starting server..."));
        controls.addView(startButton, weightedButtonParams());

        stopButton = button("Stop");
        stopButton.setOnClickListener(v -> postJson("/api/server/stop", "{}", "Stopping server..."));
        controls.addView(stopButton, weightedButtonParams());

        restartButton = button("Restart");
        restartButton.setOnClickListener(v -> postJson("/api/server/restart", "{}", "Restarting server..."));
        controls.addView(restartButton, weightedButtonParams());

        webButton = button("Power UI");
        webButton.setOnClickListener(v -> startActivity(new Intent(this, MineMuxWebActivity.class)));
        root.addView(webButton, fullWidthButtonParams());

        TextView logsTitle = sectionTitle("Recent Logs");
        root.addView(logsTitle);

        logs = text("No logs yet.", 12, 0xffdbf1de, false);
        logs.setTypeface(android.graphics.Typeface.MONOSPACE);
        logs.setBackgroundColor(0xff070a08);
        logs.setPadding(dp(10), dp(10), dp(10), dp(10));
        root.addView(logs, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(210)));

        setControllerActionsEnabled(false);

        return scroll;
    }

    private TextView addMetric(LinearLayout parent, String label, String value) {
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.VERTICAL);
        row.setPadding(dp(14), dp(12), dp(14), dp(12));
        row.setBackgroundColor(0xff191f1b);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        params.setMargins(0, 0, 0, dp(8));
        parent.addView(row, params);

        TextView labelView = text(label, 12, 0xffa8b8ad, false);
        row.addView(labelView);

        TextView valueView = text(value, 20, 0xfff4f7f4, true);
        row.addView(valueView);
        return valueView;
    }

    private TextView sectionTitle(String title) {
        TextView view = text(title, 18, 0xffedf5ef, true);
        view.setPadding(0, dp(12), 0, dp(8));
        return view;
    }

    private TextView text(String value, int sp, int color, boolean bold) {
        TextView view = new TextView(this);
        view.setText(value);
        view.setTextSize(sp);
        view.setTextColor(color);
        view.setIncludeFontPadding(true);
        if (bold) view.setTypeface(android.graphics.Typeface.DEFAULT_BOLD);
        return view;
    }

    private Button primaryButton(String label) {
        Button button = button(label);
        button.setTextColor(0xff102014);
        button.setBackgroundColor(0xffb8f2c6);
        return button;
    }

    private Button button(String label) {
        Button button = new Button(this);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextColor(0xfff4f7f4);
        button.setBackgroundColor(0xff232a26);
        return button;
    }

    private LinearLayout.LayoutParams weightedButtonParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(48), 1);
        params.setMargins(dp(3), 0, dp(3), dp(8));
        return params;
    }

    private LinearLayout.LayoutParams fullWidthButtonParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(48));
        params.setMargins(0, dp(4), 0, dp(8));
        return params;
    }

    private void startMineMuxDaemon() {
        TermuxInstaller.setupBootstrapIfNeeded(this, () -> {
            MineMuxRuntime.ensureInstalled(this);
            MineMuxRuntime.startDaemon(this);
            setNotice("Starting local controller...", false);
            mainHandler.postDelayed(this::refreshStatus, 1200);
        });
    }

    private void refreshStatus() {
        getJson("/api/status", body -> {
            try {
                JSONObject root = new JSONObject(body);
                JSONObject server = root.getJSONObject("server");
                JSONObject profile = root.optJSONObject("profile");

                failedPolls = 0;
                controllerOnline = true;
                setControllerActionsEnabled(true);
                daemonState.setText("Online");
                serverState.setText(server.optBoolean("running") ? "Running" : server.optBoolean("installed") ? "Ready" : "Needs Setup");
                joinAddress.setText(server.optString("joinAddress", defaultJoinAddress()));
                version.setText(profile != null ? profile.optString("minecraftVersion", "-") : "-");
                memory.setText(profile != null ? profile.optInt("memoryMb", 0) + " MB" : "-");
                setNotice(server.optString("lastError", ""), server.has("lastError") && !server.optString("lastError").isEmpty());
                refreshLogs();
            } catch (Exception e) {
                setNotice(e.getMessage(), true);
            }
        }, error -> {
            failedPolls++;
            controllerOnline = false;
            setControllerActionsEnabled(false);
            daemonState.setText("Starting");
            if (failedPolls < 3) {
                setNotice("Starting local controller...", false);
            } else {
                setNotice("Controller not reachable: " + error, true);
            }
        });
    }

    private void refreshLogs() {
        getJson("/api/server/logs", body -> {
            try {
                org.json.JSONArray lines = new JSONObject(body).optJSONArray("lines");
                if (lines == null || lines.length() == 0) {
                    logs.setText("No logs yet.");
                    return;
                }
                StringBuilder out = new StringBuilder();
                int start = Math.max(0, lines.length() - 12);
                for (int i = start; i < lines.length(); i++) {
                    out.append(lines.getString(i)).append('\n');
                }
                logs.setText(out.toString());
            } catch (Exception e) {
                logs.setText(e.getMessage());
            }
        }, error -> {});
    }

    private void postJson(String path, String json, String progress) {
        if (!controllerOnline) {
            setNotice("Controller is still starting. Try again in a moment.", true);
            return;
        }
        setNotice(progress, false);
        new Thread(() -> {
            try {
                String response = request(path, "POST", json);
                mainHandler.post(() -> {
                    setNotice("Done", false);
                    refreshStatus();
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void getJson(String path, ResultHandler success, ResultHandler failure) {
        new Thread(() -> {
            try {
                String response = request(path, "GET", null);
                mainHandler.post(() -> success.handle(response));
            } catch (Exception e) {
                mainHandler.post(() -> failure.handle(e.getMessage()));
            }
        }).start();
    }

    private String request(String path, String method, @Nullable String body) throws Exception {
        HttpURLConnection connection = (HttpURLConnection) new URL(MineMuxRuntime.DASHBOARD_URL + path).openConnection();
        connection.setConnectTimeout(2500);
        connection.setReadTimeout(600000);
        connection.setRequestMethod(method);
        connection.setRequestProperty("Content-Type", "application/json");
        if (body != null) {
            connection.setDoOutput(true);
            try (OutputStream out = connection.getOutputStream()) {
                out.write(body.getBytes(StandardCharsets.UTF_8));
            }
        }

        int status = connection.getResponseCode();
        BufferedReader reader = new BufferedReader(new InputStreamReader(
            status >= 200 && status < 300 ? connection.getInputStream() : connection.getErrorStream(),
            StandardCharsets.UTF_8));
        StringBuilder response = new StringBuilder();
        String line;
        while ((line = reader.readLine()) != null) response.append(line);
        reader.close();
        connection.disconnect();

        if (status < 200 || status >= 300) {
            String message = response.toString();
            try {
                message = new JSONObject(message).optString("error", message);
            } catch (Exception ignored) {}
            throw new IllegalStateException(message.isEmpty() ? "Request failed: " + status : message);
        }
        return response.toString();
    }

    private void setNotice(String value, boolean error) {
        if (notice == null) return;
        notice.setText(value == null || value.isEmpty() ? "Ready" : value);
        notice.setTextColor(error ? 0xffffb4a8 : 0xffffdca3);
        if (error && value != null && !value.isEmpty()) Toast.makeText(this, value, Toast.LENGTH_SHORT).show();
    }

    private void setControllerActionsEnabled(boolean enabled) {
        if (setupButton == null) return;
        setupButton.setEnabled(enabled);
        startButton.setEnabled(enabled);
        stopButton.setEnabled(enabled);
        restartButton.setEnabled(enabled);
        webButton.setEnabled(enabled);
        float alpha = enabled ? 1.0f : 0.45f;
        setupButton.setAlpha(alpha);
        startButton.setAlpha(alpha);
        stopButton.setAlpha(alpha);
        restartButton.setAlpha(alpha);
        webButton.setAlpha(alpha);
    }

    private String defaultJoinAddress() {
        return localWifiIp() + ":25565";
    }

    private String localWifiIp() {
        try {
            Enumeration<NetworkInterface> interfaces = NetworkInterface.getNetworkInterfaces();
            while (interfaces.hasMoreElements()) {
                NetworkInterface networkInterface = interfaces.nextElement();
                if (!networkInterface.isUp() || networkInterface.isLoopback()) continue;
                Enumeration<InetAddress> addresses = networkInterface.getInetAddresses();
                while (addresses.hasMoreElements()) {
                    InetAddress address = addresses.nextElement();
                    if (address instanceof Inet4Address && !address.isLoopbackAddress()) {
                        return address.getHostAddress();
                    }
                }
            }
        } catch (Exception e) {
            // Fall through to localhost below.
        }
        return "127.0.0.1";
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private interface ResultHandler {
        void handle(String value);
    }
}
