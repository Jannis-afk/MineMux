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
import java.util.function.Consumer;

public class MineMuxActivity extends Activity {

    private static final int BG = 0xff07111f;
    private static final int PANEL = 0xff101a2a;
    private static final int PANEL_ALT = 0xff182437;
    private static final int PANEL_SOFT = 0xff0d1726;
    private static final int STROKE = 0xff26364f;
    private static final int TEXT = 0xfff5f7fb;
    private static final int MUTED = 0xff9aa8bc;
    private static final int TERTIARY = 0xff6f7b8f;
    private static final int ACCENT = 0xff2f86ff;
    private static final int GREEN = 0xff0db50d;
    private static final int ACCENT_TEXT = 0xffffffff;
    private static final int WARNING = 0xffffc857;
    private static final int ERROR = 0xffff4d5a;
    private static final int RED = 0xffef3f4d;
    private static final int PURPLE = 0xffa66bff;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Map<Button, Long> buttonCooldowns = new HashMap<>();
    private final ArrayList<String> pageHistory = new ArrayList<>();
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
    private Button serversTab;
    private Button settingsTab;
    private Button webButton;
    private String currentPage = "dashboard";
    private boolean navigatingBack;
    private long lastBackPressMs;
    private boolean controllerOnline;
    private boolean operationRunning;
    private Boolean lastDashboardInstalled;
    private Boolean lastDashboardRunning;
    private String operationMessage = "";
    private int setupStep;
    private int failedPolls;
    private JSONObject latestStatus;
    private String wizardServerId = "main";
    private String wizardServerName = "Main Server";
    private String wizardVersion = "latest-compatible";
    private String wizardLoader = "paper";
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

    @Override
    public void onBackPressed() {
        if ("setup".equals(currentPage) && setupStep > 0) {
            setupStep--;
            setPage("setup");
            return;
        }
        if (!"dashboard".equals(currentPage)) {
            String previous = pageHistory.isEmpty() ? "dashboard" : pageHistory.remove(pageHistory.size() - 1);
            navigatingBack = true;
            setPage(previous);
            navigatingBack = false;
            return;
        }
        long now = System.currentTimeMillis();
        if (now - lastBackPressMs < 2000) {
            super.onBackPressed();
            return;
        }
        lastBackPressMs = now;
        Toast.makeText(this, "Press back again to exit MineMux", Toast.LENGTH_SHORT).show();
    }

    private View buildContent() {
        LinearLayout screen = column();
        screen.setBackgroundColor(BG);

        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(BG);
        screen.addView(scroll, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));

        LinearLayout root = column();
        root.setPadding(dp(24), dp(22), dp(24), dp(22));
        scroll.addView(root, new ScrollView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        root.addView(header, matchWrap());

        LinearLayout titleBlock = column();
        header.addView(titleBlock, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        LinearLayout brand = row();
        brand.setGravity(Gravity.CENTER_VERTICAL);
        titleBlock.addView(brand);
        TextView cube = text("■", 26, GREEN, true);
        cube.setPadding(0, 0, dp(10), 0);
        brand.addView(cube);
        cube.setVisibility(View.GONE);
        TextView cubeBlock = new TextView(this);
        cubeBlock.setBackground(makeBg(GREEN, 0xff86ff95, 7));
        LinearLayout.LayoutParams cubeBlockParams = new LinearLayout.LayoutParams(dp(24), dp(24));
        cubeBlockParams.setMargins(0, 0, dp(10), 0);
        brand.addView(cubeBlock, cubeBlockParams);
        brand.addView(text("MineMux", 30, TEXT, true));
        titleBlock.addView(text("Phone Minecraft server", 13, MUTED, false));

        notice = text(controllerOnline ? "Ready" : "Starting local controller...", 14, WARNING, false);
        notice.setPadding(0, dp(14), 0, dp(10));
        root.addView(notice);

        globalProgress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        globalProgress.setIndeterminate(true);
        globalProgress.setVisibility(View.GONE);
        LinearLayout.LayoutParams progressParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(4));
        progressParams.setMargins(0, 0, 0, dp(8));
        root.addView(globalProgress, progressParams);

        operationPanel = card();
        operationPanel.setPadding(dp(14), dp(12), dp(14), dp(12));
        operationPanel.setVisibility(View.GONE);
        root.addView(operationPanel, matchWrapMargin(0, dp(10), 0, dp(8)));
        operationTitle = text("Working", 15, TEXT, true);
        operationDetail = text("", 12, MUTED, false);
        operationPanel.addView(operationTitle);
        operationPanel.addView(operationDetail);

        LinearLayout hero = glassCard();
        hero.setPadding(dp(16), dp(14), dp(16), dp(14));
        root.addView(hero, matchWrapMargin(0, dp(12), 0, dp(14)));
        daemonState = addMetric(hero, "Controller", "Starting");
        serverState = addMetric(hero, "Server", "Unknown");
        joinAddress = addMetric(hero, "Join address", defaultJoinAddress());
        version = addMetric(hero, "Minecraft", "-");
        memory = addMetric(hero, "Memory", "-");

        pageTitle = text("", 20, TEXT, true);
        root.addView(pageTitle);

        content = root;

        LinearLayout tabs = bottomNav();
        dashboardTab = tabButton("⌂\nDashboard", "dashboard");
        serversTab = tabButton("▤\nServers", "servers");
        settingsTab = tabButton("⚙\nSettings", "settings");
        tabs.addView(dashboardTab, weightedTabParams());
        tabs.addView(serversTab, weightedTabParams());
        tabs.addView(settingsTab, weightedTabParams());
        screen.addView(tabs);

        setPage("dashboard");
        return screen;
    }

    private void setPage(String page) {
        if (!navigatingBack && content != null && currentPage != null && !currentPage.equals(page)) {
            pageHistory.add(currentPage);
        }
        currentPage = page;
        if (content == null) return;
        content.removeAllViews();
        buildScreenChrome(page);
        updateTabs();
        if ("setup".equals(page)) buildSetupPage();
        else if ("servers".equals(page)) buildServersPage();
        else if ("settings".equals(page)) buildSettingsPage();
        else if ("backups".equals(page)) buildBackupsPage();
        else if ("mods".equals(page)) buildModsPage();
        else buildDashboardPage();
        setControllerActionsEnabled(controllerOnline);
    }

    private void buildScreenChrome(String page) {
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        content.addView(header, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(70)));

