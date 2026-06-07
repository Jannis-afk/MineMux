package com.termux.app.minemux;

import android.app.Activity;
import android.content.Intent;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.ArrayAdapter;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.Nullable;

import com.termux.app.TermuxActivity;
import com.termux.app.TermuxInstaller;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.net.URLEncoder;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.text.DecimalFormat;
import java.util.ArrayList;
import java.util.Enumeration;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

public class MineMuxActivity extends Activity {

    private static final int BG = 0xff0d1210;
    private static final int PANEL = 0xff171f1b;
    private static final int PANEL_ALT = 0xff202923;
    private static final int TEXT = 0xfff3f7f4;
    private static final int MUTED = 0xff98a69e;
    private static final int ACCENT = 0xff9df2b3;
    private static final int ACCENT_TEXT = 0xff102014;
    private static final int WARNING = 0xffffd38f;
    private static final int ERROR = 0xffffaaa0;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Map<Button, Long> buttonCooldowns = new HashMap<>();
    private final Runnable pollStatus = new Runnable() {
        @Override
        public void run() {
            refreshStatus();
            mainHandler.postDelayed(this, 3000);
        }
    };

    private LinearLayout content;
    private TextView pageTitle;
    private TextView notice;
    private TextView daemonState;
    private TextView serverState;
    private TextView joinAddress;
    private TextView version;
    private TextView memory;
    private TextView logs;
    private LinearLayout operationPanel;
    private TextView operationTitle;
    private TextView operationDetail;
    private ProgressBar globalProgress;
    private Button dashboardTab;
    private Button setupTab;
    private Button modsTab;
    private Button backupsTab;
    private Button webButton;
    private String currentPage = "dashboard";
    private boolean controllerOnline;
    private boolean operationRunning;
    private String operationMessage = "";
    private int setupStep;
    private int failedPolls;
    private JSONObject latestStatus;
    private String wizardServerId = "main";
    private String wizardServerName = "Main Server";
    private String wizardVersion = "latest-compatible";
    private int wizardMemory = 2048;
    private int wizardPlayers = 8;
    private boolean wizardEula;

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
        scroll.setBackgroundColor(BG);

        LinearLayout root = column();
        root.setPadding(dp(18), dp(16), dp(18), dp(26));
        scroll.addView(root, new ScrollView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        root.addView(header, matchWrap());

        LinearLayout titleBlock = column();
        header.addView(titleBlock, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        titleBlock.addView(text("MineMux", 30, TEXT, true));
        titleBlock.addView(text("Local Minecraft hosting", 13, MUTED, false));

        Button terminal = secondaryButton("Terminal");
        terminal.setOnClickListener(v -> startActivity(new Intent(this, TermuxActivity.class)));
        header.addView(terminal, fixedButtonParams(112));

        notice = text("Starting local controller...", 14, WARNING, false);
        notice.setPadding(0, dp(14), 0, dp(10));
        root.addView(notice);

        globalProgress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        globalProgress.setIndeterminate(true);
        globalProgress.setVisibility(View.GONE);
        root.addView(globalProgress, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(4)));

        operationPanel = card();
        operationPanel.setPadding(dp(14), dp(12), dp(14), dp(12));
        operationPanel.setVisibility(View.GONE);
        root.addView(operationPanel, matchWrapMargin(0, dp(10), 0, dp(8)));
        operationTitle = text("Working", 15, TEXT, true);
        operationDetail = text("", 12, MUTED, false);
        operationPanel.addView(operationTitle);
        operationPanel.addView(operationDetail);

        LinearLayout hero = card();
        hero.setPadding(dp(14), dp(14), dp(14), dp(14));
        root.addView(hero, matchWrapMargin(0, dp(14), 0, dp(10)));
        daemonState = addMetric(hero, "Controller", "Starting");
        serverState = addMetric(hero, "Server", "Unknown");
        joinAddress = addMetric(hero, "Join address", defaultJoinAddress());
        version = addMetric(hero, "Minecraft", "-");
        memory = addMetric(hero, "Memory", "-");

        LinearLayout tabs = row();
        root.addView(tabs, matchWrapMargin(0, dp(4), 0, dp(8)));
        dashboardTab = tabButton("Home", "dashboard");
        setupTab = tabButton("Setup", "setup");
        modsTab = tabButton("Mods", "mods");
        backupsTab = tabButton("Backups", "backups");
        tabs.addView(dashboardTab, weightedTabParams());
        tabs.addView(setupTab, weightedTabParams());
        tabs.addView(modsTab, weightedTabParams());
        tabs.addView(backupsTab, weightedTabParams());

        pageTitle = text("", 20, TEXT, true);
        root.addView(pageTitle);

        content = column();
        root.addView(content, matchWrap());

        webButton = secondaryButton("Open Power UI");
        webButton.setOnClickListener(v -> startActivity(new Intent(this, MineMuxWebActivity.class)));
        root.addView(webButton, fullWidthButtonParams());

        setPage("dashboard");
        return scroll;
    }

