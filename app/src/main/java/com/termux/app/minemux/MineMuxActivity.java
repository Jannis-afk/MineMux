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

    private static final int BG = 0xff07111f;
    private static final int PANEL = 0xff101a2a;
    private static final int PANEL_ALT = 0xff182437;
    private static final int PANEL_SOFT = 0xff0d1726;
    private static final int STROKE = 0xff26364f;
    private static final int TEXT = 0xfff5f7fb;
    private static final int MUTED = 0xff9aa8bc;
    private static final int TERTIARY = 0xff6f7b8f;
    private static final int ACCENT = 0xff2f86ff;
    private static final int GREEN = 0xff58df6c;
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
        dashboardTab = tabButton("⌂", "dashboard");
        serversTab = tabButton("▤", "servers");
        settingsTab = tabButton("⚙", "settings");
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
        else buildDashboardPage();
        setControllerActionsEnabled(controllerOnline);
    }

    private void buildScreenChrome(String page) {
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        content.addView(header, matchWrap());

        LinearLayout titleBlock = column();
        header.addView(titleBlock, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        LinearLayout brand = row();
        brand.setGravity(Gravity.CENTER_VERTICAL);
        titleBlock.addView(brand);
        TextView cubeBlock = new TextView(this);
        cubeBlock.setBackground(makeBg(GREEN, 0xff86ff95, 7));
        LinearLayout.LayoutParams cubeBlockParams = new LinearLayout.LayoutParams(dp(24), dp(24));
        cubeBlockParams.setMargins(0, 0, dp(10), 0);
        brand.addView(cubeBlock, cubeBlockParams);
        brand.addView(text(screenBrand(page), 30, TEXT, true));
        titleBlock.addView(text(screenSubtitle(page), 13, MUTED, false));

        TextView avatar = text("MM", 13, TEXT, true);
        avatar.setGravity(Gravity.CENTER);
        avatar.setBackground(makeBg(0xff1d2d46, controllerOnline ? GREEN : STROKE, 24));
        header.addView(avatar, new LinearLayout.LayoutParams(dp(48), dp(48)));

        notice = text("Starting local controller...", 14, WARNING, false);
        notice.setPadding(0, dp(14), 0, dp(10));
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
        pageTitle.setPadding(0, dp(6), 0, dp(8));
        content.addView(pageTitle);
    }

    private String screenBrand(String page) {
        if ("settings".equals(page)) return "Settings";
        return "MineMux";
    }

    private String screenTitle(String page) {
        if ("servers".equals(page)) return "Servers";
        if ("settings".equals(page)) return "Settings";
        if ("setup".equals(page)) return "Setup Server";
        if ("backups".equals(page)) return "Backups";
        return "Dashboard";
    }

    private String screenSubtitle(String page) {
        if ("servers".equals(page)) return "Manage configured worlds";
        if ("settings".equals(page)) return "Profile, preferences, and advanced tools";
        if ("setup".equals(page)) return "Guided Android-first server setup";
        if ("backups".equals(page)) return "Restore points and rollback";
        return "Phone Minecraft server";
    }

    private void buildDashboardPage() {
        pageTitle.setText("Dashboard");
        if (latestStatus == null || !activeServerInstalled()) {
            LinearLayout empty = glassCard();
            empty.setPadding(dp(18), dp(22), dp(18), dp(22));
            content.addView(empty, matchWrapMargin(0, dp(8), 0, dp(10)));
            empty.addView(text("No server configured", 24, TEXT, true));
            empty.addView(text("Create a phone-hosted server with a guided setup. Pick runtime, version, memory, and player limit before MineMux downloads anything.", 14, MUTED, false));
            Button setup = primaryButton("Set Up Server");
            setup.setOnClickListener(v -> {
                setupStep = 0;
                setPage("setup");
            });
            empty.addView(setup, fullWidthButtonParams());
        } else {
            LinearLayout live = glassCard();
            live.setPadding(dp(18), dp(18), dp(18), dp(18));
            content.addView(live, matchWrapMargin(0, dp(8), 0, dp(10)));
            LinearLayout titleRow = row();
            titleRow.setGravity(Gravity.CENTER_VERTICAL);
            live.addView(titleRow, matchWrap());
            LinearLayout titleTexts = column();
            titleRow.addView(titleTexts, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            titleTexts.addView(text(activeServerName(), 25, TEXT, true));
            titleTexts.addView(text(joinAddressText(), 15, MUTED, false));
            titleRow.addView(statusPill(activeServerRunning() ? "Running" : "Ready", activeServerRunning() ? GREEN : MUTED));
            live.addView(text("Minecraft " + versionText() + "  /  " + activeLoader() + "  /  " + memoryText(), 14, MUTED, false));

            LinearLayout controls = row();
            live.addView(controls, matchWrapMargin(0, dp(14), 0, 0));
            Button start = activeServerRunning() ? dangerButton("Stop") : primaryButton("Start");
            if (activeServerRunning()) start.setOnClickListener(v -> actionButton(start, "/api/server/stop", "{}", "Stopping server..."));
            else start.setOnClickListener(v -> actionButton(start, "/api/server/start", "{}", "Starting server..."));
            controls.addView(start, weightedButtonParams());
            Button restart = secondaryButton("Restart");
            restart.setOnClickListener(v -> actionButton(restart, "/api/server/restart", "{}", "Restarting server..."));
            controls.addView(restart, weightedButtonParams());
            Button backup = secondaryButton("Backup");
            backup.setOnClickListener(v -> actionButton(backup, "/api/backups/create", "{}", "Creating backup..."));
            controls.addView(backup, weightedButtonParams());

            LinearLayout statsA = row();
            content.addView(statsA, matchWrapMargin(0, 0, 0, dp(8)));
            statsA.addView(statCard("TPS", tpsText() + " / 20", GREEN), weightedButtonParams());
            statsA.addView(statCard("Players", playerCountText() + " / " + maxPlayersText(), ACCENT), weightedButtonParams());
            LinearLayout statsB = row();
            content.addView(statsB, matchWrapMargin(0, 0, 0, dp(8)));
            statsB.addView(statCard("CPU", cpuText(), ACCENT), weightedButtonParams());
            statsB.addView(statCard("RAM", ramText(), PURPLE), weightedButtonParams());
            LinearLayout statsC = row();
            content.addView(statsC, matchWrapMargin(0, 0, 0, dp(8)));
            statsC.addView(statCard("Disk", diskText(), WARNING), weightedButtonParams());
            statsC.addView(statCard("Uptime", uptimeText(), GREEN), weightedButtonParams());

            LinearLayout graphs = row();
            content.addView(graphs, matchWrapMargin(0, 0, 0, dp(8)));
            graphs.addView(graphCard("TPS live", tpsText() + " / 20", GREEN), weightedButtonParams());
            graphs.addView(graphCard("Players live", playerCountText() + " / " + maxPlayersText(), ACCENT), weightedButtonParams());

            LinearLayout playerActions = row();
            content.addView(playerActions, matchWrapMargin(0, 0, 0, dp(8)));
            Button listPlayers = secondaryButton("List Players");
            listPlayers.setOnClickListener(v -> actionButton(listPlayers, "/api/server/command", "{\"command\":\"list\"}", "Asking server for player list..."));
            playerActions.addView(listPlayers, weightedButtonParams());
            Button saveAll = secondaryButton("Save World");
            saveAll.setOnClickListener(v -> actionButton(saveAll, "/api/server/command", "{\"command\":\"save-all\"}", "Saving world..."));
            playerActions.addView(saveAll, weightedButtonParams());
        }

        LinearLayout activity = glassCard();
        activity.setPadding(dp(16), dp(14), dp(16), dp(14));
        content.addView(activity, matchWrapMargin(0, dp(8), 0, dp(10)));
        LinearLayout activityHeader = row();
        activityHeader.setGravity(Gravity.CENTER_VERTICAL);
        activity.addView(activityHeader, matchWrap());
        activityHeader.addView(text("Recent Activity", 18, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        TextView viewBackups = text("View backups", 13, ACCENT, true);
        viewBackups.setOnClickListener(v -> setPage("backups"));
        activityHeader.addView(viewBackups);
        activity.addView(activityRow("Backup", "Create restore points before experimenting", GREEN));
        activity.addView(activityRow("Runtime", "Use Settings for Terminal and Power UI", ACCENT));
        logs = text("No logs yet.", 12, 0xffd9e7ff, false);
        logs.setTypeface(Typeface.MONOSPACE);
        logs.setBackground(makeBg(0xff08111f, STROKE, 10));
        logs.setPadding(dp(10), dp(10), dp(10), dp(10));
        activity.addView(logs, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(150)));
        refreshLogs();

        TextView serversTitle = text("Servers", 17, TEXT, true);
        serversTitle.setPadding(0, dp(6), 0, dp(8));
        content.addView(serversTitle);
        LinearLayout serverList = column();
        content.addView(serverList, matchWrapMargin(0, 0, 0, dp(10)));
        loadServers(serverList);
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
        Button create = primaryButton("Create New Server");
        create.setOnClickListener(v -> {
            setupStep = 0;
            wizardServerId = "server-" + System.currentTimeMillis() / 1000;
            wizardServerName = "New Server";
            setPage("setup");
        });
        content.addView(create, fullWidthButtonParams());
        LinearLayout list = column();
        content.addView(list, matchWrapMargin(0, dp(10), 0, 0));
        loadServers(list);
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
        LinearLayout info = glassCard();
        info.setPadding(dp(16), dp(16), dp(16), dp(16));
        content.addView(info, matchWrapMargin(0, 0, 0, dp(10)));
        LinearLayout profile = row();
        profile.setGravity(Gravity.CENTER_VERTICAL);
        info.addView(profile, matchWrap());
        TextView avatar = text("MM", 18, TEXT, true);
        avatar.setGravity(Gravity.CENTER);
        avatar.setBackground(makeBg(0xff1d2d46, ACCENT, 28));
        profile.addView(avatar, new LinearLayout.LayoutParams(dp(56), dp(56)));
        LinearLayout identity = column();
        LinearLayout.LayoutParams identityParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        identityParams.setMargins(dp(12), 0, 0, 0);
        profile.addView(identity, identityParams);
        identity.addView(text("MineMux Runtime", 22, TEXT, true));
        identity.addView(text(controllerOnline ? "Controller online" : "Controller starting", 13, controllerOnline ? GREEN : WARNING, false));
        info.addView(summaryLine("Join address", joinAddressText()));
        info.addView(summaryLine("Active server", activeServerInstalled() ? activeServerName() : "None configured"));
        info.addView(summaryLine("Package", "com.termux MVP runtime"));

        TextView preferencesTitle = text("Preferences", 13, MUTED, true);
        preferencesTitle.setPadding(0, dp(6), 0, dp(6));
        content.addView(preferencesTitle);
        LinearLayout preferences = glassCard();
        preferences.setPadding(dp(12), dp(8), dp(12), dp(8));
        content.addView(preferences, matchWrapMargin(0, 0, 0, dp(10)));
        CheckBox autoStart = settingCheckBox("Start on boot", "Start the active server when MineMux daemon starts.", profileFeatureBool("autoStart", false));
        preferences.addView(autoStart, matchWrapMargin(0, dp(4), 0, dp(4)));
        autoStart.setOnCheckedChangeListener((button, checked) -> postJson("/api/config", "{\"autoStart\":" + checked + "}", "Saving start setting...", () -> setNotice("Start setting saved.", false)));
        CheckBox restartCrash = settingCheckBox("Auto-restart on crash", "Restart the server automatically after a crash.", profileFeatureBool("restartOnCrash", true));
        preferences.addView(restartCrash, matchWrapMargin(0, dp(4), 0, dp(4)));
        restartCrash.setOnCheckedChangeListener((button, checked) -> postJson("/api/config", "{\"restartOnCrash\":" + checked + "}", "Saving restart setting...", () -> setNotice("Restart setting saved.", false)));
        preferences.addView(settingsRow("Storage", "Backups and disk usage", WARNING, () -> setPage("backups")));
        preferences.addView(settingsRow("Notifications", "Server status alerts are not enabled yet", PURPLE, null));

        TextView serverTitle = text("Server Defaults", 13, MUTED, true);
        serverTitle.setPadding(0, dp(4), 0, dp(6));
        content.addView(serverTitle);
        LinearLayout defaults = glassCard();
        defaults.setPadding(dp(12), dp(12), dp(12), dp(12));
        content.addView(defaults, matchWrapMargin(0, 0, 0, dp(10)));
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

        TextView advancedTitle = text("Advanced", 13, MUTED, true);
        advancedTitle.setPadding(0, dp(4), 0, dp(6));
        content.addView(advancedTitle);
        LinearLayout advanced = glassCard();
        advanced.setPadding(dp(12), dp(8), dp(12), dp(8));
        content.addView(advanced, matchWrapMargin(0, 0, 0, dp(10)));
        advanced.addView(settingsRow("Terminal", "Open shell recovery", GREEN, () -> startActivity(new Intent(this, TermuxActivity.class))));
        advanced.addView(settingsRow("Power UI", "Open advanced web console", PURPLE, () -> startActivity(new Intent(this, MineMuxWebActivity.class))));

        TextView footer = text("App Version " + appVersionText() + "\nMineMux MVP", 11, TERTIARY, false);
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
        button.setTextSize(24);
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