        LinearLayout brand = row();
        brand.setGravity(Gravity.CENTER_VERTICAL);
        header.addView(brand, "servers".equals(page) ? new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1) : new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        TextView cubeBlock = new TextView(this);
        cubeBlock.setBackground(voxelBg());
        LinearLayout.LayoutParams cubeBlockParams = new LinearLayout.LayoutParams(dp(36), dp(36));
        cubeBlockParams.setMargins(0, 0, dp(12), 0);
        brand.addView(cubeBlock, cubeBlockParams);
        brand.addView(text(screenBrand(page), "servers".equals(page) ? 21 : 28, TEXT, true));

        if ("servers".equals(page)) {
            TextView center = text("Servers", 24, TEXT, true);
            center.setGravity(Gravity.CENTER);
            header.addView(center, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        }

        TextView avatar = text("MM", 13, TEXT, true);
        avatar.setGravity(Gravity.CENTER);
        avatar.setBackground(makeBg(0xff1d2d46, controllerOnline ? GREEN : 0xff657187, 24));
        header.addView(avatar, new LinearLayout.LayoutParams(dp(48), dp(48)));

        notice = text(controllerOnline ? "" : "Starting local controller...", 13, WARNING, false);
        notice.setPadding(dp(12), dp(8), dp(12), dp(8));
        notice.setBackground(makeBg(0xff121d2d, STROKE, 14));
        notice.setVisibility(controllerOnline ? View.GONE : View.VISIBLE);
        content.addView(notice);

        globalProgress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        globalProgress.setIndeterminate(true);
        globalProgress.setVisibility(operationRunning ? View.VISIBLE : View.GONE);
        LinearLayout.LayoutParams progressParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(4));
        progressParams.setMargins(0, 0, 0, dp(8));
        content.addView(globalProgress, progressParams);

        operationPanel = card();
        operationPanel.setPadding(dp(14), dp(12), dp(14), dp(12));
        operationPanel.setVisibility(operationRunning ? View.VISIBLE : View.GONE);
        content.addView(operationPanel, matchWrapMargin(0, dp(10), 0, dp(8)));
        operationTitle = text(operationRunning ? "Working" : "Ready", 15, TEXT, true);
        operationDetail = text(operationMessage, 12, MUTED, false);
        operationPanel.addView(operationTitle);
        operationPanel.addView(operationDetail);