    private void setPage(String page) {
        currentPage = page;
        if (content == null) return;
        content.removeAllViews();
        updateTabs();
        if ("setup".equals(page)) buildSetupPage();
        else if ("mods".equals(page)) buildModsPage();
        else if ("backups".equals(page)) buildBackupsPage();
        else buildDashboardPage();
        setControllerActionsEnabled(controllerOnline);
    }

    private void buildDashboardPage() {
        pageTitle.setText("Server");
        if (latestStatus == null || !activeServerInstalled()) {
            LinearLayout empty = card();
            empty.setPadding(dp(16), dp(18), dp(16), dp(18));
            content.addView(empty, matchWrapMargin(0, dp(8), 0, dp(10)));
            empty.addView(text("No server is set up yet", 19, TEXT, true));
            empty.addView(text("Create a phone-hosted Paper server with a guided setup. You can choose the Minecraft version, memory, and player limit before MineMux downloads the server jar.", 13, MUTED, false));
            Button setup = primaryButton("Set Up Server");
            setup.setOnClickListener(v -> {
                setupStep = 0;
                setPage("setup");
            });
            empty.addView(setup, fullWidthButtonParams());
        } else {
            LinearLayout live = card();
            live.setPadding(dp(16), dp(14), dp(16), dp(14));
            content.addView(live, matchWrapMargin(0, dp(8), 0, dp(10)));
            live.addView(text(activeServerRunning() ? "Live server running" : "Last server", 18, TEXT, true));
            live.addView(text(joinAddress.getText().toString(), 22, ACCENT, true));
            live.addView(text("Minecraft " + version.getText() + "  " + memory.getText(), 13, MUTED, false));

            LinearLayout controls = row();
            content.addView(controls, matchWrapMargin(0, 0, 0, dp(8)));
            Button start = primaryButton("Start");
            start.setOnClickListener(v -> actionButton(start, "/api/server/start", "{}", "Starting server..."));
            controls.addView(start, weightedButtonParams());
            Button stop = secondaryButton("Stop");
            stop.setOnClickListener(v -> actionButton(stop, "/api/server/stop", "{}", "Stopping server..."));
            controls.addView(stop, weightedButtonParams());
            Button restart = secondaryButton("Restart");
            restart.setOnClickListener(v -> actionButton(restart, "/api/server/restart", "{}", "Restarting server..."));
            controls.addView(restart, weightedButtonParams());
        }

        TextView serversTitle = text("Servers", 15, TEXT, true);
        serversTitle.setPadding(0, dp(2), 0, dp(6));
        content.addView(serversTitle);
        LinearLayout serverList = column();
        content.addView(serverList, matchWrapMargin(0, 0, 0, dp(10)));
        loadServers(serverList);

        LinearLayout quick = card();
        quick.setPadding(dp(14), dp(12), dp(14), dp(12));
        content.addView(quick, matchWrapMargin(0, 0, 0, dp(10)));
        quick.addView(text("Quick actions", 15, TEXT, true));
        Button backup = secondaryButton("Create Backup");
        backup.setOnClickListener(v -> actionButton(backup, "/api/backups/create", "{}", "Creating backup..."));
        quick.addView(backup, fullWidthButtonParams());

        TextView logsTitle = text("Recent logs", 15, TEXT, true);
        logsTitle.setPadding(0, dp(8), 0, dp(6));
        content.addView(logsTitle);
        logs = text("No logs yet.", 12, 0xffd9f0dd, false);
        logs.setTypeface(Typeface.MONOSPACE);
        logs.setBackground(makeBg(0xff070a08, 0xff253026, 8));
        logs.setPadding(dp(10), dp(10), dp(10), dp(10));
        content.addView(logs, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(220)));
        refreshLogs();
    }

    private void buildSetupPage() {
        pageTitle.setText("Setup server");
        LinearLayout panel = card();
        panel.setPadding(dp(14), dp(14), dp(14), dp(14));
        content.addView(panel, matchWrapMargin(0, dp(6), 0, dp(10)));

        panel.addView(text("Step " + (setupStep + 1) + " of 4", 12, ACCENT, true));
        if (setupStep == 0) buildSetupNameStep(panel);
        else if (setupStep == 1) buildSetupVersionStep(panel);
        else if (setupStep == 2) buildSetupResourcesStep(panel);
        else buildSetupConfirmStep(panel);
    }

    private void buildSetupNameStep(LinearLayout panel) {
        panel.addView(text("Name the server", 19, TEXT, true));
        panel.addView(text("This is how the server appears in the Android dashboard. The ID is used for the folder under MineMux servers.", 13, MUTED, false));
        EditText name = input(wizardServerName);
        addField(panel, "Display name", name);
        EditText id = input(wizardServerId);
        addField(panel, "Server ID", id);
        setupNav(panel, null, () -> {
            wizardServerName = valueOr(name, "Main Server");
            wizardServerId = valueOr(id, "main");
            setupStep = 1;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupVersionStep(LinearLayout panel) {
        panel.addView(text("Choose Minecraft", 19, TEXT, true));
        panel.addView(text("Paper is selected because it is the working phone-friendly runtime in this MVP. Fabric and NeoForge are planned later.", 13, MUTED, false));
        Spinner versionSpinner = spinner(new String[]{wizardVersion, "latest-compatible", "1.21.8", "1.21.7", "1.21.6", "1.21.5", "1.21.4", "1.20.6", "1.20.4"});
        addField(panel, "Minecraft version", versionSpinner);
        Spinner loaderSpinner = spinner(new String[]{"paper - Paper Plugins", "fabric - coming soon", "neoforge - coming soon"});
        addField(panel, "Runtime", loaderSpinner);
        loadSetupOptions(versionSpinner, loaderSpinner);
        setupNav(panel, () -> {
            setupStep = 0;
            setPage("setup");
        }, () -> {
            String loaderChoice = loaderSpinner.getSelectedItem().toString();
            if (!loaderChoice.startsWith("paper")) {
                setNotice("Only Paper Plugins are supported in this MVP build.", true);
                return;
            }
            wizardVersion = versionSpinner.getSelectedItem().toString();
            setupStep = 2;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupResourcesStep(LinearLayout panel) {
        panel.addView(text("Tune phone resources", 19, TEXT, true));
        panel.addView(text("These defaults are conservative for a phone. Increase memory only if the phone has enough RAM free.", 13, MUTED, false));
        EditText memoryInput = input(String.valueOf(wizardMemory));
        addField(panel, "Memory MB", memoryInput);
        EditText playersInput = input(String.valueOf(wizardPlayers));
        addField(panel, "Max players", playersInput);
        setupNav(panel, () -> {
            setupStep = 1;
            setPage("setup");
        }, () -> {
            wizardMemory = clamp(numberOr(memoryInput, 2048), 512, 8192);
            wizardPlayers = clamp(numberOr(playersInput, 8), 1, 50);
            setupStep = 3;
            setPage("setup");
        }, "Review");
    }

    private void buildSetupConfirmStep(LinearLayout panel) {
        panel.addView(text("Review and create", 19, TEXT, true));
        panel.addView(summaryLine("Server", wizardServerName + " (" + wizardServerId + ")"));
        panel.addView(summaryLine("Runtime", "Paper Plugins"));
        panel.addView(summaryLine("Minecraft", wizardVersion));
        panel.addView(summaryLine("Resources", wizardMemory + " MB, " + wizardPlayers + " players"));
        CheckBox eula = new CheckBox(this);
        eula.setText("I accept the Minecraft EULA");
        eula.setTextColor(TEXT);
        eula.setTextSize(15);
        eula.setChecked(wizardEula);
        panel.addView(eula, matchWrapMargin(0, dp(8), 0, dp(6)));
        setupNav(panel, () -> {
            wizardEula = eula.isChecked();
            setupStep = 2;
            setPage("setup");
        }, () -> {
            wizardEula = eula.isChecked();
            if (!wizardEula) {
                setNotice("Accept the Minecraft EULA first.", true);
                return;
            }
            String body = "{"
                + "\"serverId\":\"" + escapeJson(wizardServerId) + "\","
                + "\"name\":\"" + escapeJson(wizardServerName) + "\","
                + "\"loader\":\"paper\","
                + "\"minecraftVersion\":\"" + escapeJson(wizardVersion) + "\","
                + "\"memoryMb\":" + wizardMemory + ","
                + "\"maxPlayers\":" + wizardPlayers + ","
                + "\"viewDistance\":6,"
                + "\"simulationDistance\":4,"
                + "\"acceptEula\":true"
                + "}";
            beginOperation("Setting up server", "Preparing DNS and Java...");
            postJson("/api/server/setup", body, "Setting up server...", () -> {
                endOperation();
                setPage("dashboard");
            });
        }, "Create Server");
    }

    private void loadSetupOptions(Spinner versionSpinner, Spinner loaderSpinner) {
        getJson("/api/server/options", body -> {
            try {
                JSONObject root = new JSONObject(body);
                JSONArray versions = root.optJSONArray("minecraftVersions");
                if (versions != null && versions.length() > 0) {
                    List<String> items = new ArrayList<>();
                    items.add("latest-compatible");
                    for (int i = 0; i < Math.min(12, versions.length()); i++) {
                        String id = versions.getJSONObject(i).optString("id");
                        if (!id.isEmpty() && !items.contains(id)) items.add(id);
                    }
                    setSpinnerItems(versionSpinner, items);
                }
                JSONArray loaders = root.optJSONArray("loaders");
                if (loaders != null && loaders.length() > 0) {
                    List<String> items = new ArrayList<>();
                    for (int i = 0; i < loaders.length(); i++) {
                        JSONObject loader = loaders.getJSONObject(i);
                        String id = loader.optString("id");
                        String name = loader.optString("name", id);
                        boolean supported = loader.optBoolean("supported");
                        items.add(id + " - " + name + (supported ? "" : " - coming soon"));
                    }
                    setSpinnerItems(loaderSpinner, items);
                }
            } catch (Exception ignored) {}
        }, error -> {});
    }

    private void setupNav(LinearLayout panel, @Nullable Runnable back, Runnable next, String nextLabel) {
        LinearLayout nav = row();
        panel.addView(nav, matchWrapMargin(0, dp(12), 0, 0));
        if (back != null) {
            Button backButton = secondaryButton("Back");
            backButton.setOnClickListener(v -> back.run());
            nav.addView(backButton, weightedButtonParams());
        }
        Button nextButton = primaryButton(nextLabel);
        nextButton.setOnClickListener(v -> next.run());
        nav.addView(nextButton, weightedButtonParams());
    }

    private TextView summaryLine(String label, String value) {
        TextView line = text(label + ": " + value, 14, TEXT, false);
        line.setPadding(0, dp(6), 0, 0);
        return line;
    }

    private void loadServers(LinearLayout list) {
        list.removeAllViews();
        getJson("/api/servers", body -> {
            try {
                JSONArray servers = new JSONObject(body).optJSONArray("servers");
                if (servers == null || servers.length() == 0) {
                    list.addView(emptyState("No server profiles yet. Create one in Setup."));
                    return;
                }
                for (int i = 0; i < servers.length(); i++) {
                    list.addView(serverRow(servers.getJSONObject(i)));
                }
            } catch (Exception e) {
                list.addView(emptyState(e.getMessage()));
            }
        }, error -> list.addView(emptyState(error)));
    }

    private View serverRow(JSONObject server) {
        LinearLayout row = card();
        row.setPadding(dp(12), dp(12), dp(12), dp(12));
        row.setOrientation(LinearLayout.VERTICAL);
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(8)));
        boolean active = server.optBoolean("active");
        String id = server.optString("id", "main");
        String name = server.optString("name", id);
        row.addView(text(name + (active ? "  Active" : ""), 14, TEXT, true));
        row.addView(text(server.optBoolean("installed") ? server.optString("joinAddress", "") : "Needs setup", 12, MUTED, false));
        if (!active) {
            Button use = secondaryButton("Use This Server");
            use.setOnClickListener(v -> actionButton(use, "/api/servers/switch", "{\"serverId\":\"" + escapeJson(id) + "\"}", "Switching server..."));
            row.addView(use, fullWidthButtonParams());
        }
        return row;
    }

    private void buildModsPage() {
        pageTitle.setText("Mods & Plugins");
        LinearLayout panel = card();
        panel.setPadding(dp(14), dp(14), dp(14), dp(14));
        content.addView(panel, matchWrapMargin(0, dp(6), 0, dp(10)));
        panel.addView(text("Search Modrinth", 15, TEXT, true));
        panel.addView(text("MineMux filters results to the active server version and Paper loader.", 12, MUTED, false));

        EditText query = input("Chunky, Geyser, ViaVersion");
        addField(panel, "Plugin name", query);
        Button search = primaryButton("Search");
        panel.addView(search, fullWidthButtonParams());

        LinearLayout results = column();
        content.addView(results, matchWrap());
        search.setOnClickListener(v -> {
            String q = query.getText().toString().trim();
            if (q.isEmpty()) {
                setNotice("Type a plugin name first.", true);
                return;
            }
            String original = beginButton(search, "Searching...");
            getJson("/api/mods/search?query=" + urlEncode(q), body -> {
                results.removeAllViews();
                try {
                    JSONArray hits = new JSONObject(body).optJSONArray("hits");
                    if (hits == null || hits.length() == 0) {
                        results.addView(emptyState("No compatible plugins found."));
                        return;
                    }
                    for (int i = 0; i < Math.min(8, hits.length()); i++) {
                        JSONObject hit = hits.getJSONObject(i);
                        results.addView(modRow(hit));
                    }
                } catch (Exception e) {
                    setNotice(e.getMessage(), true);
                } finally {
                    finishButton(search, original);
                    endOperation();
                }
            }, error -> {
                setNotice(error, true);
                finishButton(search, original);
                endOperation();
            });
        });

        refreshModsList(results);
    }

    private View modRow(JSONObject hit) {
        LinearLayout row = card();
        row.setPadding(dp(12), dp(12), dp(12), dp(12));
        row.setOrientation(LinearLayout.VERTICAL);
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(8)));
        String title = hit.optString("title", "Plugin");
        String downloads = compactNumber(hit.optInt("downloads", 0));
        row.addView(text(title + "  " + downloads + " downloads", 15, TEXT, true));
        row.addView(text(hit.optString("description", ""), 12, MUTED, false));
        Button install = secondaryButton("Install");
        install.setOnClickListener(v -> {
            String projectId = hit.optString("project_id");
            if (projectId.isEmpty()) projectId = hit.optString("projectId");
            String body = "{\"projectId\":\"" + escapeJson(projectId) + "\"}";
            actionButton(install, "/api/mods/install", body, "Installing " + title + "...");
        });
        row.addView(install, fullWidthButtonParams());
        return row;
    }

    private void refreshModsList(LinearLayout parent) {
        getJson("/api/mods", body -> {
            try {
                JSONArray installed = new JSONObject(body).optJSONArray("installed");
                if (installed == null || installed.length() == 0) return;
                parent.addView(text("Installed", 15, TEXT, true));
                for (int i = 0; i < installed.length(); i++) {
                    JSONObject mod = installed.getJSONObject(i);
                    TextView item = text(mod.optString("name", mod.optString("fileName", "Installed jar")), 13, TEXT, false);
                    item.setPadding(dp(12), dp(8), dp(12), dp(8));
                    item.setBackground(makeBg(PANEL, 0xff26342b, 8));
                    parent.addView(item, matchWrapMargin(0, 0, 0, dp(6)));
                }
            } catch (Exception ignored) {}
        }, error -> {});
    }

    private void buildBackupsPage() {
        pageTitle.setText("Backups");
        LinearLayout actions = row();
        content.addView(actions, matchWrapMargin(0, dp(6), 0, dp(8)));
        Button create = primaryButton("Create");
        create.setOnClickListener(v -> actionButton(create, "/api/backups/create", "{}", "Creating backup..."));
        actions.addView(create, weightedButtonParams());
        Button rollback = secondaryButton("Rollback");
        rollback.setOnClickListener(v -> actionButton(rollback, "/api/mods/rollback", "{}", "Restoring latest backup..."));
        actions.addView(rollback, weightedButtonParams());

        LinearLayout list = column();
        content.addView(list, matchWrap());
        loadBackups(list);
    }

    private void loadBackups(LinearLayout list) {
        list.removeAllViews();
        getJson("/api/backups", body -> {
            try {
                JSONArray backups = new JSONObject(body).optJSONArray("backups");
                if (backups == null || backups.length() == 0) {
                    list.addView(emptyState("No backups yet."));
                    return;
                }
                for (int i = 0; i < backups.length(); i++) {
                    JSONObject backup = backups.getJSONObject(i);
                    list.addView(backupRow(backup));
                }
            } catch (Exception e) {
                list.addView(emptyState(e.getMessage()));
            }
        }, error -> list.addView(emptyState(error)));
    }

    private View backupRow(JSONObject backup) {
        LinearLayout row = card();
        row.setPadding(dp(12), dp(12), dp(12), dp(12));
        row.setOrientation(LinearLayout.VERTICAL);
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(8)));
        String id = backup.optString("id", "backup");
        row.addView(text(id, 14, TEXT, true));
        row.addView(text(bytes(backup.optLong("sizeBytes", 0)), 12, MUTED, false));
        Button restore = secondaryButton("Restore");
        restore.setOnClickListener(v -> actionButton(restore, "/api/backups/restore?id=" + urlEncode(id), "{}", "Restoring backup..."));
        row.addView(restore, fullWidthButtonParams());
        return row;
    }

    private TextView emptyState(String message) {
        TextView view = text(message, 13, MUTED, false);
        view.setGravity(Gravity.CENTER);
        view.setPadding(dp(14), dp(22), dp(14), dp(22));
        view.setBackground(makeBg(PANEL, 0xff253026, 8));
        return view;
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
                latestStatus = new JSONObject(body);
                JSONObject server = latestStatus.getJSONObject("server");
                JSONObject profile = latestStatus.optJSONObject("profile");
                failedPolls = 0;
                controllerOnline = true;
                daemonState.setText("Online");
                serverState.setText(server.optBoolean("running") ? "Running" : server.optBoolean("installed") ? "Ready" : "Needs Setup");
                joinAddress.setText(server.optString("joinAddress", defaultJoinAddress()));
                version.setText(profile != null ? profile.optString("minecraftVersion", "-") : "-");
                memory.setText(profile != null ? profile.optInt("memoryMb", 0) + " MB" : "-");
                setControllerActionsEnabled(true);
                setNotice(server.optString("lastError", ""), server.has("lastError") && !server.optString("lastError").isEmpty());
                if ("dashboard".equals(currentPage) || operationRunning) refreshLogs();
            } catch (Exception e) {
                setNotice(e.getMessage(), true);
            }
        }, error -> {
            failedPolls++;
            controllerOnline = false;
            setControllerActionsEnabled(false);
            daemonState.setText("Starting");
            if (failedPolls < 3) setNotice("Starting local controller...", false);
            else setNotice("Controller not reachable: " + error, true);
        });
    }

    private void refreshLogs() {
        getJson("/api/server/logs", body -> {
            try {
                JSONArray lines = new JSONObject(body).optJSONArray("lines");
                if (lines == null || lines.length() == 0) {
                    if (logs != null) logs.setText("No logs yet.");
                    return;
                }
                StringBuilder out = new StringBuilder();
                int start = Math.max(0, lines.length() - 12);
                for (int i = start; i < lines.length(); i++) out.append(lines.getString(i)).append('\n');
                if (logs != null) logs.setText(out.toString());
                updateOperationFromLog(lines);
            } catch (Exception e) {
                if (logs != null) logs.setText(e.getMessage());
            }
        }, error -> {});
    }

    private void updateOperationFromLog(JSONArray lines) {
        if (!operationRunning || lines == null || lines.length() == 0 || operationDetail == null) return;
        try {
            String latest = lines.getString(lines.length() - 1);
            if (latest != null && !latest.trim().isEmpty()) {
                operationMessage = latest.trim();
                operationDetail.setText(operationMessage);
            }
        } catch (Exception ignored) {}
    }

    private void actionButton(Button button, String path, String json, String progress) {
        if (!controllerOnline) {
            setNotice("Controller is still starting. Try again in a moment.", true);
            return;
        }
        if (isCoolingDown(button)) return;
        String original = beginButton(button, progress);
        beginOperation(progress.replace("...", ""), progress);
        postJson(path, json, progress, () -> {
            finishButton(button, original);
            endOperation();
        });
    }

    private String beginButton(Button button, String busyText) {
        String original = button.getText().toString();
        buttonCooldowns.put(button, System.currentTimeMillis() + 1200);
        button.setEnabled(false);
        button.setText(busyText);
        beginOperation("Working", busyText);
        return original;
    }

    private void finishButton(Button button, String original) {
        long remaining = Math.max(0, buttonCooldowns.getOrDefault(button, 0L) - System.currentTimeMillis());
        mainHandler.postDelayed(() -> {
            button.setText(original);
            button.setEnabled(controllerOnline);
            if (!operationRunning && globalProgress != null) globalProgress.setVisibility(View.GONE);
        }, remaining);
    }

    private void beginOperation(String title, String detail) {
        operationRunning = true;
        operationMessage = detail;
        if (globalProgress != null) globalProgress.setVisibility(View.VISIBLE);
        if (operationPanel != null) operationPanel.setVisibility(View.VISIBLE);
        if (operationTitle != null) operationTitle.setText(title == null || title.isEmpty() ? "Working" : title);
        if (operationDetail != null) operationDetail.setText(detail == null || detail.isEmpty() ? "Working..." : detail);
        setControllerActionsEnabled(false);
    }

    private void endOperation() {
        operationRunning = false;
        operationMessage = "";
        if (globalProgress != null) globalProgress.setVisibility(View.GONE);
        if (operationPanel != null) operationPanel.setVisibility(View.GONE);
        setControllerActionsEnabled(controllerOnline);
    }

    private boolean isCoolingDown(Button button) {
        Long until = buttonCooldowns.get(button);
        if (until == null || System.currentTimeMillis() >= until) return false;
        setNotice("Still working. Wait a moment.", false);
        return true;
    }

    private void postJson(String path, String json, String progress, Runnable done) {
        setNotice(progress, false);
        new Thread(() -> {
            try {
                request(path, "POST", json);
                mainHandler.post(() -> {
                    setNotice("Done", false);
                    refreshStatus();
                    if ("backups".equals(currentPage) || "mods".equals(currentPage)) setPage(currentPage);
                    done.run();
                });
            } catch (Exception e) {
                mainHandler.post(() -> {
                    setNotice(e.getMessage(), true);
                    done.run();
                });
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
        notice.setTextColor(error ? ERROR : WARNING);
        if (error && value != null && !value.isEmpty()) Toast.makeText(this, value, Toast.LENGTH_SHORT).show();
    }

    private void setControllerActionsEnabled(boolean enabled) {
        if (operationRunning) enabled = false;
        enableTree(content, enabled);
        if (webButton != null) webButton.setEnabled(enabled);
    }

    private void enableTree(View view, boolean enabled) {
        if (view == null) return;
        if (view instanceof Button && !isTabButton((Button) view)) {
            view.setEnabled(enabled);
            view.setAlpha(enabled ? 1f : 0.45f);
        }
        if (view instanceof ViewGroup) {
            ViewGroup group = (ViewGroup) view;
            for (int i = 0; i < group.getChildCount(); i++) enableTree(group.getChildAt(i), enabled);
        }
    }

    private boolean isTabButton(Button button) {
        return button == dashboardTab || button == setupTab || button == modsTab || button == backupsTab;
    }

    private boolean activeServerInstalled() {
        try {
            if (latestStatus == null) return false;
            return latestStatus.getJSONObject("server").optBoolean("installed");
        } catch (Exception e) {
            return false;
        }
    }

    private boolean activeServerRunning() {
        try {
            if (latestStatus == null) return false;
            return latestStatus.getJSONObject("server").optBoolean("running");
        } catch (Exception e) {
            return false;
        }
    }

    private void updateTabs() {
        styleTab(dashboardTab, "dashboard".equals(currentPage));
        styleTab(setupTab, "setup".equals(currentPage));
        styleTab(modsTab, "mods".equals(currentPage));
        styleTab(backupsTab, "backups".equals(currentPage));
    }

    private TextView addMetric(LinearLayout parent, String label, String value) {
        LinearLayout row = column();
        row.setPadding(0, dp(4), 0, dp(8));
        parent.addView(row, matchWrap());
        row.addView(text(label, 11, MUTED, false));
        TextView valueView = text(value, 19, TEXT, true);
        row.addView(valueView);
        return valueView;
    }

    private void addField(LinearLayout parent, String label, View field) {
        TextView labelView = text(label, 12, MUTED, false);
        labelView.setPadding(0, dp(12), 0, dp(4));
        parent.addView(labelView);
        parent.addView(field, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(48)));
    }

    private Spinner spinner(String[] values) {
        Spinner spinner = new Spinner(this);
        ArrayAdapter<String> adapter = new ArrayAdapter<>(this, android.R.layout.simple_spinner_item, values);
        adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        spinner.setAdapter(adapter);
        spinner.setBackground(makeBg(PANEL_ALT, 0xff3a493f, 8));
        return spinner;
    }

    private void setSpinnerItems(Spinner spinner, List<String> values) {
        ArrayAdapter<String> adapter = new ArrayAdapter<>(this, android.R.layout.simple_spinner_item, values);
        adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        spinner.setAdapter(adapter);
    }

    private EditText input(String hint) {
        EditText edit = new EditText(this);
        edit.setText(hint);
        edit.setSingleLine(true);
        edit.setTextColor(TEXT);
        edit.setHintTextColor(MUTED);
        edit.setTextSize(15);
        edit.setPadding(dp(12), 0, dp(12), 0);
        edit.setBackground(makeBg(PANEL_ALT, 0xff3a493f, 8));
        return edit;
    }

    private Button tabButton(String label, String page) {
        Button button = new Button(this);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(13);
        button.setOnClickListener(v -> setPage(page));
        return button;
    }

    private Button primaryButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(ACCENT_TEXT);
        button.setBackground(makeBg(ACCENT, 0xffd7ffe0, 8));
        return button;
    }

    private Button secondaryButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(TEXT);
        button.setBackground(makeBg(PANEL_ALT, 0xff3a493f, 8));
        return button;
    }

    private Button baseButton(String label) {
        Button button = new Button(this);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(14);
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        button.setPadding(dp(10), 0, dp(10), 0);
        return button;
    }

    private void styleTab(Button button, boolean active) {
        if (button == null) return;
        button.setTextColor(active ? ACCENT_TEXT : TEXT);
        button.setBackground(makeBg(active ? ACCENT : PANEL_ALT, active ? 0xffd7ffe0 : 0xff39483f, 8));
    }

    private LinearLayout column() {
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        return layout;
    }

    private LinearLayout row() {
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.HORIZONTAL);
        return layout;
    }

    private LinearLayout card() {
        LinearLayout layout = column();
        layout.setBackground(makeBg(PANEL, 0xff28332c, 8));
        return layout;
    }

    private GradientDrawable makeBg(int color, int stroke, int radius) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(color);
        drawable.setStroke(dp(1), stroke);
        drawable.setCornerRadius(dp(radius));
        return drawable;
    }

    private TextView text(String value, int sp, int color, boolean bold) {
        TextView view = new TextView(this);
        view.setText(value);
        view.setTextSize(sp);
        view.setTextColor(color);
        view.setIncludeFontPadding(true);
        if (bold) view.setTypeface(Typeface.DEFAULT_BOLD);
        return view;
    }

    private LinearLayout.LayoutParams matchWrap() {
        return new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
    }

    private LinearLayout.LayoutParams matchWrapMargin(int l, int t, int r, int b) {
        LinearLayout.LayoutParams params = matchWrap();
        params.setMargins(l, t, r, b);
        return params;
    }

    private LinearLayout.LayoutParams weightedButtonParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(48), 1);
        params.setMargins(dp(3), 0, dp(3), dp(8));
        return params;
    }

    private LinearLayout.LayoutParams weightedTabParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(44), 1);
        params.setMargins(dp(2), 0, dp(2), 0);
        return params;
    }

    private LinearLayout.LayoutParams fullWidthButtonParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(48));
        params.setMargins(0, dp(8), 0, 0);
        return params;
    }

    private LinearLayout.LayoutParams fixedButtonParams(int widthDp) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(dp(widthDp), dp(44));
        params.setMargins(dp(8), 0, 0, 0);
        return params;
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
                    if (address instanceof Inet4Address && !address.isLoopbackAddress()) return address.getHostAddress();
                }
            }
        } catch (Exception ignored) {}
        return "127.0.0.1";
    }

    private int numberOr(EditText editText, int fallback) {
        try {
            return Integer.parseInt(editText.getText().toString().trim());
        } catch (Exception e) {
            return fallback;
        }
    }

    private int clamp(int value, int min, int max) {
        return Math.max(min, Math.min(max, value));
    }

    private String valueOr(EditText editText, String fallback) {
        String value = editText.getText().toString().trim();
        return value.isEmpty() ? fallback : value;
    }

    private String escapeJson(String value) {
        return value.replace("\\", "\\\\").replace("\"", "\\\"");
    }

    private String urlEncode(String value) {
        try {
            return URLEncoder.encode(value, "UTF-8");
        } catch (Exception e) {
            return value;
        }
    }

    private String compactNumber(int value) {
        if (value >= 1000000) return new DecimalFormat("#.#M").format(value / 1000000.0);
        if (value >= 1000) return new DecimalFormat("#.#k").format(value / 1000.0);
        return String.valueOf(value);
    }

    private String bytes(long value) {
        if (value <= 0) return "Unknown size";
        if (value > 1024L * 1024L * 1024L) return new DecimalFormat("#.# GB").format(value / 1024.0 / 1024.0 / 1024.0);
        if (value > 1024L * 1024L) return new DecimalFormat("#.# MB").format(value / 1024.0 / 1024.0);
        return new DecimalFormat("# KB").format(value / 1024.0);
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private interface ResultHandler {
        void handle(String value);
    }
}