        pageTitle = text(screenTitle(page), 28, TEXT, true);
        if (!"dashboard".equals(page) && !"servers".equals(page) && !"settings".equals(page)) {
            pageTitle.setPadding(0, dp(6), 0, dp(8));
            content.addView(pageTitle);
        }
    }

    private String screenBrand(String page) {
        if ("settings".equals(page)) return "Settings";
        return "Mc Phone";
    }

    private String screenTitle(String page) {
        if ("servers".equals(page)) return "Servers";
        if ("settings".equals(page)) return "Settings";
        if ("setup".equals(page)) return "Setup Server";
        if ("backups".equals(page)) return "Backups";
        if ("mods".equals(page)) return "Mods";
        return "Dashboard";
    }

    private String screenSubtitle(String page) {
        if ("servers".equals(page)) return "Manage configured worlds";
        if ("settings".equals(page)) return "Profile, preferences, and advanced tools";
        if ("setup".equals(page)) return "Guided Android-first server setup";
        if ("backups".equals(page)) return "Restore points and rollback";
        if ("mods".equals(page)) return "Install plugins and server mods";
        return "Phone Minecraft server";
    }

    private void buildDashboardPage() {
        pageTitle.setText("Dashboard");
        if (latestStatus == null || !activeServerInstalled()) {
            LinearLayout empty = glassCard();
            empty.setPadding(dp(24), dp(24), dp(24), dp(24));
            content.addView(empty, matchWrapMargin(0, dp(18), 0, dp(16)));
            empty.addView(text("No server configured", 26, TEXT, true));
            empty.addView(text("Create a phone-hosted server with a guided setup. Pick runtime, version, memory, and player limit before MineMux downloads anything.", 15, MUTED, false));
            Button setup = primaryButton("Set Up Server");
            setup.setOnClickListener(v -> {
                setupStep = 0;
                setPage("setup");
            });
            empty.addView(setup, fullWidthButtonParams());
        } else {
            LinearLayout live = glassCard();
            live.setPadding(dp(24), dp(24), dp(24), dp(24));
            content.addView(live, matchWrapMargin(0, dp(18), 0, dp(18)));
            LinearLayout titleRow = row();
            titleRow.setGravity(Gravity.TOP);
            live.addView(titleRow, matchWrap());
            titleRow.addView(thumbnail("overworld"), new LinearLayout.LayoutParams(dp(86), dp(78)));
            LinearLayout titleTexts = column();
            LinearLayout.LayoutParams titleParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
            titleParams.setMargins(dp(18), 0, 0, 0);
            titleRow.addView(titleTexts, titleParams);
            LinearLayout nameRow = row();
            nameRow.setGravity(Gravity.CENTER_VERTICAL);
            titleTexts.addView(nameRow, matchWrap());
            nameRow.addView(text(activeServerName(), 28, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            nameRow.addView(statusPill(activeServerRunning() ? "Running" : "Ready", activeServerRunning() ? GREEN : MUTED));
            titleTexts.addView(text(joinAddressText(), 16, MUTED, false));
            TextView versionLine = text("Minecraft " + versionText() + "  .  " + activeLoaderLabel(), 15, MUTED, false);
            versionLine.setPadding(0, dp(10), 0, 0);
            titleTexts.addView(versionLine);

            LinearLayout controls = row();
            live.addView(controls, matchWrapMargin(0, dp(22), 0, 0));
            Button start = activeServerRunning() ? dangerButton("■ Stop") : primaryButton("▶ Start");
            if (activeServerRunning()) start.setOnClickListener(v -> actionButton(start, "/api/server/stop", "{}", "Stopping server..."));
            else start.setOnClickListener(v -> actionButton(start, "/api/server/start", "{}", "Starting server..."));
            controls.addView(start, weightedButtonParams());
            Button restart = secondaryButton("↻ Restart");
            restart.setOnClickListener(v -> actionButton(restart, "/api/server/restart", "{}", "Restarting server..."));
            controls.addView(restart, weightedButtonParams());
            Button backup = secondaryButton("▣ Backup");
            backup.setOnClickListener(v -> actionButton(backup, "/api/backups/create", "{}", "Creating backup..."));
            controls.addView(backup, weightedButtonParams());
            Button details = secondaryButton("ⓘ Details");
            details.setOnClickListener(v -> setPage("servers"));
            controls.addView(details, weightedButtonParams());

            LinearLayout statsA = row();
            live.addView(statsA, matchWrapMargin(0, dp(22), 0, dp(10)));
            statsA.addView(statCard("TPS", tpsText() + " / 20", GREEN), weightedButtonParams());
            statsA.addView(statCard("Players", playerCountText() + " / " + maxPlayersText(), ACCENT), weightedButtonParams());
            statsA.addView(statCard("CPU", cpuText(), 0xff4dbbff), weightedButtonParams());
            LinearLayout statsB = row();
            live.addView(statsB, matchWrapMargin(0, 0, 0, dp(10)));
            statsB.addView(statCard("RAM", ramText(), PURPLE), weightedButtonParams());
            statsB.addView(statCard("Disk", diskText(), WARNING), weightedButtonParams());
            statsB.addView(statCard("Uptime", uptimeText(), MUTED), weightedButtonParams());

            LinearLayout graphs = row();
            live.addView(graphs, matchWrapMargin(0, dp(12), 0, 0));
            graphs.addView(graphCard("TPS (Live)", tpsText(), GREEN), weightedButtonParams());
            graphs.addView(graphCard("Players (Live)", playerCountText() + " / " + maxPlayersText(), ACCENT), weightedButtonParams());

            LinearLayout playerActions = row();
            live.addView(playerActions, matchWrapMargin(0, dp(12), 0, 0));
            Button listPlayers = secondaryButton("List Players");
            listPlayers.setOnClickListener(v -> actionButton(listPlayers, "/api/server/command", "{\"command\":\"list\"}", "Asking server for player list..."));
            playerActions.addView(listPlayers, weightedButtonParams());
            Button saveAll = secondaryButton("Save World");
            saveAll.setOnClickListener(v -> actionButton(saveAll, "/api/server/command", "{\"command\":\"save-all\"}", "Saving world..."));
            playerActions.addView(saveAll, weightedButtonParams());
        }

        LinearLayout activity = glassCard();
        activity.setPadding(dp(24), dp(20), dp(24), dp(20));
        content.addView(activity, matchWrapMargin(0, 0, 0, dp(18)));
        LinearLayout activityHeader = row();
        activityHeader.setGravity(Gravity.CENTER_VERTICAL);
        activity.addView(activityHeader, matchWrap());
        activityHeader.addView(text("Recent Activity", 22, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        TextView viewBackups = text("View all >", 15, ACCENT, true);
        viewBackups.setOnClickListener(v -> setPage("backups"));
        activityHeader.addView(viewBackups);
        activity.addView(activityRow("Controller " + (controllerOnline ? "online" : "starting"), "MineMux local daemon", controllerOnline ? GREEN : WARNING));
        activity.addView(activityRow("Active server", activeServerInstalled() ? activeServerName() : "No server configured", ACCENT));
        activity.addView(activityRow("Backups", "Open restore points and rollback tools", PURPLE));
        logs = text("No logs yet.", 12, 0xffd9e7ff, false);
        logs.setTypeface(Typeface.MONOSPACE);
        logs.setBackground(makeBg(0xff08111f, STROKE, 10));
        logs.setPadding(dp(10), dp(10), dp(10), dp(10));
        activity.addView(logs, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(120)));
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
        panel.addView(text("Choose the runtime MineMux should install. Paper is best for plugins, Vanilla is simplest, and Forge-family loaders are for modded servers.", 13, MUTED, false));
        Spinner versionSpinner = spinner(new String[]{wizardVersion, "latest-compatible", "1.21.8", "1.21.7", "1.21.6", "1.21.5", "1.21.4", "1.20.6", "1.20.4"});
        addField(panel, "Minecraft version", versionSpinner);
        Spinner loaderSpinner = spinner(new String[]{"paper - Paper Plugins", "vanilla - Vanilla", "quilt - Quilt Mods", "forge - Forge Mods", "neoforge - NeoForge Mods"});
        addField(panel, "Runtime", loaderSpinner);
        loadSetupOptions(versionSpinner, loaderSpinner);
        setupNav(panel, () -> {
            setupStep = 0;
            setPage("setup");
        }, () -> {
            String loaderChoice = loaderSpinner.getSelectedItem().toString();
            wizardVersion = versionSpinner.getSelectedItem().toString();
            wizardLoader = loaderId(loaderChoice);
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
        panel.addView(summaryLine("Runtime", wizardLoader));
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
                + "\"loader\":\"" + escapeJson(wizardLoader) + "\","
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

    private void buildServersPage() {
        pageTitle.setText("Servers");
        LinearLayout list = column();
        content.addView(list, matchWrapMargin(0, dp(14), 0, dp(8)));
        LinearLayout countRow = row();
        countRow.setGravity(Gravity.CENTER);
        content.addView(countRow, matchWrapMargin(0, 0, 0, dp(10)));
        LinearLayout detail = column();
        content.addView(detail, matchWrap());
        loadServersPage(list, countRow, detail);
    }

    private void loadServersPage(LinearLayout list, LinearLayout countRow, LinearLayout detail) {
        list.removeAllViews();
        detail.removeAllViews();
        getJson("/api/servers", body -> {
            try {
                JSONArray servers = new JSONObject(body).optJSONArray("servers");
                if (servers == null || servers.length() == 0) {
                    list.addView(emptyState("No server profiles yet. Create one in Setup."));
                    Button create = primaryButton("Create New Server");
                    create.setOnClickListener(v -> {
                        setupStep = 0;
                        wizardServerId = "server-" + System.currentTimeMillis() / 1000;
                        wizardServerName = "New Server";
                        setPage("setup");
                    });
                    detail.addView(create, fullWidthButtonParams());
                    return;
                }
                JSONObject selected = servers.getJSONObject(0);
                for (int i = 0; i < servers.length(); i++) {
                    JSONObject server = servers.getJSONObject(i);
                    if (server.optBoolean("active")) selected = server;
                }
                for (int i = 0; i < servers.length(); i++) {
                    JSONObject server = servers.getJSONObject(i);
                    boolean isSelected = server.optString("id").equals(selected.optString("id"));
                    list.addView(serverListRow(server, isSelected, detail));
                }
                countRow.removeAllViews();
                countRow.addView(text(servers.length() + (servers.length() == 1 ? " server" : " servers"), 14, MUTED, false));
                renderServerDetail(detail, selected);
            } catch (Exception e) {
                list.addView(emptyState(e.getMessage()));
            }
        }, error -> list.addView(emptyState(error)));
    }

    private View serverListRow(JSONObject server, boolean selected, LinearLayout detail) {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(12), dp(8), dp(12), dp(8));
        row.setBackground(makeBg(0xff101a2a, selected ? ACCENT : STROKE, 16));
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(10)));
        row.addView(thumbnail(server.optBoolean("running") ? "overworld" : "cave"), new LinearLayout.LayoutParams(dp(76), dp(58)));

        JSONObject profile = server.optJSONObject("profile");
        LinearLayout copy = column();
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        copyParams.setMargins(dp(14), 0, dp(8), 0);
        row.addView(copy, copyParams);
        copy.addView(text(server.optString("name", server.optString("id", "Server")), 20, TEXT, true));
        String mc = profile == null ? "-" : profile.optString("minecraftVersion", "-");
        String loader = profile == null ? "-" : profile.optString("loader", "-");
        copy.addView(text("Minecraft " + mc + "  .  " + loader, 14, MUTED, false));
        row.addView(statusPill(server.optBoolean("running") ? "Running" : server.optBoolean("installed") ? "Ready" : "Offline", server.optBoolean("running") ? GREEN : server.optBoolean("installed") ? MUTED : ERROR));
        row.addView(text(">", 22, MUTED, true));
        row.setOnClickListener(v -> renderServerDetail(detail, server));
        return row;
    }

    private void renderServerDetail(LinearLayout parent, JSONObject server) {
        parent.removeAllViews();
        LinearLayout detail = glassCard();
        detail.setPadding(dp(22), dp(20), dp(22), dp(20));
        parent.addView(detail, matchWrapMargin(0, 0, 0, dp(18)));

        JSONObject profile = server.optJSONObject("profile");
        LinearLayout top = row();
        top.setGravity(Gravity.CENTER_VERTICAL);
        detail.addView(top, matchWrap());
        top.addView(thumbnail("overworld"), new LinearLayout.LayoutParams(dp(78), dp(58)));
        LinearLayout copy = column();
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        copyParams.setMargins(dp(14), 0, dp(10), 0);
        top.addView(copy, copyParams);
        copy.addView(text(server.optString("name", "Server"), 26, TEXT, true));
        copy.addView(text((server.optBoolean("running") ? "● Running" : "● Ready"), 16, server.optBoolean("running") ? GREEN : MUTED, true));
        copy.addView(text(server.optString("joinAddress", joinAddressText()), 14, MUTED, false));
        LinearLayout players = column();
        top.addView(players, new LinearLayout.LayoutParams(dp(96), ViewGroup.LayoutParams.WRAP_CONTENT));
        players.addView(text("Players", 13, MUTED, false));
        players.addView(text(playerCountText() + " / " + (profile == null ? "-" : profile.optInt("maxPlayers", 0)), 24, TEXT, true));
        players.addView(progressBar(GREEN, playerProgressPercent(profile)));

        LinearLayout info = row();
        info.setPadding(0, dp(16), 0, dp(14));
        detail.addView(info, matchWrap());
        info.addView(infoCell("Version", profile == null ? "-" : profile.optString("minecraftVersion", "-")), weightedButtonParams());
        info.addView(infoCell("Software", profile == null ? "-" : profile.optString("loader", "-")), weightedButtonParams());
        info.addView(infoCell("Type", profile == null ? "-" : profile.optString("serverType", "Survival")), weightedButtonParams());
        info.addView(infoCell("Uptime", uptimeText()), weightedButtonParams());

        LinearLayout actionsA = row();
        detail.addView(actionsA, matchWrapMargin(0, 0, 0, dp(8)));
        Button stop = server.optBoolean("running") ? dangerButton("■ Stop") : primaryButton("▶ Start");
        stop.setOnClickListener(v -> actionButton(stop, server.optBoolean("running") ? "/api/server/stop" : "/api/server/start", "{}", server.optBoolean("running") ? "Stopping server..." : "Starting server..."));
        actionsA.addView(stop, weightedButtonParams());
        Button restart = secondaryButton("↻ Restart");
        restart.setOnClickListener(v -> actionButton(restart, "/api/server/restart", "{}", "Restarting server..."));
        actionsA.addView(restart, weightedButtonParams());
        Button backups = secondaryButton("▣ Backups");
        backups.setOnClickListener(v -> setPage("backups"));
        actionsA.addView(backups, weightedButtonParams());

        LinearLayout actionsB = row();
        detail.addView(actionsB, matchWrapMargin(0, 0, 0, dp(10)));
        Button mods = secondaryButton("✚ Mods");
        mods.setOnClickListener(v -> setPage("mods"));
        actionsB.addView(mods, weightedButtonParams());
        actionsB.addView(secondaryButton("⇧ JAR"), weightedButtonParams());
        actionsB.addView(secondaryButton("⬡ Modrinth"), weightedButtonParams());

        LinearLayout backupsCard = sectionCard("Backups", "View all >", () -> setPage("backups"));
        detail.addView(backupsCard, matchWrapMargin(0, dp(4), 0, dp(10)));
        LinearLayout backupRows = column();
        backupsCard.addView(backupRows, matchWrap());
        loadBackups(backupRows);

        LinearLayout settings = sectionCard("Server Settings", "", null);
        detail.addView(settings, matchWrap());
        CheckBox restartCrash = settingCheckBox("Auto-restart on crash", "Restart the server automatically if it crashes.", profileFeatureBool("restartOnCrash", true));
        settings.addView(restartCrash, matchWrap());
        restartCrash.setOnCheckedChangeListener((button, checked) -> postJson("/api/config", "{\"restartOnCrash\":" + checked + "}", "Saving restart setting...", () -> setNotice("Restart setting saved.", false)));
        CheckBox autoStart = settingCheckBox("Start on boot", "Start the active server when MineMux daemon starts.", profileFeatureBool("autoStart", false));
        settings.addView(autoStart, matchWrap());
        autoStart.setOnCheckedChangeListener((button, checked) -> postJson("/api/config", "{\"autoStart\":" + checked + "}", "Saving start setting...", () -> setNotice("Start setting saved.", false)));
    }

    private View serverRow(JSONObject server) {
        LinearLayout row = glassCard();
        row.setPadding(dp(14), dp(12), dp(14), dp(12));
        row.setOrientation(LinearLayout.VERTICAL);
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(8)));
        boolean active = server.optBoolean("active");
        String id = server.optString("id", "main");
        String name = server.optString("name", id);
        LinearLayout top = row();
        top.setGravity(Gravity.CENTER_VERTICAL);
        row.addView(top, matchWrap());
        LinearLayout copy = column();
        top.addView(copy, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        copy.addView(text(name, 18, TEXT, true));
        JSONObject profile = server.optJSONObject("profile");
        String runtime = profile == null ? "-" : profile.optString("loader", "-");
        String mc = profile == null ? "-" : profile.optString("minecraftVersion", "-");
        copy.addView(text("Minecraft " + mc + "  /  " + runtime, 13, MUTED, false));
        top.addView(statusPill(server.optBoolean("running") ? "Running" : active ? "Active" : server.optBoolean("installed") ? "Ready" : "Setup", server.optBoolean("running") ? GREEN : active ? ACCENT : MUTED));
        row.addView(text(server.optBoolean("installed") ? server.optString("joinAddress", "") : "Needs setup", 12, MUTED, false));
        if (!active) {
            Button use = secondaryButton("Use This Server");
            use.setOnClickListener(v -> actionButton(use, "/api/servers/switch", "{\"serverId\":\"" + escapeJson(id) + "\"}", "Switching server..."));
            row.addView(use, fullWidthButtonParams());
        }
        Button details = secondaryButton("Details");
        details.setOnClickListener(v -> showServerDetails(server));
        row.addView(details, fullWidthButtonParams());
        return row;
    }

    private void showServerDetails(JSONObject server) {
        currentPage = "servers";
        content.removeAllViews();
        updateTabs();
        pageTitle.setText(server.optString("name", "Server"));
        LinearLayout detail = card();
        detail.setPadding(dp(14), dp(14), dp(14), dp(14));
        content.addView(detail, matchWrapMargin(0, dp(6), 0, dp(10)));
        JSONObject profile = server.optJSONObject("profile");
        detail.addView(summaryLine("ID", server.optString("id", "-")));
        detail.addView(summaryLine("Status", server.optBoolean("running") ? "Running" : server.optBoolean("installed") ? "Ready" : "Needs setup"));
        detail.addView(summaryLine("Join", server.optString("joinAddress", "-")));
        if (profile != null) {
            detail.addView(summaryLine("Runtime", profile.optString("loader", "-")));
            detail.addView(summaryLine("Minecraft", profile.optString("minecraftVersion", "-")));
            detail.addView(summaryLine("Memory", profile.optInt("memoryMb", 0) + " MB"));
            detail.addView(summaryLine("Players", String.valueOf(profile.optInt("maxPlayers", 0))));
        }
        Button back = secondaryButton("Back to Servers");
        back.setOnClickListener(v -> setPage("servers"));
        content.addView(back, fullWidthButtonParams());
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
        LinearLayout hero = glassCard();
        hero.setPadding(dp(16), dp(16), dp(16), dp(16));
        content.addView(hero, matchWrapMargin(0, dp(6), 0, dp(10)));
        hero.addView(text("Restore points", 22, TEXT, true));
        TextView intro = text("Backups include the server name and date, so restore points stay readable when you manage multiple worlds.", 13, MUTED, false);
        intro.setPadding(0, dp(4), 0, dp(8));
        hero.addView(intro);
        LinearLayout actions = row();
        hero.addView(actions, matchWrapMargin(0, dp(6), 0, 0));
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

    private void buildSettingsPage() {
        pageTitle.setText("Settings");
        LinearLayout profile = glassCard();
        profile.setPadding(dp(24), dp(22), dp(24), dp(22));
        content.addView(profile, matchWrapMargin(0, dp(18), 0, dp(18)));
        LinearLayout profileTop = row();
        profileTop.setGravity(Gravity.CENTER_VERTICAL);
        profile.addView(profileTop, matchWrap());
        TextView avatar = text("MP", 24, TEXT, true);
        avatar.setGravity(Gravity.CENTER);
        avatar.setBackground(makeBg(0xff1d2d46, ACCENT, 36));
        profileTop.addView(avatar, new LinearLayout.LayoutParams(dp(72), dp(72)));
        LinearLayout identity = column();
        LinearLayout.LayoutParams identityParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        identityParams.setMargins(dp(18), 0, dp(10), 0);
        profileTop.addView(identity, identityParams);
        identity.addView(text("Mc Phone", 26, TEXT, true));
        identity.addView(text("mcphone@example.com", 15, MUTED, false));
        LinearLayout plan = row();
        plan.setGravity(Gravity.CENTER_VERTICAL);
        identity.addView(plan, matchWrapMargin(0, dp(8), 0, 0));
        plan.addView(statusPill("Pro Plan", GREEN));
        TextView active = text("  . Active", 14, GREEN, true);
        plan.addView(active);
        profileTop.addView(secondaryButton("Edit"));

        LinearLayout stats = row();
        profile.addView(stats, matchWrapMargin(0, dp(22), 0, 0));
        stats.addView(profileStat("▤", activeServerInstalled() ? "1" : "0", "Servers", ACCENT), weightedButtonParams());
        stats.addView(profileStat("↺", "-", "Backups", GREEN), weightedButtonParams());
        stats.addView(profileStat("▣", diskText(), "Storage Used", PURPLE), weightedButtonParams());
        stats.addView(profileStat("□", "Pro", "Plan", ACCENT), weightedButtonParams());

        LinearLayout general = settingRowIcon("⚙", "General Settings", "Configure general app preferences", ACCENT, null);
        content.addView(general, matchWrapMargin(0, 0, 0, dp(18)));

        TextView preferencesTitle = text("Preferences", 17, MUTED, true);
        preferencesTitle.setPadding(dp(2), 0, 0, dp(10));
        content.addView(preferencesTitle);
        LinearLayout preferences = sectionCard("", "", null);
        content.addView(preferences, matchWrapMargin(0, 0, 0, dp(18)));
        preferences.addView(toggleSettingRow("●", "Notifications", "Manage push notifications", WARNING, true, null));
        preferences.addView(toggleSettingRow("◐", "Appearance", "Dark Mode", PURPLE, true, null));
        preferences.addView(toggleSettingRow("▶", "Start on boot", "Start active server when daemon starts", GREEN, profileFeatureBool("autoStart", false),
            checked -> postJson("/api/config", "{\"autoStart\":" + checked + "}", "Saving start setting...", () -> setNotice("Start setting saved.", false))));
        preferences.addView(toggleSettingRow("↻", "Auto-restart", "Restart server automatically after crash", ACCENT, profileFeatureBool("restartOnCrash", true),
            checked -> postJson("/api/config", "{\"restartOnCrash\":" + checked + "}", "Saving restart setting...", () -> setNotice("Restart setting saved.", false))));
        preferences.addView(settingRowIcon("▰", "Storage", "Manage backups and disk usage", WARNING, () -> setPage("backups")));
        preferences.addView(settingRowIcon("✚", "Integrations", "Modrinth, GitHub, Discord", 0xff4ddde4, null));

        TextView serverTitle = text("Server Defaults", 17, MUTED, true);
        serverTitle.setPadding(dp(2), 0, 0, dp(10));
        content.addView(serverTitle);
        LinearLayout defaults = sectionCard("", "", null);
        content.addView(defaults, matchWrapMargin(0, 0, 0, dp(18)));
        EditText memoryInput = input(String.valueOf(profileInt("memoryMb", wizardMemory)));
        addField(defaults, "Memory MB", memoryInput);
        EditText playersInput = input(String.valueOf(profileInt("maxPlayers", wizardPlayers)));
        addField(defaults, "Max players", playersInput);
        Button saveDefaults = primaryButton("Save Defaults");
        saveDefaults.setOnClickListener(v -> {
            int memoryValue = clamp(numberOr(memoryInput, wizardMemory), 512, 8192);
            int playersValue = clamp(numberOr(playersInput, wizardPlayers), 1, 50);
            actionButton(saveDefaults, "/api/config", "{\"memoryMb\":" + memoryValue + ",\"maxPlayers\":" + playersValue + "}", "Saving server defaults...");
        });
        defaults.addView(saveDefaults, fullWidthButtonParams());

        TextView accountTitle = text("Account", 17, MUTED, true);
        accountTitle.setPadding(dp(2), 0, 0, dp(10));
        content.addView(accountTitle);
        LinearLayout account = sectionCard("", "", null);
        content.addView(account, matchWrapMargin(0, 0, 0, dp(14)));
        account.addView(settingRowIcon("⌁", "Linked Accounts", "Manage connected accounts", ACCENT, null));
        account.addView(settingRowIcon("↪", "Sign Out", "Sign out of your account", ERROR, null));
        account.addView(settingRowIcon("⌘", "Terminal", "Open shell recovery", GREEN, () -> startActivity(new Intent(this, TermuxActivity.class))));
        account.addView(settingRowIcon("▣", "Power UI", "Open advanced web console", PURPLE, () -> startActivity(new Intent(this, MineMuxWebActivity.class))));

        TextView footer = text("App Version " + appVersionText() + "\n© 2024 Mc Phone. All rights reserved.", 11, TERTIARY, false);
        footer.setGravity(Gravity.CENTER);
        footer.setPadding(0, dp(4), 0, dp(18));
        content.addView(footer, matchWrap());
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
        LinearLayout row = glassCard();
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

    private View settingsRow(String title, String subtitle, int color, @Nullable Runnable action) {
        LinearLayout row = activityRow(title, subtitle, color);
        if (action != null) row.setOnClickListener(v -> action.run());
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
                if (daemonState != null) daemonState.setText("Online");
                if (serverState != null) serverState.setText(server.optBoolean("running") ? "Running" : server.optBoolean("installed") ? "Ready" : "Needs Setup");
                if (joinAddress != null) joinAddress.setText(server.optString("joinAddress", defaultJoinAddress()));
                if (version != null) version.setText(profile != null ? profile.optString("minecraftVersion", "-") : "-");
                if (memory != null) memory.setText(profile != null ? profile.optInt("memoryMb", 0) + " MB" : "-");
                setControllerActionsEnabled(true);
                setNotice(server.optString("lastError", ""), server.has("lastError") && !server.optString("lastError").isEmpty());
                if ("dashboard".equals(currentPage) || operationRunning) refreshLogs();
                boolean installedNow = server.optBoolean("installed");
                boolean runningNow = server.optBoolean("running");
                if ("dashboard".equals(currentPage)
                    && (lastDashboardInstalled == null || lastDashboardInstalled != installedNow || lastDashboardRunning == null || lastDashboardRunning != runningNow)) {
                    lastDashboardInstalled = installedNow;
                    lastDashboardRunning = runningNow;
                    setPage("dashboard");
                }
            } catch (Exception e) {
                setNotice(e.getMessage(), true);
            }
        }, error -> {
            failedPolls++;
            controllerOnline = false;
            setControllerActionsEnabled(false);
            if (daemonState != null) daemonState.setText("Starting");
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
        notice.setVisibility(value == null || value.isEmpty() || "Ready".equals(value) ? View.GONE : View.VISIBLE);
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
        return button == dashboardTab || button == serversTab || button == settingsTab;
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

    private String uptimeText() {
        try {
            long seconds = latestStatus.getJSONObject("server").optLong("uptimeSec", 0);
            if (seconds <= 0) return "-";
            long minutes = seconds / 60;
            long hours = minutes / 60;
            if (hours > 0) return hours + "h " + (minutes % 60) + "m";
            return minutes + "m";
        } catch (Exception e) {
            return "-";
        }
    }

    private String tpsText() {
        try {
            double tps = latestStatus.getJSONObject("server").optDouble("tps", 0);
            return tps <= 0 ? "-" : new DecimalFormat("#.#").format(tps);
        } catch (Exception e) {
            return "-";
        }
    }

    private String playerCountText() {
        try {
            return String.valueOf(latestStatus.getJSONObject("server").optInt("players", 0));
        } catch (Exception e) {
            return "0";
        }
    }

    private String activeServerName() {
        try {
            JSONObject profile = latestStatus.optJSONObject("profile");
            return profile == null ? "MineMux Server" : profile.optString("name", "MineMux Server");
        } catch (Exception e) {
            return "MineMux Server";
        }
    }

    private String joinAddressText() {
        try {
            return latestStatus.getJSONObject("server").optString("joinAddress", defaultJoinAddress());
        } catch (Exception e) {
            return defaultJoinAddress();
        }
    }

    private String versionText() {
        try {
            JSONObject profile = latestStatus.optJSONObject("profile");
            return profile == null ? "-" : profile.optString("minecraftVersion", "-");
        } catch (Exception e) {
            return "-";
        }
    }

    private String memoryText() {
        try {
            JSONObject profile = latestStatus.optJSONObject("profile");
            return profile == null ? "-" : profile.optInt("memoryMb", 0) + " MB";
        } catch (Exception e) {
            return "-";
        }
    }

    private String activeLoader() {
        try {
            JSONObject profile = latestStatus.optJSONObject("profile");
            return profile == null ? "-" : profile.optString("loader", "-");
        } catch (Exception e) {
            return "-";
        }
    }

    private String maxPlayersText() {
        try {
            JSONObject profile = latestStatus.optJSONObject("profile");
            return profile == null ? "-" : String.valueOf(profile.optInt("maxPlayers", 0));
        } catch (Exception e) {
            return "-";
        }
    }

    private boolean profileFeatureBool(String key, boolean fallback) {
        try {
            JSONObject profile = latestStatus == null ? null : latestStatus.optJSONObject("profile");
            if (profile == null) return fallback;
            JSONObject features = profile.optJSONObject("features");
            return features == null ? fallback : features.optBoolean(key, fallback);
        } catch (Exception e) {
            return fallback;
        }
    }

    private int profileInt(String key, int fallback) {
        try {
            JSONObject profile = latestStatus == null ? null : latestStatus.optJSONObject("profile");
            return profile == null ? fallback : profile.optInt(key, fallback);
        } catch (Exception e) {
            return fallback;
        }
    }

    private String cpuText() {
        try {
            JSONObject cpu = latestStatus.getJSONObject("system").getJSONObject("cpu");
            double percent = cpu.optDouble("percent", 0);
            if (percent <= 0) return "Sampling";
            return new DecimalFormat("#.#").format(percent) + "%";
        } catch (Exception e) {
            return "-";
        }
    }

    private String ramText() {
        return systemStorageText("memory");
    }

    private String diskText() {
        return systemStorageText("disk");
    }

    private String systemStorageText(String key) {
        try {
            JSONObject stats = latestStatus.getJSONObject("system").getJSONObject(key);
            double percent = stats.optDouble("percent", 0);
            long used = stats.optLong("usedBytes", 0);
            long total = stats.optLong("totalBytes", 0);
            if (total <= 0) return "-";
            return new DecimalFormat("#").format(percent) + "%  " + bytes(used) + " / " + bytes(total);
        } catch (Exception e) {
            return "-";
        }
    }

    private String appVersionText() {
        try {
            return latestStatus == null ? "MVP" : latestStatus.optString("version", "MVP");
        } catch (Exception e) {
            return "MVP";
        }
    }

    private CheckBox settingCheckBox(String title, String subtitle, boolean checked) {
        CheckBox box = new CheckBox(this);
        box.setText(title + "\n" + subtitle);
        box.setTextColor(TEXT);
        box.setTextSize(14);
        box.setChecked(checked);
        box.setButtonTintList(android.content.res.ColorStateList.valueOf(ACCENT));
        box.setPadding(dp(4), dp(8), dp(4), dp(8));
        return box;
    }

    private void updateTabs() {
        styleTab(dashboardTab, "dashboard".equals(currentPage));
        styleTab(serversTab, "servers".equals(currentPage));
        styleTab(settingsTab, "settings".equals(currentPage));
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
        button.setContentDescription(screenTitle(page));
        button.setAllCaps(false);
        button.setTextSize(13);
        button.setGravity(Gravity.CENTER);
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        button.setOnClickListener(v -> setPage(page));
        return button;
    }

    private Button primaryButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(ACCENT_TEXT);
        button.setBackground(makeBg(ACCENT, 0xff5aa5ff, 12));
        return button;
    }

    private Button dangerButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(0xffffffff);
        button.setBackground(makeBg(RED, 0xffff6570, 12));
        return button;
    }

    private Button secondaryButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(TEXT);
        button.setBackground(makeBg(PANEL_ALT, STROKE, 12));
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
        button.setTextColor(active ? 0xffffffff : MUTED);
        button.setBackground(makeBg(active ? ACCENT : 0x00000000, active ? 0xff5aa5ff : 0x00000000, 16));
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

    private LinearLayout bottomNav() {
        LinearLayout tabs = row();
        tabs.setGravity(Gravity.CENTER);
        tabs.setPadding(dp(10), dp(10), dp(10), dp(10));
        tabs.setBackground(makeBg(0xff0f1a2b, STROKE, 24));
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(88));
        params.setMargins(dp(20), dp(8), dp(20), dp(18));
        tabs.setLayoutParams(params);
        return tabs;
    }

    private LinearLayout card() {
        LinearLayout layout = column();
        layout.setBackground(makeBg(PANEL, STROKE, 12));
        return layout;
    }

    private LinearLayout glassCard() {
        LinearLayout layout = column();
        layout.setBackground(makeBg(PANEL_SOFT, STROKE, 18));
        return layout;
    }

    private TextView statusPill(String label, int color) {
        TextView pill = text(label, 13, color, true);
        pill.setGravity(Gravity.CENTER);
        pill.setPadding(dp(12), dp(6), dp(12), dp(6));
        int bg = color == GREEN ? 0xff102716 : 0xff182337;
        int stroke = color == GREEN ? 0xff255d35 : STROKE;
        pill.setBackground(makeBg(bg, stroke, 18));
        return pill;
    }

    private LinearLayout statCard(String label, String value, int color) {
        LinearLayout stat = glassCard();
        stat.setPadding(dp(14), dp(12), dp(14), dp(12));
        stat.addView(text(label, 12, MUTED, true));
        TextView valueView = text(value, 24, color, true);
        stat.addView(valueView);
        TextView bar = new TextView(this);
        bar.setBackground(makeBg(color, color, 6));
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(5));
        params.setMargins(0, dp(8), dp(28), 0);
        stat.addView(bar, params);
        return stat;
    }

    private LinearLayout graphCard(String title, String value, int color) {
        LinearLayout graph = glassCard();
        graph.setPadding(dp(12), dp(12), dp(12), dp(12));
        LinearLayout top = row();
        top.setGravity(Gravity.CENTER_VERTICAL);
        graph.addView(top, matchWrap());
        top.addView(text(title, 12, MUTED, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        top.addView(text(value, 13, color, true));

        int[] widths = new int[]{34, 54, 45, 68, 58, 74, 64};
        for (int i = 0; i < widths.length; i++) {
            TextView line = new TextView(this);
            int lineColor = i % 2 == 0 ? color : 0xff31465f;
            line.setBackground(makeBg(lineColor, lineColor, 5));
            LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(dp(widths[i]), dp(5));
            params.setMargins(0, dp(6), 0, 0);
            graph.addView(line, params);
        }
        graph.addView(text("Live history pending", 11, MUTED, false));
        return graph;
    }

    private LinearLayout activityRow(String title, String subtitle, int color) {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(0, dp(12), 0, dp(12));
        TextView dot = text("■", 20, color, true);
        dot.setPadding(0, 0, dp(12), 0);
        row.addView(dot);
        dot.setVisibility(View.GONE);
        TextView dotBlock = new TextView(this);
        dotBlock.setBackground(makeBg(color, color, 9));
        LinearLayout.LayoutParams dotBlockParams = new LinearLayout.LayoutParams(dp(18), dp(18));
        dotBlockParams.setMargins(0, 0, dp(12), 0);
        row.addView(dotBlock, dotBlockParams);
        LinearLayout copy = column();
        row.addView(copy, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        copy.addView(text(title, 14, TEXT, true));
        copy.addView(text(subtitle, 12, MUTED, false));
        row.addView(text(">", 18, MUTED, true));
        return row;
    }

    private GradientDrawable voxelBg() {
        GradientDrawable drawable = new GradientDrawable(GradientDrawable.Orientation.TL_BR, new int[]{0xff49db32, 0xff0db50d, 0xff8b5a2b, 0xff5b321a});
        drawable.setCornerRadius(dp(8));
        return drawable;
    }

    private TextView thumbnail(String kind) {
        int[] colors;
        if ("cave".equals(kind)) colors = new int[]{0xff4b3a2b, 0xff11151c};
        else if ("nether".equals(kind)) colors = new int[]{0xff572979, 0xff1d0e2e};
        else colors = new int[]{0xffd1e7ff, 0xff2c7a3d, 0xff225f31};
        TextView view = new TextView(this);
        GradientDrawable drawable = new GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM, colors);
        drawable.setCornerRadius(dp(16));
        view.setBackground(drawable);
        return view;
    }

    private String activeLoaderLabel() {
        String loader = activeLoader();
        if ("-".equals(loader)) return memoryText();
        return loader + "  " + memoryText();
    }

    private LinearLayout progressBar(int color, int percent) {
        LinearLayout outer = row();
        outer.setBackground(makeBg(0xff273449, 0xff273449, 99));
        outer.setPadding(0, 0, 0, 0);
        LinearLayout fill = new LinearLayout(this);
        fill.setBackground(makeBg(color, color, 99));
        outer.addView(fill, new LinearLayout.LayoutParams(0, dp(6), Math.max(1, percent)));
        TextView rest = new TextView(this);
        outer.addView(rest, new LinearLayout.LayoutParams(0, dp(6), Math.max(1, 100 - percent)));
        return outer;
    }

    private int playerProgressPercent(JSONObject profile) {
        int max = profile == null ? 0 : profile.optInt("maxPlayers", 0);
        if (max <= 0) return 0;
        try {
            int players = latestStatus.getJSONObject("server").optInt("players", 0);
            return clamp(players * 100 / max, 0, 100);
        } catch (Exception e) {
            return 0;
        }
    }

    private LinearLayout infoCell(String label, String value) {
        LinearLayout cell = column();
        cell.setGravity(Gravity.CENTER);
        cell.setPadding(dp(6), dp(10), dp(6), dp(10));
        cell.setBackground(makeBg(0xff101a2a, 0xff24334b, 14));
        TextView labelView = text(label, 12, MUTED, false);
        labelView.setGravity(Gravity.CENTER);
        cell.addView(labelView);
        TextView valueView = text(value, 14, TEXT, true);
        valueView.setGravity(Gravity.CENTER);
        cell.addView(valueView);
        return cell;
    }

    private LinearLayout sectionCard(String title, String actionLabel, @Nullable Runnable action) {
        LinearLayout card = column();
        card.setPadding(dp(16), dp(14), dp(16), dp(14));
        card.setBackground(makeBg(0xff111b2b, STROKE, 18));
        if (title == null || title.isEmpty()) return card;
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        card.addView(header, matchWrap());
        header.addView(text(title, 19, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        if (action != null && actionLabel != null && !actionLabel.isEmpty()) {
            TextView actionView = text(actionLabel, 14, ACCENT, true);
            actionView.setOnClickListener(v -> action.run());
            header.addView(actionView);
        }
        return card;
    }

    private LinearLayout profileStat(String icon, String value, String label, int color) {
        LinearLayout stat = column();
        stat.setGravity(Gravity.CENTER);
        stat.setPadding(dp(4), dp(10), dp(4), dp(10));
        TextView iconView = text(icon, 22, color, true);
        iconView.setGravity(Gravity.CENTER);
        stat.addView(iconView);
        TextView valueView = text(value, 19, TEXT, true);
        valueView.setGravity(Gravity.CENTER);
        stat.addView(valueView);
        TextView labelView = text(label, 12, MUTED, false);
        labelView.setGravity(Gravity.CENTER);
        stat.addView(labelView);
        return stat;
    }

    private LinearLayout settingRowIcon(String icon, String title, String subtitle, int color, @Nullable Runnable action) {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(14), dp(12), dp(14), dp(12));
        row.setBackground(makeBg(0xff111b2b, STROKE, 16));
        TextView iconBox = text(icon, 22, color, true);
        iconBox.setGravity(Gravity.CENTER);
        iconBox.setBackground(makeBg(tintFor(color), tintFor(color), 12));
        row.addView(iconBox, new LinearLayout.LayoutParams(dp(48), dp(48)));
        LinearLayout copy = column();
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        copyParams.setMargins(dp(14), 0, dp(8), 0);
        row.addView(copy, copyParams);
        copy.addView(text(title, 18, title.equals("Sign Out") ? ERROR : TEXT, true));
        copy.addView(text(subtitle, 14, MUTED, false));
        row.addView(text(">", 24, MUTED, true));
        if (action != null) row.setOnClickListener(v -> action.run());
        return row;
    }

    private LinearLayout toggleSettingRow(String icon, String title, String subtitle, int color, boolean checked, @Nullable Consumer<Boolean> action) {
        LinearLayout row = settingRowIcon(icon, title, subtitle, color, null);
        row.removeViewAt(row.getChildCount() - 1);
        CheckBox toggle = new CheckBox(this);
        toggle.setChecked(checked);
        toggle.setButtonTintList(android.content.res.ColorStateList.valueOf(ACCENT));
        toggle.setOnCheckedChangeListener((button, isChecked) -> {
            if (action != null) action.accept(isChecked);
        });
        row.addView(toggle);
        return row;
    }

    private int tintFor(int color) {
        if (color == GREEN) return 0xff113719;
        if (color == WARNING) return 0xff332914;
        if (color == PURPLE) return 0xff271d3f;
        if (color == ERROR) return 0xff351820;
        return 0xff162a4c;
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

    private String loaderId(String label) {
        String value = label == null ? "" : label.trim().toLowerCase();
        int space = value.indexOf(' ');
        if (space > 0) value = value.substring(0, space);
        int dash = value.indexOf('-');
        if (dash > 0) value = value.substring(0, dash).trim();
        if (value.isEmpty()) return "paper";
        return value;
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
