package com.termux.app.minemux;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ContentResolver;
import android.content.ContentValues;
import android.content.SharedPreferences;
import android.content.res.Configuration;
import android.database.Cursor;
import android.content.Intent;
import android.content.res.ColorStateList;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.provider.Settings;
import android.provider.OpenableColumns;
import android.provider.MediaStore;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.ArrayAdapter;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.SeekBar;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.Nullable;

import com.termux.R;
import com.termux.app.TermuxActivity;
import com.termux.app.TermuxInstaller;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.BufferedReader;
import java.io.InputStream;
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

    private static final int REQUEST_PICK_BACKUP_ZIP = 7110;
    private static final int REQUEST_PICK_SERVER_UPLOAD = 7111;
    private static final int REQUEST_PICK_SETUP_JAR = 7112;
    private static final int REQUEST_PICK_SERVER_ICON = 7113;
    private static final int REQUEST_PICK_RESOURCE_PACK = 7114;
    private static final String PREF_THEME_MODE = "theme_mode";
    private static final String PREF_NOTIFICATIONS = "notifications_enabled";
    private static final String PREF_DEFAULT_MEMORY_MB = "default_memory_mb";
    private static final String PREF_DEFAULT_MAX_PLAYERS = "default_max_players";
    private static final String PREF_DEFAULT_VIEW_DISTANCE = "default_view_distance";
    private static final String PREF_DEFAULT_SIM_DISTANCE = "default_simulation_distance";
    private static final int[] BACKUP_INTERVAL_PRESET_MINUTES = new int[]{30, 60, 180, 360, 720, 1440};

    private int BG = 0xff050505;
    private int PANEL = 0xff111111;
    private int PANEL_ALT = 0xff1a1a1a;
    private int PANEL_SOFT = 0xff0d0d0d;
    private int STROKE = 0xff303030;
    private int TEXT = 0xfff4f4f4;
    private int MUTED = 0xffa8a8a8;
    private int TERTIARY = 0xff737373;
    private int ACCENT = 0xff0db50d;
    private static final int GREEN = 0xff0db50d;
    private static final int DANGER_RED = 0xffd64040;
    private int ACCENT_TEXT = 0xffffffff;
    private int WARNING = 0xffd6d6d6;
    private int ERROR = 0xfff4f4f4;
    private int RED = 0xff202020;
    private int PURPLE = 0xff8a8a8a;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Map<Button, Long> buttonCooldowns = new HashMap<>();
    private final Map<String, Long> actionCooldowns = new HashMap<>();
    private final Map<String, TextView> dashboardMetricViews = new HashMap<>();
    private final Map<String, LinearLayout> dashboardGraphViews = new HashMap<>();
    private final ArrayList<String> pageHistory = new ArrayList<>();
    private final Runnable pollStatus = new Runnable() {
        @Override
        public void run() {
            refreshStatus();
            mainHandler.postDelayed(this, "dashboard".equals(currentPage) ? 1000 : 3000);
        }
    };

    private ScrollView scrollView;
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
    private LinearLayout setupProgressPanel;
    private TextView setupProgressTitle;
    private TextView setupProgressDetail;
    private ProgressBar setupProgressBar;
    private TextView setupLogOutput;
    private Button dashboardTab;
    private Button serversTab;
    private Button settingsTab;
    private Button webButton;
    private static PowerManager.WakeLock serverWakeLock;
    private String currentPage = "dashboard";
    private boolean navigatingBack;
    private long lastBackPressMs;
    private boolean controllerOnline;
    private boolean operationRunning;
    private boolean setupProgressRunning;
    private boolean startingServer;
    private boolean stoppingServer;
    private boolean dashboardCommandFocused;
    private long lastStartRequestedAtMs;
    private long lastStopRequestedAtMs;
    private Boolean lastDashboardInstalled;
    private Boolean lastDashboardRunning;
    private String operationMessage = "";
    private long setupStartedAtMs;
    private long setupEstimateMs = 300000;
    private long lowTpsSinceMs;
    private int setupStep;
    private int failedPolls;
    private JSONObject latestStatus;
    private JSONObject selectedServerDetail;
    private SharedPreferences preferences;
    private String themeMode = "System";
    private String wizardServerId = "main";
    private String wizardServerName = "Main Server";
    private String wizardVersion = "latest-compatible";
    private String wizardLoader = "paper";
    private String wizardSeed = "";
    private String wizardJarMode = "download";
    private String wizardCustomJarId = "";
    private String wizardCustomJarName = "";
    private String pendingIconServerId = "";
    private int wizardMemory = 2048;
    private int wizardPlayers = 8;
    private int wizardViewDistance = 6;
    private int wizardSimulationDistance = 4;
    private boolean wizardEula;
    private boolean wizardCrossplayEnabled;
    private boolean wizardFloodgateEnabled;
    private boolean wizardPlayitEnabled;
    private boolean batteryPromptShown;
    private String fileBrowserPath = "";
    private final ArrayList<Float> tpsHistory = new ArrayList<>();
    private final ArrayList<Float> playerHistory = new ArrayList<>();
    private final ArrayList<Float> cpuHistory = new ArrayList<>();
    private final ArrayList<Float> memoryHistory = new ArrayList<>();
    private final ArrayList<String> wizardPendingMods = new ArrayList<>();

    @Override
    protected void onCreate(@Nullable Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        preferences = getSharedPreferences("minemux", MODE_PRIVATE);
        applyPaletteFromPreferences();
        getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        setContentView(buildContent());
        startMineMuxDaemon();
        maybeRequestBatteryOptimizationExemption();
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
    protected void onDestroy() {
        if (!activeServerRunning()) releaseServerWakeLock();
        super.onDestroy();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, @Nullable Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQUEST_PICK_BACKUP_ZIP && resultCode == RESULT_OK && data != null && data.getData() != null) {
            uploadBackupAndRestore(data.getData());
        } else if (requestCode == REQUEST_PICK_SERVER_UPLOAD && resultCode == RESULT_OK && data != null && data.getData() != null) {
            uploadServerFile(data.getData(), fileBrowserPath);
        } else if (requestCode == REQUEST_PICK_SETUP_JAR && resultCode == RESULT_OK && data != null && data.getData() != null) {
            uploadSetupJar(data.getData());
        } else if (requestCode == REQUEST_PICK_SERVER_ICON && resultCode == RESULT_OK && data != null && data.getData() != null) {
            uploadServerIcon(data.getData(), pendingIconServerId);
        } else if (requestCode == REQUEST_PICK_RESOURCE_PACK && resultCode == RESULT_OK && data != null && data.getData() != null) {
            uploadResourcePack(data.getData());
        }
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
        scrollView = scroll;
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(BG);
        screen.addView(scroll, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));

        LinearLayout root = column();
        root.setPadding(dp(14), dp(8), dp(14), dp(12));
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
        cubeBlock.setBackground(makeBg(GREEN, GREEN, 7));
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
        dashboardTab = tabButton("\u2302", "dashboard");
        serversTab = tabButton("\u25A4", "servers");
        settingsTab = tabButton("\u2699", "settings");
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
        else if ("server-detail".equals(page)) buildServerDetailPage();
        else if ("settings".equals(page)) buildSettingsPage();
        else if ("backups".equals(page)) buildBackupsPage();
        else if ("mods".equals(page)) buildModsPage();
        else if ("server-files".equals(page)) buildServerFilesPage();
        else buildDashboardPage();
        setControllerActionsEnabled(controllerOnline);
    }

    private void buildScreenChrome(String page) {
        TextView safeTop = new TextView(this);
        safeTop.setBackgroundColor(BG);
        content.addView(safeTop, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(10)));

        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        content.addView(header, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(58)));

        ImageView appIcon = new ImageView(this);
        appIcon.setImageResource(getApplicationInfo().icon);
        appIcon.setScaleType(ImageView.ScaleType.FIT_CENTER);
        LinearLayout.LayoutParams cubeBlockParams = new LinearLayout.LayoutParams(dp(34), dp(34));
        cubeBlockParams.setMargins(0, 0, dp(14), 0);
        header.addView(appIcon, cubeBlockParams);
        header.addView(text(screenTitle(page), 28, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

        notice = text(controllerOnline ? "" : "Starting local controller...", 13, WARNING, false);
        notice.setPadding(dp(12), dp(8), dp(12), dp(8));
        notice.setBackground(makeBg(0xff151515, STROKE, 14));
        notice.setVisibility(controllerOnline ? View.GONE : View.VISIBLE);
        content.addView(notice);

        operationPanel = null;
        operationTitle = null;
        operationDetail = null;
        globalProgress = null;
        setupProgressPanel = null;
        setupProgressTitle = null;
        setupProgressDetail = null;
        setupProgressBar = null;
        setupLogOutput = null;
        if ("dashboard".equals(page)) {
            globalProgress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
            globalProgress.setIndeterminate(!setupProgressRunning);
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
        }

        pageTitle = text(screenTitle(page), 28, TEXT, true);
        if (!"dashboard".equals(page) && !"servers".equals(page) && !"settings".equals(page)
            && !"setup".equals(page) && !"server-detail".equals(page) && !"backups".equals(page)) {
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
        if ("server-detail".equals(page)) return selectedServerDetail == null ? "Server" : selectedServerDetail.optString("name", "Server");
        if ("settings".equals(page)) return "Settings";
        if ("setup".equals(page)) return "Setup Server";
        if ("backups".equals(page)) return "Backups";
        if ("mods".equals(page)) return "Mods";
        if ("server-files".equals(page)) return "Files";
        return "Dashboard";
    }

    private String screenSubtitle(String page) {
        if ("servers".equals(page)) return "Manage configured worlds";
        if ("server-detail".equals(page)) return "Server details and actions";
        if ("settings".equals(page)) return "Profile, preferences, and advanced tools";
        if ("setup".equals(page)) return "Guided Android-first server setup";
        if ("backups".equals(page)) return "Restore points and rollback";
        if ("mods".equals(page)) return "Install plugins and server mods";
        return "Phone Minecraft server";
    }

    private void buildDashboardPage() {
        pageTitle.setText("Dashboard");
        dashboardMetricViews.clear();
        dashboardGraphViews.clear();
        if (useRedesignedDashboard()) {
            if (latestStatus == null || !activeServerInstalled()) {
                LinearLayout empty = glassCard();
                empty.setPadding(dp(18), dp(18), dp(18), dp(18));
                content.addView(empty, matchWrapMargin(0, dp(12), 0, dp(12)));
                empty.addView(text("No server configured", 26, TEXT, true));
                empty.addView(text("Create a phone-hosted server with a guided setup. Pick runtime, version, memory, and player limit before MineMux downloads anything.", 15, MUTED, false));
                Button setup = primaryButton("Set Up Server");
                setup.setOnClickListener(v -> {
                    setupStep = 0;
                    setPage("setup");
                });
                empty.addView(setup, fullWidthButtonParams());
            } else {
                content.addView(dashboardServerCard(), matchWrapMargin(0, dp(12), 0, dp(12)));
            }
            buildRecentActivitySection();
            return;
        }
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
            if (activeServerRunning()) start.setOnClickListener(v -> {
                actionButton(start, "/api/server/stop", "{}", "Stopping server...");
                releaseServerWakeLock();
            });
            else start.setOnClickListener(v -> {
                acquireServerWakeLock();
                actionButton(start, "/api/server/start", "{}", "Starting server...");
            });
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
            statsA.addView(statCard("CPU", cpuText(), TEXT), weightedButtonParams());
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
        logs = text("No logs yet.", 12, TEXT, false);
        logs.setTypeface(Typeface.MONOSPACE);
        logs.setBackground(makeBg(0xff050505, STROKE, 10));
        logs.setPadding(dp(10), dp(10), dp(10), dp(10));
        activity.addView(logs, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(120)));
        refreshLogs();
    }

    private boolean useRedesignedDashboard() {
        return true;
    }

    private LinearLayout dashboardServerCard() {
        LinearLayout live = glassCard();
        live.setPadding(dp(16), dp(16), dp(16), dp(16));

        LinearLayout titleRow = row();
        titleRow.setGravity(Gravity.TOP);
        live.addView(titleRow, matchWrap());
        titleRow.addView(loaderIconView(activeLoader()), new LinearLayout.LayoutParams(dp(64), dp(58)));

        LinearLayout titleTexts = column();
        LinearLayout.LayoutParams titleParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        titleParams.setMargins(dp(14), 0, 0, 0);
        titleRow.addView(titleTexts, titleParams);
        LinearLayout nameRow = row();
        nameRow.setGravity(Gravity.CENTER_VERTICAL);
        titleTexts.addView(nameRow, matchWrap());
        TextView name = text(activeServerName(), 24, TEXT, true);
        name.setSingleLine(false);
        name.setMaxLines(2);
        nameRow.addView(name, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        nameRow.addView(statusPill(dashboardStatusLabel(), dashboardStatusColor()));
        titleTexts.addView(text("IP: " + joinAddressText(), 15, MUTED, false));
        addDashboardPlayItLines(titleTexts);
        titleTexts.addView(text("Minecraft " + versionText() + " " + activeLoader(), 14, MUTED, false));

        LinearLayout controls = row();
        live.addView(controls, matchWrapMargin(0, dp(14), 0, dp(8)));
        Button start = iconActionButton(activeServerRunning() ? R.drawable.ic_mm_stop : R.drawable.ic_mm_play,
            activeServerRunning() ? DANGER_RED : ACCENT, activeServerRunning() ? DANGER_RED : GREEN, 0xffffffff,
            activeServerRunning() ? "Stop server" : "Start server");
        start.setContentDescription(activeServerRunning() ? "Stop server" : "Start server");
        if (activeServerRunning()) start.setOnClickListener(v -> {
            confirmStop(start);
        });
        else start.setOnClickListener(v -> {
            startServerFromUi(start);
        });
        controls.addView(start, weightedButtonParams());
        Button restart = iconActionButton(R.drawable.ic_mm_restart, PANEL_ALT, STROKE, TEXT, "Restart server");
        restart.setContentDescription("Restart server");
        restart.setOnClickListener(v -> confirmRestart(restart));
        controls.addView(restart, weightedButtonParams());
        Button backup = iconActionButton(R.drawable.ic_mm_save, PANEL_ALT, STROKE, TEXT, "Create backup");
        backup.setContentDescription("Create backup");
        backup.setOnClickListener(v -> actionButton(backup, "/api/backups/create", "{}", "Creating backup..."));
        controls.addView(backup, weightedButtonParams());
        Button details = iconActionButton(R.drawable.ic_mm_info, PANEL_ALT, STROKE, TEXT, "Server details");
        details.setContentDescription("Server details");
        details.setOnClickListener(v -> setPage("servers"));
        controls.addView(details, weightedButtonParams());

        live.addView(metricPair(
            metricPanel("TPS", tpsText() + " / 20", GREEN, tpsHistory, true, () -> showMetricDialog("TPS", tpsText(), GREEN, tpsHistory)),
            metricPanel("Players", playerCountText() + " / " + maxPlayersText(), ACCENT, playerHistory, true, this::showPlayersDialog)
        ));
        live.addView(metricPair(
            metricPanel("CPU", cpuText(), TEXT, cpuHistory, true, () -> showMetricDialog("CPU", cpuText(), TEXT, cpuHistory)),
            metricPanel("Memory", ramPercentText(), PURPLE, memoryHistory, true, () -> showMetricDialog("Memory", ramPercentText(), PURPLE, memoryHistory))
        ));
        live.addView(metricPair(
            metricPanel("Uptime", uptimeText(), MUTED, null, false, null),
            metricPanel("Disk", serverDiskText(), WARNING, null, false, null)
        ));
        addPlayItCard(live, playItStatus(), true);
        return live;
    }

    private void addDashboardPlayItLines(LinearLayout parent) {
        JSONObject playit = playItStatus();
        if (!hasPlayItOutput(playit)) return;
        String address = playit.optString("address", "");
        String claim = playit.optString("claimUrl", "");
        String error = playit.optString("error", "");
        if (!address.isEmpty()) {
            parent.addView(text("PlayIt: " + address, 15, GREEN, true));
        } else if (!error.isEmpty()) {
            parent.addView(text("PlayIt: " + error, 14, DANGER_RED, true));
        } else if (!claim.isEmpty()) {
            parent.addView(text("PlayIt setup: claim link ready", 15, GREEN, true));
        }
    }

    private void addServerPlayItLines(LinearLayout parent, JSONObject server) {
        JSONObject playit = playItStatus(server);
        if (!hasPlayItOutput(playit)) return;
        String address = playit.optString("address", "");
        String claim = playit.optString("claimUrl", "");
        String error = playit.optString("error", "");
        if (!address.isEmpty()) {
            parent.addView(text("PlayIt: " + address, 15, GREEN, true));
        } else if (!error.isEmpty()) {
            parent.addView(text("PlayIt: " + error, 14, DANGER_RED, true));
        } else if (!claim.isEmpty()) {
            parent.addView(text("PlayIt setup pending", 15, GREEN, true));
        }
    }

    private void addPlayItCard(LinearLayout parent, JSONObject playit, boolean compact) {
        if (!hasPlayItOutput(playit)) return;
        LinearLayout card = sectionCard("PlayIt.gg", "", null);
        card.setPadding(dp(14), dp(14), dp(14), dp(14));
        parent.addView(card, matchWrapMargin(0, compact ? dp(10) : 0, 0, dp(12)));

        String address = playit.optString("address", "");
        String claim = playit.optString("claimUrl", "");
        String error = playit.optString("error", "");
        if (!address.isEmpty()) {
            card.addView(summaryLine("Public Address", address));
            LinearLayout actions = row();
            card.addView(actions, matchWrapMargin(0, dp(8), 0, 0));
            Button copy = secondaryButton("Copy Address");
            copy.setOnClickListener(v -> copyToClipboard("PlayIt address", address));
            actions.addView(copy, weightedButtonParams());
        } else if (!error.isEmpty()) {
            card.addView(text(error, 13, DANGER_RED, true));
            card.addView(text("Set the PlayIt agent/tunnel to IPv4 only in PlayIt first. If the plugin still fails, start the fallback agent here.", 12, MUTED, false));
            if (!claim.isEmpty()) {
                card.addView(summaryLine("Claim Link", claim));
                LinearLayout actions = row();
                card.addView(actions, matchWrapMargin(0, dp(8), 0, 0));
                Button open = primaryButton("Open Claim");
                open.setOnClickListener(v -> openUrl(claim));
                actions.addView(open, weightedButtonParams());
                Button copy = secondaryButton("Copy");
                copy.setOnClickListener(v -> copyToClipboard("PlayIt claim", claim));
                actions.addView(copy, weightedButtonParams());
            }
            LinearLayout fallbackActions = row();
            card.addView(fallbackActions, matchWrapMargin(0, dp(8), 0, 0));
            if (playit.optBoolean("running", false)) {
                Button stopFallback = secondaryButton("Stop Fallback");
                stopFallback.setOnClickListener(v -> actionButton(stopFallback, "/api/playit/agent/stop", "{}", "Stopping PlayIt fallback..."));
                fallbackActions.addView(stopFallback, weightedButtonParams());
            } else {
                Button startFallback = secondaryButton("Start Fallback");
                startFallback.setOnClickListener(v -> actionButton(startFallback, "/api/playit/agent/start", "{}", "Starting PlayIt fallback..."));
                fallbackActions.addView(startFallback, weightedButtonParams());
            }
        } else if (!claim.isEmpty()) {
            card.addView(text("PlayIt found a claim link. Open it once, log in, and add the agent. After PlayIt creates the tunnel, MineMux will show the public address here.", 13, MUTED, false));
            card.addView(summaryLine("Claim Link", claim));
            LinearLayout actions = row();
            card.addView(actions, matchWrapMargin(0, dp(8), 0, 0));
            Button open = primaryButton("Open Claim");
            open.setOnClickListener(v -> openUrl(claim));
            actions.addView(open, weightedButtonParams());
            Button copy = secondaryButton("Copy");
            copy.setOnClickListener(v -> copyToClipboard("PlayIt claim", claim));
            actions.addView(copy, weightedButtonParams());
        }
    }

    private LinearLayout metricPair(View left, View right) {
        LinearLayout pair = row();
        pair.addView(left, dashboardMetricParams());
        pair.addView(right, dashboardMetricParams());
        return pair;
    }

    private LinearLayout metricPanel(String label, String value, int color, @Nullable ArrayList<Float> history, boolean graph) {
        return metricPanel(label, value, color, history, graph, null);
    }

    private LinearLayout metricPanel(String label, String value, int color, @Nullable ArrayList<Float> history, boolean graph, @Nullable Runnable action) {
        LinearLayout panel = card();
        panel.setGravity(Gravity.CENTER_VERTICAL);
        panel.setPadding(dp(12), dp(10), dp(12), dp(10));
        panel.addView(text(label, 12, MUTED, true));
        TextView valueView = text(value, 20, color, true);
        panel.addView(valueView);
        dashboardMetricViews.put(label, valueView);
        if (graph && history != null) {
            LinearLayout graphView = metricGraph(history, color, 44);
            dashboardGraphViews.put(label, graphView);
            panel.addView(graphView, matchWrapMargin(0, dp(6), 0, 0));
        }
        if (action != null) panel.setOnClickListener(v -> action.run());
        return panel;
    }

    private LinearLayout metricGraph(ArrayList<Float> history, int color, int heightDp) {
        LinearLayout bars = row();
        bars.setGravity(Gravity.BOTTOM);
        bars.setMinimumHeight(dp(heightDp));
        populateMetricGraph(bars, history, color, heightDp);
        return bars;
    }

    private void populateMetricGraph(LinearLayout bars, ArrayList<Float> history, int color, int heightDp) {
        bars.removeAllViews();
        int start = Math.max(0, history.size() - 18);
        if (history.size() <= start) {
            for (int i = 0; i < 10; i++) bars.addView(tinyBar(color, 12 + i * 2));
        } else {
            for (int i = start; i < history.size(); i++) {
                int height = clamp(Math.round(history.get(i)), 8, heightDp);
                bars.addView(tinyBar(color, height));
            }
        }
    }

    private View tinyBar(int color, int heightDp) {
        TextView bar = new TextView(this);
        bar.setBackground(makeBg(color, color, 4));
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(heightDp), 1);
        params.setMargins(dp(1), 0, dp(1), 0);
        bar.setLayoutParams(params);
        bar.setAlpha(0.42f);
        return bar;
    }

    private void showMetricDialog(String title, String value, int color, ArrayList<Float> history) {
        LinearLayout body = column();
        body.setPadding(dp(16), dp(12), dp(16), dp(8));
        body.addView(text(value, 28, color, true));
        body.addView(metricGraph(history, color, 160), matchWrapMargin(0, dp(12), 0, 0));
        new AlertDialog.Builder(this).setTitle(title).setView(body).setPositiveButton("Close", null).show();
    }

    private void showPlayersDialog() {
        LinearLayout body = column();
        body.setPadding(dp(12), dp(8), dp(12), dp(8));
        body.addView(text("Loading players...", 14, MUTED, false));
        AlertDialog dialog = new AlertDialog.Builder(this).setTitle("Players").setView(body).setPositiveButton("Close", null).show();
        getJson("/api/server/players", json -> renderPlayersDialog(body, json), error -> {
            body.removeAllViews();
            body.addView(text(error, 14, ERROR, false));
        });
    }

    private void renderPlayersDialog(LinearLayout body, String json) {
        body.removeAllViews();
        try {
            JSONArray players = new JSONObject(json).optJSONArray("players");
            if (players == null || players.length() == 0) {
                body.addView(text("No players online.", 14, MUTED, false));
                return;
            }
            for (int i = 0; i < players.length(); i++) {
                String player = players.getString(i);
                LinearLayout row = column();
                row.setPadding(0, dp(8), 0, dp(8));
                row.addView(text(player, 17, TEXT, true));
                LinearLayout actions = row();
                row.addView(actions, matchWrapMargin(0, dp(6), 0, 0));
                Button kick = secondaryButton("Kick");
                kick.setOnClickListener(v -> sendPlayerCommand("kick " + player));
                actions.addView(kick, weightedButtonParams());
                Button survival = secondaryButton("Survival");
                survival.setOnClickListener(v -> sendPlayerCommand("gamemode survival " + player));
                actions.addView(survival, weightedButtonParams());
                Button creative = secondaryButton("Creative");
                creative.setOnClickListener(v -> sendPlayerCommand("gamemode creative " + player));
                actions.addView(creative, weightedButtonParams());
                Button ban = dangerButton("Ban");
                ban.setOnClickListener(v -> sendPlayerCommand("ban " + player));
                actions.addView(ban, weightedButtonParams());
                body.addView(row, matchWrap());
            }
        } catch (Exception e) {
            body.addView(text(e.getMessage(), 14, ERROR, false));
        }
    }

    private void sendPlayerCommand(String command) {
        postJson("/api/server/command", "{\"command\":\"" + escapeJson(command) + "\"}", "Sending command...", () -> refreshStatus());
    }

    private void buildRecentActivitySection() {
        LinearLayout activity = glassCard();
        activity.setPadding(dp(16), dp(16), dp(16), dp(16));
        content.addView(activity, matchWrapMargin(0, 0, 0, dp(18)));
        LinearLayout activityHeader = row();
        activityHeader.setGravity(Gravity.CENTER_VERTICAL);
        activity.addView(activityHeader, matchWrap());
        activityHeader.addView(text("Recent Activity", 22, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        TextView viewBackups = text("Backups >", 14, ACCENT, true);
        viewBackups.setOnClickListener(v -> setPage("backups"));
        activityHeader.addView(viewBackups);

        LinearLayout events = column();
        activity.addView(events, matchWrapMargin(0, dp(8), 0, dp(8)));
        loadRecentActivity(events);

        logs = text("No logs yet.", 12, TEXT, false);
        logs.setTypeface(Typeface.MONOSPACE);
        logs.setBackground(makeBg(BG, STROKE, 10));
        logs.setPadding(dp(10), dp(10), dp(10), dp(10));
        activity.addView(logs, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(132)));

        LinearLayout commandRow = row();
        commandRow.setGravity(Gravity.CENTER_VERTICAL);
        activity.addView(commandRow, matchWrapMargin(0, dp(10), 0, 0));
        TextView keyboardSpacer = new TextView(this);
        activity.addView(keyboardSpacer, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0));
        EditText command = input("say Hello from MineMux");
        command.setOnFocusChangeListener((view, focused) -> {
            dashboardCommandFocused = focused;
            ViewGroup.LayoutParams params = keyboardSpacer.getLayoutParams();
            params.height = focused ? dp(320) : 0;
            keyboardSpacer.setLayoutParams(params);
            if (focused && scrollView != null) mainHandler.postDelayed(() -> scrollView.fullScroll(View.FOCUS_DOWN), 250);
        });
        commandRow.addView(command, new LinearLayout.LayoutParams(0, dp(48), 1));
        Button send = primaryButton(">");
        send.setContentDescription("Send command");
        send.setOnClickListener(v -> {
            String value = command.getText().toString().trim();
            if (value.isEmpty()) {
                setNotice("Type a server command first.", true);
                return;
            }
            command.setText("");
            actionButton(send, "/api/server/command", "{\"command\":\"" + escapeJson(value) + "\"}", "Sending command...");
        });
        commandRow.addView(send, fixedButtonParams(58));
        refreshLogs();
    }

    private void loadRecentActivity(LinearLayout events) {
        events.removeAllViews();
        getJson("/api/server/logs", body -> {
            try {
                JSONArray lines = new JSONObject(body).optJSONArray("lines");
                ArrayList<String> rows = new ArrayList<>();
                if (lines != null) {
                    for (int i = lines.length() - 1; i >= 0 && rows.size() < 3; i--) {
                        String line = lines.optString(i, "");
                        String lower = line.toLowerCase();
                        if (lower.contains("joined the game")) rows.add("Player joined|" + line);
                        else if (lower.contains("left the game")) rows.add("Player left|" + line);
                        else if (lower.contains("backup")) rows.add("Backup activity|" + line);
                        else if (lower.contains("server setup complete")) rows.add("Setup complete|" + line);
                        else if (lower.contains("done") && lower.contains("for help")) rows.add("Server started|" + line);
                    }
                }
                if (rows.isEmpty()) {
                    events.addView(activityRow("Server activity", activeServerInstalled() ? "No player events yet" : "No server configured", MUTED));
                    return;
                }
                for (String rowText : rows) {
                    String[] parts = rowText.split("\\|", 2);
                    events.addView(activityRow(parts[0], parts.length > 1 ? parts[1] : "", ACCENT));
                }
            } catch (Exception e) {
                events.addView(activityRow("Activity unavailable", e.getMessage(), WARNING));
            }
        }, error -> events.addView(activityRow("Activity unavailable", error, WARNING)));
    }

    private void buildSetupPage() {
        LinearLayout panel = card();
        panel.setPadding(dp(14), dp(14), dp(14), dp(14));
        content.addView(panel, matchWrapMargin(0, dp(6), 0, dp(10)));

        panel.addView(text("Step " + (setupStep + 1) + " of " + setupStepCount(), 12, ACCENT, true));
        if (setupStep == 0) buildSetupNameStep(panel);
        else if (setupStep == 1) buildSetupVersionStep(panel);
        else if (setupStep == 2) buildSetupResourcesStep(panel);
        else if (setupStep == 3) buildSetupSeedStep(panel);
        else if (setupStep == 4) buildSetupJarStep(panel);
        else if (setupStep == 5) buildSetupModsStep(panel);
        else if (setupStep == 6) buildSetupCrossplayStep(panel);
        else if (setupStep == 7) buildSetupPlayitStep(panel);
        else buildSetupConfirmStep(panel);

        if (setupStep == setupStepCount() - 1) buildSetupProgressPanel();
    }

    private int setupStepCount() {
        return 9;
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
        panel.addView(text("Choose the runtime MineMux should install. Paper is best for plugins, Vanilla is simplest, and Fabric, Quilt, Forge, and NeoForge are for modded servers.", 13, MUTED, false));
        Spinner versionSpinner = spinner(new String[]{wizardVersion, "latest-compatible", "1.21.8", "1.21.7", "1.21.6", "1.21.5", "1.21.4", "1.20.6", "1.20.4"});
        addField(panel, "Minecraft version", versionSpinner);
        Spinner loaderSpinner = spinner(new String[]{"paper - Paper Plugins", "vanilla - Vanilla", "fabric - Fabric Mods", "quilt - Quilt Mods", "forge - Forge Mods", "neoforge - NeoForge Mods"});
        addField(panel, "Runtime", loaderSpinner);
        loadSetupOptions(versionSpinner, loaderSpinner);
        setupNav(panel, () -> {
            setupStep = 0;
            setPage("setup");
        }, () -> {
            String loaderChoice = loaderSpinner.getSelectedItem().toString();
            wizardVersion = versionSpinner.getSelectedItem().toString();
            wizardLoader = loaderId(loaderChoice);
            if (!"paper".equals(wizardLoader)) wizardPlayitEnabled = false;
            setupStep = 2;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupResourcesStep(LinearLayout panel) {
        panel.addView(text("Tune phone resources", 19, TEXT, true));
        panel.addView(text("These defaults are conservative for a phone. Increase memory only if the phone has enough RAM free.", 13, MUTED, false));
        int[] ramOptions = ramSliderOptions();
        addDiscreteSlider(panel, "Memory", ramOptions, wizardMemory, value -> wizardMemory = value, "Java heap");
        addRangeSlider(panel, "Max players", 1, 100, wizardPlayers, value -> wizardPlayers = value, "Player slots");
        addRangeSlider(panel, "View distance", 2, 32, wizardViewDistance, value -> wizardViewDistance = value, "Chunks sent to players");
        addRangeSlider(panel, "Simulation distance", 2, 32, wizardSimulationDistance, value -> wizardSimulationDistance = value, "Chunks that stay active");
        setupNav(panel, () -> {
            setupStep = 1;
            setPage("setup");
        }, () -> {
            setupStep = 3;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupSeedStep(LinearLayout panel) {
        panel.addView(text("World seed", 19, TEXT, true));
        panel.addView(text("Leave this empty for a random world. Existing worlds ignore this after first generation.", 13, MUTED, false));
        EditText seed = input(wizardSeed);
        addField(panel, "Seed", seed);
        setupNav(panel, () -> {
            wizardSeed = seed.getText().toString().trim();
            setupStep = 2;
            setPage("setup");
        }, () -> {
            wizardSeed = seed.getText().toString().trim();
            setupStep = 4;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupJarStep(LinearLayout panel) {
        panel.addView(text("Server JAR source", 19, TEXT, true));
        panel.addView(text("MineMux can download the selected runtime, or you can upload a custom server.jar.", 13, MUTED, false));
        CheckBox custom = new CheckBox(this);
        custom.setText("Use uploaded custom server.jar");
        custom.setTextColor(TEXT);
        custom.setTextSize(15);
        custom.setChecked("custom".equals(wizardJarMode));
        panel.addView(custom, matchWrapMargin(0, dp(8), 0, dp(6)));
        TextView selected = text(wizardCustomJarName.isEmpty() ? "No custom JAR selected" : wizardCustomJarName, 13, MUTED, false);
        panel.addView(selected, matchWrapMargin(0, 0, 0, dp(8)));
        Button upload = secondaryButton("Upload .jar");
        upload.setOnClickListener(v -> pickSetupJar());
        panel.addView(upload, fullWidthButtonParams());
        setupNav(panel, () -> {
            wizardJarMode = custom.isChecked() ? "custom" : "download";
            setupStep = 3;
            setPage("setup");
        }, () -> {
            wizardJarMode = custom.isChecked() ? "custom" : "download";
            if ("custom".equals(wizardJarMode) && wizardCustomJarId.isEmpty()) {
                setNotice("Upload a custom .jar first, or switch back to automatic download.", true);
                return;
            }
            setupStep = 5;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupModsStep(LinearLayout panel) {
        panel.addView(text("Recommended add-ons", 19, TEXT, true));
        panel.addView(text("MineMux installs compatible Modrinth projects after the server runtime is ready. Incompatible picks are skipped and logged.", 13, MUTED, false));
        for (String[] mod : recommendedSetupMods()) {
            CheckBox box = new CheckBox(this);
            box.setText(mod[0] + " - " + mod[2]);
            box.setTextColor(TEXT);
            box.setTextSize(14);
            box.setChecked(wizardPendingMods.contains(mod[1]));
            box.setOnCheckedChangeListener((button, checked) -> {
                if (checked && !wizardPendingMods.contains(mod[1])) wizardPendingMods.add(mod[1]);
                if (!checked) wizardPendingMods.remove(mod[1]);
            });
            panel.addView(box, matchWrapMargin(0, dp(4), 0, dp(4)));
        }
        setupNav(panel, () -> {
            setupStep = 4;
            setPage("setup");
        }, () -> {
            setupStep = 6;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupCrossplayStep(LinearLayout panel) {
        panel.addView(text("Crossplay", 19, TEXT, true));
        panel.addView(text("Install Geyser for Bedrock clients. Floodgate allows Bedrock players to join without owning Java, where supported.", 13, MUTED, false));
        CheckBox geyser = new CheckBox(this);
        geyser.setText("Enable Bedrock crossplay with Geyser");
        geyser.setTextColor(TEXT);
        geyser.setTextSize(15);
        geyser.setChecked(wizardCrossplayEnabled);
        panel.addView(geyser, matchWrapMargin(0, dp(8), 0, dp(4)));
        CheckBox floodgate = new CheckBox(this);
        floodgate.setText("Install Floodgate");
        floodgate.setTextColor(TEXT);
        floodgate.setTextSize(15);
        floodgate.setChecked(wizardFloodgateEnabled);
        panel.addView(floodgate, matchWrapMargin(0, 0, 0, dp(4)));
        panel.addView(text("Bedrock uses UDP port 19132. If you use PlayIt with Bedrock, the Paper plugin alone is not enough for UDP tunnels.", 12, MUTED, false));
        setupNav(panel, () -> {
            wizardCrossplayEnabled = geyser.isChecked();
            wizardFloodgateEnabled = floodgate.isChecked();
            setupStep = 5;
            setPage("setup");
        }, () -> {
            wizardCrossplayEnabled = geyser.isChecked();
            wizardFloodgateEnabled = floodgate.isChecked();
            setupStep = 7;
            setPage("setup");
        }, "Next");
    }

    private void buildSetupPlayitStep(LinearLayout panel) {
        panel.addView(text("PlayIt.gg", 19, TEXT, true));
        if (!"paper".equals(wizardLoader)) {
            wizardPlayitEnabled = false;
            panel.addView(text("PlayIt plugin setup is currently offered for Paper/Spigot-style servers. Other runtimes can still use the standalone PlayIt agent later as fallback.", 13, MUTED, false));
            setupNav(panel, () -> {
                setupStep = 6;
                setPage("setup");
            }, () -> {
                setupStep = 8;
                setPage("setup");
            }, "Review");
            return;
        }
        panel.addView(text("MineMux installs the official PlayIt plugin jar. If PlayIt throws IPv6/control-channel errors on Android, set the PlayIt agent/tunnel to IPv4 only in PlayIt. The standalone agent remains available as fallback.", 13, MUTED, false));
        CheckBox playit = new CheckBox(this);
        playit.setText("Install PlayIt plugin");
        playit.setTextColor(TEXT);
        playit.setTextSize(15);
        playit.setChecked(wizardPlayitEnabled);
        panel.addView(playit, matchWrapMargin(0, dp(8), 0, dp(4)));
        setupNav(panel, () -> {
            wizardPlayitEnabled = playit.isChecked();
            setupStep = 6;
            setPage("setup");
        }, () -> {
            wizardPlayitEnabled = playit.isChecked();
            setupStep = 8;
            setPage("setup");
        }, "Review");
    }

    private void buildSetupConfirmStep(LinearLayout panel) {
        panel.addView(text("Review and create", 19, TEXT, true));
        panel.addView(summaryLine("Server", wizardServerName + " (" + wizardServerId + ")"));
        panel.addView(summaryLine("Runtime", wizardLoader));
        panel.addView(summaryLine("Minecraft", wizardVersion));
        panel.addView(summaryLine("Resources", wizardMemory + " MB, " + wizardPlayers + " players"));
        panel.addView(summaryLine("Distances", wizardViewDistance + " view, " + wizardSimulationDistance + " simulation"));
        panel.addView(summaryLine("Seed", wizardSeed.isEmpty() ? "Random" : wizardSeed));
        panel.addView(summaryLine("JAR", "custom".equals(wizardJarMode) ? wizardCustomJarName : "Automatic download"));
        panel.addView(summaryLine("Add-ons", wizardPendingMods.isEmpty() ? "None" : String.valueOf(wizardPendingMods.size())));
        panel.addView(summaryLine("Crossplay", wizardCrossplayEnabled ? "Geyser" + (wizardFloodgateEnabled ? " + Floodgate" : "") : "Off"));
        panel.addView(summaryLine("PlayIt.gg", wizardPlayitEnabled && "paper".equals(wizardLoader) ? "Install plugin" : "Off"));
        CheckBox eula = new CheckBox(this);
        eula.setText("I accept the Minecraft EULA");
        eula.setTextColor(TEXT);
        eula.setTextSize(15);
        eula.setChecked(wizardEula || eulaAlreadyAccepted());
        panel.addView(eula, matchWrapMargin(0, dp(8), 0, dp(6)));
        setupNav(panel, () -> {
            wizardEula = eula.isChecked();
            setupStep = 7;
            setPage("setup");
        }, () -> {
            wizardEula = eula.isChecked();
            if (!wizardEula && !eulaAlreadyAccepted()) {
                setNotice("Accept the Minecraft EULA first.", true);
                return;
            }
            String body = "{"
                + "\"serverId\":\"" + escapeJson(wizardServerId) + "\","
                + "\"name\":\"" + escapeJson(wizardServerName) + "\","
                + "\"loader\":\"" + escapeJson(wizardLoader) + "\","
                + "\"minecraftVersion\":\"" + escapeJson(wizardVersion) + "\","
                + "\"seed\":\"" + escapeJson(wizardSeed) + "\","
                + "\"jarMode\":\"" + escapeJson(wizardJarMode) + "\","
                + "\"customJarId\":\"" + escapeJson(wizardCustomJarId) + "\","
                + "\"memoryMb\":" + wizardMemory + ","
                + "\"maxPlayers\":" + wizardPlayers + ","
                + "\"viewDistance\":" + wizardViewDistance + ","
                + "\"simulationDistance\":" + wizardSimulationDistance + ","
                + "\"pendingModrinthProjects\":" + jsonStringArray(wizardPendingMods) + ","
                + "\"crossplay\":{\"enabled\":" + wizardCrossplayEnabled
                + ",\"installGeyser\":" + wizardCrossplayEnabled
                + ",\"installFloodgate\":" + wizardFloodgateEnabled
                + ",\"floodgate\":" + wizardFloodgateEnabled
                + ",\"bedrockUdp\":19132},"
                + "\"playit\":{\"enabled\":" + (wizardPlayitEnabled && "paper".equals(wizardLoader)) + "},"
                + "\"acceptEula\":" + (wizardEula || eulaAlreadyAccepted())
                + "}";
            beginSetupProgress();
            beginOperation("Setting up server", "Preparing DNS and Java...");
            showSetupProgressPanel();
            refreshLogs();
            startSetupRequest(body);
        }, "Create Server");
    }

    private void buildSetupProgressPanel() {
        setupProgressPanel = card();
        setupProgressPanel.setPadding(dp(14), dp(12), dp(14), dp(12));
        setupProgressPanel.setVisibility(operationRunning || setupProgressRunning ? View.VISIBLE : View.GONE);
        content.addView(setupProgressPanel, matchWrapMargin(0, 0, 0, dp(12)));

        setupProgressTitle = text("Install Output", 16, TEXT, true);
        setupProgressDetail = text(setupProgressRunning ? setupProgressText() : "Waiting to start...", 13, MUTED, false);
        setupProgressBar = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        setupProgressBar.setMax(100);
        setupProgressBar.setIndeterminate(false);
        setupProgressBar.setProgress(setupProgressPercent());
        setupLogOutput = text("Waiting for setup output...", 12, TEXT, false);
        setupLogOutput.setTypeface(Typeface.MONOSPACE);
        setupLogOutput.setBackground(makeBg(BG, STROKE, 10));
        setupLogOutput.setPadding(dp(10), dp(10), dp(10), dp(10));

        setupProgressPanel.addView(setupProgressTitle);
        setupProgressPanel.addView(setupProgressDetail, matchWrapMargin(0, dp(4), 0, dp(8)));
        setupProgressPanel.addView(setupProgressBar, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(6)));
        setupProgressPanel.addView(setupLogOutput, matchWrapMargin(0, dp(10), 0, 0));
    }

    private void showSetupProgressPanel() {
        if (setupProgressPanel != null) setupProgressPanel.setVisibility(View.VISIBLE);
        if (setupProgressTitle != null) setupProgressTitle.setText("Setting up server");
        if (setupProgressDetail != null) setupProgressDetail.setText(setupProgressText());
        if (setupProgressBar != null) {
            setupProgressBar.setIndeterminate(false);
            setupProgressBar.setProgress(setupProgressPercent());
        }
    }

    private void startSetupRequest(String body) {
        setNotice("Setting up server...", false);
        new Thread(() -> {
            try {
                request("/api/server/setup", "POST", body);
                mainHandler.post(() -> {
                    setupProgressRunning = false;
                    if (setupProgressBar != null) setupProgressBar.setProgress(100);
                    if (setupProgressDetail != null) setupProgressDetail.setText("Setup complete. Opening dashboard...");
                    setNotice("Server setup complete.", false);
                    refreshStatus();
                    mainHandler.postDelayed(() -> {
                        endOperation();
                        setPage("dashboard");
                    }, 500);
                });
            } catch (Exception e) {
                mainHandler.post(() -> {
                    setupProgressRunning = false;
                    operationRunning = false;
                    if (setupProgressDetail != null) setupProgressDetail.setText("Setup failed: " + e.getMessage());
                    setControllerActionsEnabled(controllerOnline);
                    setNotice(e.getMessage(), true);
                });
            }
        }).start();
    }

    private void loadSetupOptions(Spinner versionSpinner, Spinner loaderSpinner) {
        getJson("/api/server/options", body -> {
            try {
                JSONObject root = new JSONObject(body);
                JSONArray versions = root.optJSONArray("minecraftVersions");
                if (versions != null && versions.length() > 0) {
                    List<String> items = new ArrayList<>();
                    items.add("latest-compatible");
                    for (int i = 0; i < versions.length(); i++) {
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

    private interface IntValueConsumer {
        void accept(int value);
    }

    private void addRangeSlider(LinearLayout parent, String label, int min, int max, int value, IntValueConsumer consumer, String subtitle) {
        TextView readout = text(label + ": " + value + "  " + subtitle, 14, TEXT, true);
        parent.addView(readout, matchWrapMargin(0, dp(10), 0, 0));
        SeekBar slider = new SeekBar(this);
        slider.setMax(max - min);
        slider.setProgress(clamp(value, min, max) - min);
        slider.setOnSeekBarChangeListener(new SeekBar.OnSeekBarChangeListener() {
            @Override public void onProgressChanged(SeekBar seekBar, int progress, boolean fromUser) {
                int next = min + progress;
                consumer.accept(next);
                readout.setText(label + ": " + next + "  " + subtitle);
            }
            @Override public void onStartTrackingTouch(SeekBar seekBar) {}
            @Override public void onStopTrackingTouch(SeekBar seekBar) {}
        });
        parent.addView(slider, matchWrapMargin(0, 0, 0, dp(4)));
    }

    private void addDiscreteSlider(LinearLayout parent, String label, int[] values, int value, IntValueConsumer consumer, String subtitle) {
        int index = closestIndex(values, value);
        consumer.accept(values[index]);
        TextView readout = text(label + ": " + values[index] + " MB  " + subtitle, 14, TEXT, true);
        parent.addView(readout, matchWrapMargin(0, dp(10), 0, 0));
        SeekBar slider = new SeekBar(this);
        slider.setMax(Math.max(0, values.length - 1));
        slider.setProgress(index);
        slider.setOnSeekBarChangeListener(new SeekBar.OnSeekBarChangeListener() {
            @Override public void onProgressChanged(SeekBar seekBar, int progress, boolean fromUser) {
                int next = values[clamp(progress, 0, values.length - 1)];
                consumer.accept(next);
                readout.setText(label + ": " + next + " MB  " + subtitle);
            }
            @Override public void onStartTrackingTouch(SeekBar seekBar) {}
            @Override public void onStopTrackingTouch(SeekBar seekBar) {}
        });
        parent.addView(slider, matchWrapMargin(0, 0, 0, dp(4)));
    }

    private int[] ramSliderOptions() {
        int max = Math.max(1024, totalMemoryMb() * 80 / 100);
        int[] base = new int[]{1024, 1536, 2048, 2560, 3072, 4096, 5120, 6144, 8192, 10240, 12288, 16384};
        ArrayList<Integer> values = new ArrayList<>();
        for (int option : base) {
            if (option <= max) values.add(option);
        }
        if (values.isEmpty()) values.add(1024);
        int[] out = new int[values.size()];
        for (int i = 0; i < values.size(); i++) out[i] = values.get(i);
        return out;
    }

    private int totalMemoryMb() {
        try {
            JSONObject system = latestStatus == null ? null : latestStatus.optJSONObject("system");
            if (system == null) return 8192;
            long total = system.getJSONObject("memory").optLong("totalBytes", 0);
            return total <= 0 ? 8192 : (int) (total / 1024 / 1024);
        } catch (Exception e) {
            return 8192;
        }
    }

    private int closestIndex(int[] values, int value) {
        int best = 0;
        int distance = Integer.MAX_VALUE;
        for (int i = 0; i < values.length; i++) {
            int next = Math.abs(values[i] - value);
            if (next < distance) {
                distance = next;
                best = i;
            }
        }
        return best;
    }

    private String[][] recommendedSetupMods() {
        return new String[][]{
            {"spark", "spark", "performance profiler"},
            {"Chunky", "chunky", "pre-generate chunks"},
            {"FerriteCore", "ferrite-core", "memory optimization"},
            {"ModernFix", "modernfix", "performance fixes"},
            {"Lithium", "lithium", "game logic optimization"}
        };
    }

    private String jsonStringArray(ArrayList<String> values) {
        StringBuilder out = new StringBuilder("[");
        for (int i = 0; i < values.size(); i++) {
            if (i > 0) out.append(',');
            out.append('"').append(escapeJson(values.get(i))).append('"');
        }
        out.append(']');
        return out.toString();
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
        loadServersListOnly(list);
    }

    private void loadServersListOnly(LinearLayout list) {
        list.removeAllViews();
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
                    list.addView(create, fullWidthButtonParams());
                    return;
                }
                for (int i = 0; i < servers.length(); i++) {
                    JSONObject server = servers.getJSONObject(i);
                    list.addView(serverListRow(server));
                }
                TextView count = text(servers.length() + (servers.length() == 1 ? " server" : " servers"), 14, MUTED, false);
                count.setGravity(Gravity.CENTER);
                list.addView(count, matchWrapMargin(0, dp(4), 0, dp(12)));
                Button create = primaryButton("Create New Server");
                create.setOnClickListener(v -> {
                    setupStep = 0;
                    wizardServerId = "server-" + System.currentTimeMillis() / 1000;
                    wizardServerName = "New Server";
                    setPage("setup");
                });
                list.addView(create, fullWidthButtonParams());
            } catch (Exception e) {
                list.addView(emptyState(e.getMessage()));
            }
        }, error -> list.addView(emptyState(error)));
    }

    private View serverListRow(JSONObject server) {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(16), dp(14), dp(12), dp(14));
        row.setBackground(makeBg(PANEL, server.optBoolean("active") ? GREEN : STROKE, 18));
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(12)));

        JSONObject profile = server.optJSONObject("profile");
        LinearLayout copy = column();
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        copyParams.setMargins(0, 0, dp(8), 0);
        row.addView(copy, copyParams);
        copy.addView(text(server.optString("name", server.optString("id", "Server")), 20, TEXT, true));
        String mc = profile == null ? "-" : profile.optString("minecraftVersion", "-");
        String loader = profile == null ? "-" : profile.optString("loader", "-");
        copy.addView(text("Minecraft " + mc, 14, MUTED, false));
        copy.addView(text(loaderLabel(profile), 14, MUTED, false));
        copy.addView(text("Last run: " + relativeTime(server.optString("lastRunAt", "")), 13, TERTIARY, false));
        row.addView(statusPill(serverStatusLabel(server), serverStatusColor(server)));
        row.addView(text(">", 22, MUTED, true));
        row.setOnClickListener(v -> {
            selectedServerDetail = server;
            setPage("server-detail");
        });
        return row;
    }

    private void buildServerDetailPage() {
        if (selectedServerDetail == null) {
            setPage("servers");
            return;
        }
        renderServerDetail(content, selectedServerDetail);
    }

    private void renderServerDetail(LinearLayout parent, JSONObject server) {
        if (useRedesignedServerDetail()) {
            renderServerDetailV2(parent, server);
            return;
        }
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
        stop.setOnClickListener(v -> {
            if (server.optBoolean("running")) {
                actionButton(stop, "/api/server/stop", "{}", "Stopping server...");
                releaseServerWakeLock();
            } else {
                acquireServerWakeLock();
                actionButton(stop, "/api/server/start", "{}", "Starting server...");
            }
        });
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

        Button delete = dangerButton("Delete Server + Backups");
        delete.setOnClickListener(v -> deleteServer(server));
        detail.addView(delete, fullWidthButtonParams());
    }

    private boolean useRedesignedServerDetail() {
        return true;
    }

    private void renderServerDetailV2(LinearLayout parent, JSONObject server) {
        JSONObject profile = server.optJSONObject("profile");
        boolean active = server.optBoolean("active");
        LinearLayout overview = glassCard();
        overview.setPadding(dp(16), dp(16), dp(16), dp(16));
        parent.addView(overview, matchWrapMargin(0, dp(8), 0, dp(12)));

        LinearLayout top = row();
        top.setGravity(Gravity.CENTER_VERTICAL);
        overview.addView(top, matchWrap());
        top.addView(loaderIconView(profile == null ? "-" : profile.optString("loader", "-")), new LinearLayout.LayoutParams(dp(58), dp(52)));
        LinearLayout status = column();
        LinearLayout.LayoutParams statusParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        statusParams.setMargins(dp(14), 0, dp(8), 0);
        top.addView(status, statusParams);
        status.addView(statusPill(serverStatusLabel(server), serverStatusColor(server)));
        status.addView(text(server.optString("joinAddress", joinAddressText()), 15, MUTED, false));
        addServerPlayItLines(status, server);
        status.addView(text("Minecraft " + (profile == null ? "-" : profile.optString("minecraftVersion", "-")) + " " + (profile == null ? "-" : profile.optString("loader", "-")), 14, MUTED, false));
        Button copy = secondaryButton("Copy");
        copy.setOnClickListener(v -> {
            android.content.ClipboardManager clipboard = (android.content.ClipboardManager) getSystemService(CLIPBOARD_SERVICE);
            if (clipboard != null) clipboard.setPrimaryClip(android.content.ClipData.newPlainText("MineMux address", server.optString("joinAddress", joinAddressText())));
            setNotice("Address copied.", false);
        });
        top.addView(copy, fixedButtonParams(76));

        LinearLayout info = row();
        overview.addView(info, matchWrapMargin(0, dp(14), 0, 0));
        info.addView(infoCell("Players", (active ? playerCountText() : "0") + " / " + (profile == null ? "-" : profile.optInt("maxPlayers", 0))), weightedMetricParams());
        info.addView(infoCell("Uptime", uptimeText()), weightedMetricParams());
        info.addView(infoCell("Memory", profile == null ? "-" : profile.optInt("memoryMb", 0) + " MB"), weightedMetricParams());

        LinearLayout actions = sectionCard("Actions", "", null);
        parent.addView(actions, matchWrapMargin(0, 0, 0, dp(12)));
        if (!active) {
            TextView hint = text("This server is not active. Make it active before using power, backup, mod, or file actions.", 13, MUTED, false);
            actions.addView(hint, matchWrapMargin(0, dp(8), 0, dp(8)));
            Button activateOnly = primaryButton("Make Active");
            activateOnly.setOnClickListener(v -> switchActiveServer(activateOnly, server.optString("id", "main")));
            actions.addView(activateOnly, fullWidthButtonParams());
        } else {
            LinearLayout actionsA = row();
            actions.addView(actionsA, matchWrapMargin(0, dp(8), 0, 0));
            Button power = server.optBoolean("running") ? dangerButton("Stop") : primaryButton("Start");
            power.setOnClickListener(v -> {
                if (server.optBoolean("running")) {
                    confirmStop(power);
                } else {
                    startServerFromUi(power);
                }
            });
            actionsA.addView(power, weightedButtonParams());
            Button restart = secondaryButton("Restart");
            restart.setOnClickListener(v -> confirmRestart(restart));
            actionsA.addView(restart, weightedButtonParams());
            Button backup = secondaryButton("Backup");
            backup.setOnClickListener(v -> actionButton(backup, "/api/backups/create", "{}", "Creating backup..."));
            actionsA.addView(backup, weightedButtonParams());
            LinearLayout actionsB = row();
            actions.addView(actionsB, matchWrap());
            Button mods = secondaryButton("Mods");
            mods.setOnClickListener(v -> setPage("mods"));
            actionsB.addView(mods, weightedButtonParams());
            Button backups = secondaryButton("Backups");
            backups.setOnClickListener(v -> setPage("backups"));
            actionsB.addView(backups, weightedButtonParams());
        }

        addServerPropertiesSection(parent, server, profile, active);
        addPlayItCard(parent, playItStatus(server), false);

        if (active) {
            LinearLayout backupCard = sectionCard("Backups", "View all >", () -> setPage("backups"));
            parent.addView(backupCard, matchWrapMargin(0, 0, 0, dp(12)));
            LinearLayout backupRows = column();
            backupCard.addView(backupRows, matchWrapMargin(0, dp(8), 0, 0));
            loadBackups(backupRows);
        }

        LinearLayout danger = sectionCard("Danger Zone", "", null);
        parent.addView(danger, matchWrapMargin(0, 0, 0, dp(14)));
        Button delete = dangerButton("Delete Server + Backups");
        delete.setOnClickListener(v -> deleteServer(server));
        danger.addView(delete, fullWidthButtonParams());
    }

    private void addServerPropertiesSection(LinearLayout parent, @Nullable JSONObject profile) {
        addServerPropertiesSection(parent, selectedServerDetail, profile, true);
    }

    private void addServerPropertiesSection(LinearLayout parent, @Nullable JSONObject server, @Nullable JSONObject profile, boolean editable) {
        LinearLayout settings = sectionCard("Server Properties", "", null);
        parent.addView(settings, matchWrapMargin(0, 0, 0, dp(12)));
        if (!editable) {
            settings.addView(text("Read-only until this server is active.", 13, MUTED, false), matchWrapMargin(0, dp(8), 0, dp(8)));
            settings.addView(summaryLine("Memory", (profile == null ? wizardMemory : profile.optInt("memoryMb", wizardMemory)) + " MB"));
            settings.addView(summaryLine("Max Players", String.valueOf(profile == null ? wizardPlayers : profile.optInt("maxPlayers", wizardPlayers))));
            settings.addView(summaryLine("View Distance", String.valueOf(profile == null ? 6 : profile.optInt("viewDistance", 6))));
            settings.addView(summaryLine("Simulation Distance", String.valueOf(profile == null ? 4 : profile.optInt("simulationDistance", 4))));
            settings.addView(summaryLine("MOTD", profileProperty(profile, "motd", "MineMux server")));
            settings.addView(summaryLine("Seed", profile == null ? "-" : profile.optString("seed", profileProperty(profile, "level-seed", "-"))));
        } else {
            settings.addView(configNumberRow("Memory MB", "memoryMb", profile == null ? wizardMemory : profile.optInt("memoryMb", wizardMemory), 512, 8192, "Maximum Java heap allocated to this server."));
            settings.addView(configNumberRow("Max Players", "maxPlayers", profile == null ? wizardPlayers : profile.optInt("maxPlayers", wizardPlayers), 1, 100, "Maximum player slots shown by the server."));
            settings.addView(configNumberRow("Render Distance", "viewDistance", profile == null ? 6 : profile.optInt("viewDistance", 6), 2, 32, "Chunk view distance. Lower values are safer on phones."));
            settings.addView(configNumberRow("Simulation Distance", "simulationDistance", profile == null ? 4 : profile.optInt("simulationDistance", 4), 2, 32, "Distance where mobs, redstone, and world simulation stay active."));
            settings.addView(configTextRow("MOTD", "motd", profileProperty(profile, "motd", "MineMux server"), "Message shown in the Minecraft server list."));
            settings.addView(configNumberRow("Server Port", "serverPort", profilePort(profile), 1024, 65535, "TCP port clients connect to. Default Minecraft Java port is 25565."));
            settings.addView(configTextRow("Gamemode", "gamemode", profileProperty(profile, "gamemode", "survival"), "Default mode: survival, creative, adventure, or spectator."));
            settings.addView(configTextRow("Difficulty", "difficulty", profileProperty(profile, "difficulty", "normal"), "World difficulty: peaceful, easy, normal, or hard."));
            settings.addView(configToggleRow("PVP", "pvp", boolProperty(profile, "pvp", true), "Allow players to damage other players."));
            settings.addView(configToggleRow("Online Mode", "onlineMode", boolProperty(profile, "online-mode", true), "Require Mojang/Microsoft account authentication."));
            settings.addView(configToggleRow("Allow Flight", "allowFlight", boolProperty(profile, "allow-flight", false), "Allow flying clients without server kicks."));
            settings.addView(configToggleRow("Command Blocks", "enableCommandBlock", boolProperty(profile, "enable-command-block", false), "Enable command block execution."));
            settings.addView(configNumberRow("Spawn Protection", "spawnProtection", intProperty(profile, "spawn-protection", 0), 0, 64, "Protected spawn radius in blocks."));
            settings.addView(configToggleRow("Whitelist", "whiteList", boolProperty(profile, "white-list", false), "Only allow players on the whitelist."));
        }
        Button icon = secondaryButton("Upload Server Icon");
        icon.setOnClickListener(v -> pickServerIcon(server == null ? "" : server.optString("id", "")));
        settings.addView(icon, fullWidthButtonParams());
        if (editable) {
            Button resourcePack = secondaryButton("Upload Resource Pack");
            resourcePack.setOnClickListener(v -> pickResourcePackZip());
            settings.addView(resourcePack, fullWidthButtonParams());
        }
        if (editable) {
            Button files = secondaryButton("File Browser");
            files.setOnClickListener(v -> {
                fileBrowserPath = "";
                setPage("server-files");
            });
            settings.addView(files, fullWidthButtonParams());
        }
    }

    private View configNumberRow(String title, String key, int value, int min, int max, String help) {
        EditText input = input(String.valueOf(value));
        return configInputRow(title, help, input, button -> {
            int next = clamp(numberOr(input, value), min, max);
            actionButton(button, "/api/config", "{\"" + key + "\":" + next + "}", "Saving " + title + "...");
        });
    }

    private View configTextRow(String title, String key, String value, String help) {
        EditText input = input(value);
        return configInputRow(title, help, input, button ->
            actionButton(button, "/api/config", "{\"" + key + "\":\"" + escapeJson(input.getText().toString().trim()) + "\"}", "Saving " + title + "..."));
    }

    private View configInputRow(String title, String help, EditText input, Consumer<Button> saveAction) {
        LinearLayout row = column();
        row.setPadding(0, dp(10), 0, dp(10));
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        row.addView(header, matchWrap());
        header.addView(text(title, 15, TEXT, true), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button info = secondaryButton("?");
        info.setOnClickListener(v -> showInfo(title, help));
        header.addView(info, fixedButtonParams(44));
        LinearLayout controls = row();
        controls.setGravity(Gravity.CENTER_VERTICAL);
        row.addView(controls, matchWrapMargin(0, dp(6), 0, 0));
        controls.addView(input, new LinearLayout.LayoutParams(0, dp(46), 1));
        Button save = primaryButton("Save");
        save.setOnClickListener(v -> saveAction.accept(save));
        controls.addView(save, fixedButtonParams(68));
        return row;
    }

    private View configToggleRow(String title, String key, boolean value, String help) {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(0, dp(10), 0, dp(10));
        LinearLayout copy = column();
        row.addView(copy, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        copy.addView(text(title, 15, TEXT, true));
        copy.addView(text(help, 12, MUTED, false));
        Button info = secondaryButton("?");
        info.setOnClickListener(v -> showInfo(title, help));
        row.addView(info, fixedButtonParams(44));
        CheckBox toggle = new CheckBox(this);
        toggle.setChecked(value);
        toggle.setButtonTintList(ColorStateList.valueOf(ACCENT));
        toggle.setOnCheckedChangeListener((button, checked) ->
            postJson("/api/config", "{\"" + key + "\":" + checked + "}", "Saving " + title + "...", () -> setNotice(title + " saved.", false)));
        row.addView(toggle);
        return row;
    }

    private void showInfo(String title, String message) {
        new AlertDialog.Builder(this).setTitle(title).setMessage(message).setPositiveButton("OK", null).show();
    }

    private String profileProperty(@Nullable JSONObject profile, String key, String fallback) {
        JSONObject props = profile == null ? null : profile.optJSONObject("properties");
        return props == null ? fallback : props.optString(key, fallback);
    }

    private boolean boolProperty(@Nullable JSONObject profile, String key, boolean fallback) {
        String value = profileProperty(profile, key, fallback ? "true" : "false");
        return "true".equalsIgnoreCase(value);
    }

    private int intProperty(@Nullable JSONObject profile, String key, int fallback) {
        try {
            return Integer.parseInt(profileProperty(profile, key, String.valueOf(fallback)));
        } catch (Exception e) {
            return fallback;
        }
    }

    private int profilePort(@Nullable JSONObject profile) {
        try {
            JSONObject ports = profile == null ? null : profile.optJSONObject("ports");
            return ports == null ? 25565 : ports.optInt("javaTcp", 25565);
        } catch (Exception e) {
            return 25565;
        }
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
            use.setOnClickListener(v -> switchActiveServer(use, id));
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
                    item.setBackground(makeBg(PANEL, STROKE, 8));
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
        Button upload = secondaryButton("Upload");
        upload.setOnClickListener(v -> pickBackupZip());
        actions.addView(upload, weightedButtonParams());

        addBackupPolicyCard();

        LinearLayout list = column();
        content.addView(list, matchWrap());
        loadBackups(list);
    }

    private void addBackupPolicyCard() {
        content.addView(sectionTitle("Auto Backups"));
        LinearLayout card = glassCard();
        card.setPadding(dp(16), dp(16), dp(16), dp(16));
        content.addView(card, matchWrapMargin(0, 0, 0, dp(12)));

        CheckBox enabled = settingCheckBox(
            "Auto backups",
            "Create scheduled backups for the active server.",
            backupPolicyAutoEnabled());
        card.addView(enabled, matchWrapMargin(0, 0, 0, dp(8)));

        Spinner interval = backupIntervalSpinner(backupPolicyIntervalMinutes());
        addField(card, "Interval", interval);

        EditText keep = input(String.valueOf(backupPolicyKeepAutoBackups()));
        addField(card, "Keep count", keep);

        TextView hint = text("Locked backups are kept until you delete them manually.", 12, MUTED, false);
        hint.setPadding(0, dp(8), 0, dp(10));
        card.addView(hint);

        Button save = primaryButton("Save Backup Settings");
        save.setOnClickListener(v -> saveBackupPolicy(save, enabled, interval, keep));
        card.addView(save, fullWidthButtonParams());
    }

    private Spinner backupIntervalSpinner(int currentMinutes) {
        int[] values = backupIntervalValues(currentMinutes);
        String[] labels = new String[values.length];
        int selected = 0;
        for (int i = 0; i < values.length; i++) {
            labels[i] = backupIntervalLabel(values[i]);
            if (values[i] == currentMinutes) selected = i;
        }
        Spinner spinner = spinner(labels);
        spinner.setTag(values);
        spinner.setSelection(selected);
        return spinner;
    }

    private int[] backupIntervalValues(int currentMinutes) {
        for (int preset : BACKUP_INTERVAL_PRESET_MINUTES) {
            if (preset == currentMinutes) return BACKUP_INTERVAL_PRESET_MINUTES;
        }
        int[] values = new int[BACKUP_INTERVAL_PRESET_MINUTES.length + 1];
        values[0] = Math.max(1, currentMinutes);
        for (int i = 0; i < BACKUP_INTERVAL_PRESET_MINUTES.length; i++) {
            values[i + 1] = BACKUP_INTERVAL_PRESET_MINUTES[i];
        }
        return values;
    }

    private String backupIntervalLabel(int minutes) {
        if (minutes == 30) return "Every 30 minutes";
        if (minutes == 60) return "Every hour";
        if (minutes % 1440 == 0) {
            int days = minutes / 1440;
            return days == 1 ? "Daily" : "Every " + days + " days";
        }
        if (minutes % 60 == 0) {
            int hours = minutes / 60;
            return "Every " + hours + " hours";
        }
        return "Every " + minutes + " minutes";
    }

    private void saveBackupPolicy(Button button, CheckBox enabled, Spinner interval, EditText keep) {
        int keepCount = clamp(numberOr(keep, backupPolicyKeepAutoBackups()), 1, 100);
        keep.setText(String.valueOf(keepCount));
        int intervalMinutes = selectedBackupIntervalMinutes(interval);
        String body = "{\"autoBackupEnabled\":" + enabled.isChecked()
            + ",\"autoBackupIntervalMinutes\":" + intervalMinutes
            + ",\"autoBackupKeep\":" + keepCount + "}";
        actionButton(button, "/api/config", body, "Saving backup settings...");
    }

    private int selectedBackupIntervalMinutes(Spinner interval) {
        Object tag = interval.getTag();
        if (tag instanceof int[]) {
            int[] values = (int[]) tag;
            int position = clamp(interval.getSelectedItemPosition(), 0, values.length - 1);
            return values[position];
        }
        return backupPolicyIntervalMinutes();
    }

    private JSONObject backupPolicy() {
        try {
            JSONObject profile = latestStatus == null ? null : latestStatus.optJSONObject("profile");
            JSONObject backups = profile == null ? null : profile.optJSONObject("backups");
            return backups == null ? new JSONObject() : backups;
        } catch (Exception e) {
            return new JSONObject();
        }
    }

    private boolean backupPolicyAutoEnabled() {
        return backupPolicy().optBoolean("autoEnabled", false);
    }

    private int backupPolicyIntervalMinutes() {
        return Math.max(1, backupPolicy().optInt("intervalMinutes", 360));
    }

    private int backupPolicyKeepAutoBackups() {
        return clamp(backupPolicy().optInt("keepAutoBackups", 5), 1, 100);
    }

    private void buildServerFilesPage() {
        LinearLayout panel = glassCard();
        panel.setPadding(dp(16), dp(16), dp(16), dp(16));
        content.addView(panel, matchWrapMargin(0, dp(8), 0, dp(12)));
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        panel.addView(header, matchWrap());
        Button up = secondaryButton("Up");
        up.setOnClickListener(v -> {
            if (!fileBrowserPath.isEmpty()) {
                int slash = fileBrowserPath.lastIndexOf('/');
                fileBrowserPath = slash <= 0 ? "" : fileBrowserPath.substring(0, slash);
                setPage("server-files");
            }
        });
        header.addView(up, fixedButtonParams(58));
        TextView path = text(fileBrowserPath.isEmpty() ? "/" : "/" + fileBrowserPath, 16, TEXT, true);
        header.addView(path, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button upload = primaryButton("Upload");
        upload.setOnClickListener(v -> pickServerUploadFile());
        header.addView(upload, fixedButtonParams(86));

        LinearLayout list = column();
        content.addView(list, matchWrap());
        loadServerFiles(list);
    }

    private void loadServerFiles(LinearLayout list) {
        list.removeAllViews();
        getJson("/api/server/files?path=" + urlEncode(fileBrowserPath), body -> {
            try {
                JSONArray files = new JSONObject(body).optJSONArray("files");
                if (files == null || files.length() == 0) {
                    list.addView(emptyState("This folder is empty."));
                    return;
                }
                for (int i = 0; i < files.length(); i++) {
                    JSONObject file = files.getJSONObject(i);
                    list.addView(serverFileRow(file));
                }
            } catch (Exception e) {
                list.addView(emptyState(e.getMessage()));
            }
        }, error -> list.addView(emptyState(error)));
    }

    private View serverFileRow(JSONObject file) {
        LinearLayout row = glassCard();
        row.setPadding(dp(12), dp(12), dp(12), dp(12));
        row.setOrientation(LinearLayout.VERTICAL);
        row.setLayoutParams(matchWrapMargin(0, 0, 0, dp(8)));
        boolean directory = file.optBoolean("directory");
        String path = file.optString("path", "");
        row.addView(text((directory ? "[DIR] " : "") + file.optString("name", path), 15, TEXT, true));
        row.addView(text(directory ? "Folder" : bytes(file.optLong("sizeBytes", 0)), 12, MUTED, false));
        LinearLayout actions = row();
        row.addView(actions, matchWrapMargin(0, dp(8), 0, 0));
        if (directory) {
            Button open = primaryButton("Open");
            open.setOnClickListener(v -> {
                fileBrowserPath = path;
                setPage("server-files");
            });
            actions.addView(open, weightedButtonParams());
            Button download = secondaryButton("Download");
            download.setOnClickListener(v -> downloadServerFile(path, true));
            actions.addView(download, weightedButtonParams());
        } else {
            Button edit = secondaryButton("Edit");
            edit.setOnClickListener(v -> editServerFile(path));
            actions.addView(edit, weightedButtonParams());
            Button download = secondaryButton("Download");
            download.setOnClickListener(v -> downloadServerFile(path, false));
            actions.addView(download, weightedButtonParams());
        }
        Button delete = dangerButton("Delete");
        delete.setOnClickListener(v -> confirmDeleteServerFile(path));
        actions.addView(delete, weightedButtonParams());
        return row;
    }

    private void editServerFile(String path) {
        getJson("/api/server/file?raw=1&path=" + urlEncode(path), body -> {
            try {
                JSONObject json = new JSONObject(body);
                EditText edit = input(json.optString("content", ""));
                edit.setSingleLine(false);
                edit.setMinLines(10);
                edit.setGravity(Gravity.TOP);
                new AlertDialog.Builder(this)
                    .setTitle(path)
                    .setView(edit)
                    .setNegativeButton("Cancel", null)
                    .setPositiveButton("Save", (dialog, which) ->
                        putJson("/api/server/file?path=" + urlEncode(path), "{\"content\":\"" + escapeJson(edit.getText().toString()) + "\"}", "Saving file...", () -> setPage("server-files")))
                    .show();
            } catch (Exception e) {
                setNotice(e.getMessage(), true);
            }
        }, error -> setNotice(error, true));
    }

    private void confirmDeleteServerFile(String path) {
        new AlertDialog.Builder(this)
            .setTitle("Delete file?")
            .setMessage(path)
            .setNegativeButton("Cancel", null)
            .setPositiveButton("Delete", (dialog, which) -> deleteJson("/api/server/file?path=" + urlEncode(path), "Deleting file...", () -> setPage("server-files")))
            .show();
    }

    private void pickServerUploadFile() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("*/*");
        startActivityForResult(intent, REQUEST_PICK_SERVER_UPLOAD);
    }

    private void pickSetupJar() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("application/java-archive");
        intent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{"application/java-archive", "application/x-java-archive", "application/octet-stream", "*/*"});
        startActivityForResult(intent, REQUEST_PICK_SETUP_JAR);
    }

    private void pickServerIcon(String serverId) {
        pendingIconServerId = serverId == null ? "" : serverId;
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("image/png");
        startActivityForResult(intent, REQUEST_PICK_SERVER_ICON);
    }

    private void pickResourcePackZip() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("application/zip");
        intent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{"application/zip", "application/x-zip-compressed", "application/octet-stream"});
        startActivityForResult(intent, REQUEST_PICK_RESOURCE_PACK);
    }

    private void buildSettingsPage() {
        pageTitle.setText("Settings");
        if (useRedesignedSettings()) {
            buildSettingsPageV2();
            return;
        }
        LinearLayout profile = glassCard();
        profile.setPadding(dp(24), dp(22), dp(24), dp(22));
        content.addView(profile, matchWrapMargin(0, dp(18), 0, dp(18)));
        LinearLayout profileTop = row();
        profileTop.setGravity(Gravity.CENTER_VERTICAL);
        profile.addView(profileTop, matchWrap());
        TextView avatar = text("Steve", 18, TEXT, true);
        avatar.setGravity(Gravity.CENTER);
        avatar.setBackground(makeBg(0xff1c1c1c, GREEN, 36));
        profileTop.addView(avatar, new LinearLayout.LayoutParams(dp(92), dp(92)));
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
        stats.addView(profileStat("GB", diskText(), "Cloud storage", PURPLE), weightedButtonParams());
        stats.addView(profileStat("$", "Pro", "Subscription", ACCENT), weightedButtonParams());

        LinearLayout general = settingRowIcon("⚙", "General Settings", "Configure general app preferences", ACCENT, null);
        content.addView(general, matchWrapMargin(0, 0, 0, dp(18)));

        TextView preferencesTitle = text("Preferences", 17, MUTED, true);
        preferencesTitle.setPadding(dp(2), 0, 0, dp(10));
        content.addView(preferencesTitle);
        LinearLayout preferences = sectionCard("", "", null);
        content.addView(preferences, matchWrapMargin(0, 0, 0, dp(18)));
        preferences.addView(toggleSettingRow("●", "Notifications", "Manage push notifications", WARNING, true, null));
        preferences.addView(themeSettingRow());
        preferences.addView(toggleSettingRow("▶", "Start on boot", "Start active server when daemon starts", GREEN, profileFeatureBool("autoStart", false),
            checked -> postJson("/api/config", "{\"autoStart\":" + checked + "}", "Saving start setting...", () -> setNotice("Start setting saved.", false))));
        preferences.addView(toggleSettingRow("↻", "Auto-restart", "Restart server automatically after crash", ACCENT, profileFeatureBool("restartOnCrash", true),
            checked -> postJson("/api/config", "{\"restartOnCrash\":" + checked + "}", "Saving restart setting...", () -> setNotice("Restart setting saved.", false))));
        preferences.addView(settingRowIcon("▰", "Storage", "Manage backups and disk usage", WARNING, () -> setPage("backups")));
        preferences.addView(settingRowIcon("✚", "Integrations", "Modrinth, GitHub, Discord", MUTED, null));

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

    private boolean useRedesignedSettings() {
        return true;
    }

    private void buildSettingsPageV2() {
        TextView preferencesTitle = sectionTitle("Preferences");
        content.addView(preferencesTitle);
        LinearLayout preferencesCard = sectionCard("", "", null);
        content.addView(preferencesCard, matchWrapMargin(0, 0, 0, dp(16)));
        boolean notifications = preferences == null || preferences.getBoolean(PREF_NOTIFICATIONS, true);
        preferencesCard.addView(toggleSettingRow("!", "Notifications", "Show setup, backup, and server state alerts", WARNING, notifications, checked -> {
            if (preferences != null) preferences.edit().putBoolean(PREF_NOTIFICATIONS, checked).apply();
            MineMuxRuntime.refreshServiceNotification(this);
        }));
        preferencesCard.addView(themeSettingRowV2());
        preferencesCard.addView(toggleSettingRow(">", "Start server on phone boot", "MineMux runs headless after boot and starts the active server", GREEN, profileFeatureBool("autoStart", false),
            checked -> postJson("/api/config", "{\"autoStart\":" + checked + "}", "Saving boot setting...", () -> setNotice("Boot setting saved.", false))));
        preferencesCard.addView(toggleSettingRow("R", "Auto-restart on crash", "Restart the active server after a crash", ACCENT, profileFeatureBool("restartOnCrash", true),
            checked -> postJson("/api/config", "{\"restartOnCrash\":" + checked + "}", "Saving restart setting...", () -> setNotice("Restart setting saved.", false))));
        preferencesCard.addView(settingRowIcon("B", "Backups", "Create, upload, restore, and download backups", GREEN, () -> setPage("backups")));

        addServerDefaultsSection();

        content.addView(sectionTitle("Advanced Tools"));
        LinearLayout tools = sectionCard("", "", null);
        content.addView(tools, matchWrapMargin(0, 0, 0, dp(16)));
        tools.addView(settingRowIcon("T", "Terminal", "Open shell recovery", GREEN, () -> startActivity(new Intent(this, TermuxActivity.class))));
        tools.addView(settingRowIcon("W", "Web UI", "Open advanced browser console", MUTED, () -> startActivity(new Intent(this, MineMuxWebActivity.class))));

        TextView footer = text("App Version " + appVersionText(), 11, TERTIARY, false);
        footer.setGravity(Gravity.CENTER);
        footer.setPadding(0, dp(4), 0, dp(18));
        content.addView(footer, matchWrap());
    }

    private TextView sectionTitle(String title) {
        TextView view = text(title, 16, MUTED, true);
        view.setPadding(dp(2), 0, 0, dp(8));
        return view;
    }

    private void addSettingsProfileCard() {
        LinearLayout profile = glassCard();
        profile.setPadding(dp(16), dp(16), dp(16), dp(16));
        content.addView(profile, matchWrapMargin(0, dp(12), 0, dp(16)));

        LinearLayout top = row();
        top.setGravity(Gravity.CENTER_VERTICAL);
        profile.addView(top, matchWrap());
        TextView avatar = text("C", 34, 0xff050505, true);
        avatar.setGravity(Gravity.CENTER);
        avatar.setIncludeFontPadding(false);
        avatar.setBackground(makeBg(GREEN, GREEN, 18));
        top.addView(avatar, new LinearLayout.LayoutParams(dp(76), dp(76)));
        LinearLayout identity = column();
        LinearLayout.LayoutParams identityParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        identityParams.setMargins(dp(14), 0, 0, 0);
        top.addView(identity, identityParams);
        identity.addView(text("Mc Phone", 26, TEXT, true));
        identity.addView(text("mcphone@example.com", 14, MUTED, false));
        identity.addView(statusPill("Free", MUTED), matchWrapMargin(0, dp(8), 0, 0));

        LinearLayout statsA = row();
        profile.addView(statsA, matchWrapMargin(0, dp(14), 0, dp(4)));
        TextView servers = profileStatValue("0", "Servers");
        TextView backups = profileStatValue("0", "Backups");
        statsA.addView(profileStatCard(servers), weightedMetricParams());
        statsA.addView(profileStatCard(backups), weightedMetricParams());
        LinearLayout statsB = row();
        profile.addView(statsB, matchWrap());
        TextView backupSize = profileStatValue("0 KB", "Cloud Backups");
        TextView plan = profileStatValue("Free", "Subscription");
        statsB.addView(profileStatCard(backupSize), weightedMetricParams());
        statsB.addView(profileStatCard(plan), weightedMetricParams());
        loadSettingsStats(servers, backups, backupSize);
    }

    private TextView profileStatValue(String value, String label) {
        TextView view = text(value + "\n" + label, 15, TEXT, true);
        view.setGravity(Gravity.CENTER);
        return view;
    }

    private LinearLayout profileStatCard(TextView value) {
        LinearLayout card = card();
        card.setPadding(dp(8), dp(10), dp(8), dp(10));
        card.addView(value);
        return card;
    }

    private void loadSettingsStats(TextView servers, TextView backups, TextView backupSize) {
        getJson("/api/servers", body -> {
            try {
                JSONArray list = new JSONObject(body).optJSONArray("servers");
                int count = list == null ? 0 : list.length();
                servers.setText(count + "\nServers");
            } catch (Exception ignored) {}
        }, error -> {});
        getJson("/api/backups", body -> {
            try {
                JSONArray list = new JSONObject(body).optJSONArray("backups");
                int count = list == null ? 0 : list.length();
                long total = 0;
                if (list != null) {
                    for (int i = 0; i < list.length(); i++) total += list.getJSONObject(i).optLong("sizeBytes", 0);
                }
                backups.setText(count + "\nBackups");
                backupSize.setText(bytes(total) + "\nCloud Backups");
            } catch (Exception ignored) {}
        }, error -> {});
    }

    private void addServerDefaultsSection() {
        content.addView(sectionTitle("Server Defaults"));
        LinearLayout defaults = sectionCard("", "", null);
        content.addView(defaults, matchWrapMargin(0, 0, 0, dp(16)));
        int memoryDefault = preferences == null ? profileInt("memoryMb", wizardMemory) : preferences.getInt(PREF_DEFAULT_MEMORY_MB, profileInt("memoryMb", wizardMemory));
        int playersDefault = preferences == null ? profileInt("maxPlayers", wizardPlayers) : preferences.getInt(PREF_DEFAULT_MAX_PLAYERS, profileInt("maxPlayers", wizardPlayers));
        int viewDefault = preferences == null ? profileInt("viewDistance", 6) : preferences.getInt(PREF_DEFAULT_VIEW_DISTANCE, profileInt("viewDistance", 6));
        int simDefault = preferences == null ? profileInt("simulationDistance", 4) : preferences.getInt(PREF_DEFAULT_SIM_DISTANCE, profileInt("simulationDistance", 4));
        EditText memoryInput = input(String.valueOf(memoryDefault));
        addField(defaults, "Memory MB", memoryInput);
        EditText playersInput = input(String.valueOf(playersDefault));
        addField(defaults, "Max players", playersInput);
        EditText viewInput = input(String.valueOf(viewDefault));
        addField(defaults, "Render distance", viewInput);
        EditText simInput = input(String.valueOf(simDefault));
        addField(defaults, "Simulation distance", simInput);

        LinearLayout actions = row();
        defaults.addView(actions, matchWrapMargin(0, dp(10), 0, 0));
        Button tune = secondaryButton("Auto Set");
        tune.setOnClickListener(v -> autoTuneDefaults(memoryInput, playersInput, viewInput, simInput));
        actions.addView(tune, weightedButtonParams());
        Button save = primaryButton("Save");
        save.setOnClickListener(v -> saveDefaultServerSettings(save, memoryInput, playersInput, viewInput, simInput));
        actions.addView(save, weightedButtonParams());
    }

    private void autoTuneDefaults(EditText memoryInput, EditText playersInput, EditText viewInput, EditText simInput) {
        long totalMemory = 0;
        int cores = 0;
        try {
            JSONObject system = latestStatus == null ? null : latestStatus.optJSONObject("system");
            if (system != null) {
                totalMemory = system.getJSONObject("memory").optLong("totalBytes", 0);
                cores = system.getJSONObject("cpu").optInt("cores", 0);
            }
        } catch (Exception ignored) {}
        long gb = totalMemory / 1024L / 1024L / 1024L;
        int memory = gb >= 7 ? 3072 : gb >= 5 ? 2048 : gb >= 3 ? 1536 : 1024;
        int players = gb >= 7 && cores >= 6 ? 12 : gb >= 5 ? 8 : 4;
        int view = gb >= 7 ? 8 : gb >= 5 ? 6 : 4;
        int sim = gb >= 7 ? 6 : gb >= 5 ? 4 : 3;
        memoryInput.setText(String.valueOf(memory));
        playersInput.setText(String.valueOf(players));
        viewInput.setText(String.valueOf(view));
        simInput.setText(String.valueOf(sim));
        setNotice("Defaults tuned for this phone.", false);
    }

    private void saveDefaultServerSettings(Button button, EditText memoryInput, EditText playersInput, EditText viewInput, EditText simInput) {
        int memoryValue = clamp(numberOr(memoryInput, wizardMemory), 512, 8192);
        int playersValue = clamp(numberOr(playersInput, wizardPlayers), 1, 50);
        int viewValue = clamp(numberOr(viewInput, 6), 2, 32);
        int simValue = clamp(numberOr(simInput, 4), 2, 32);
        if (preferences != null) {
            preferences.edit()
                .putInt(PREF_DEFAULT_MEMORY_MB, memoryValue)
                .putInt(PREF_DEFAULT_MAX_PLAYERS, playersValue)
                .putInt(PREF_DEFAULT_VIEW_DISTANCE, viewValue)
                .putInt(PREF_DEFAULT_SIM_DISTANCE, simValue)
                .apply();
        }
        String body = "{\"memoryMb\":" + memoryValue
            + ",\"maxPlayers\":" + playersValue
            + ",\"viewDistance\":" + viewValue
            + ",\"simulationDistance\":" + simValue + "}";
        if (!activeServerInstalled()) {
            setNotice("Default settings saved.", false);
            return;
        }
        actionButton(button, "/api/config", body, "Saving server defaults...");
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
        LinearLayout actions = row();
        row.addView(actions, matchWrapMargin(0, dp(6), 0, 0));
        Button download = secondaryButton("Download");
        download.setOnClickListener(v -> downloadBackup(id));
        actions.addView(download, weightedButtonParams());
        Button delete = secondaryButton("Delete");
        delete.setOnClickListener(v -> deleteJson("/api/backups/" + urlEncode(id), "Deleting backup...", () -> {
            if ("backups".equals(currentPage)) setPage("backups");
        }));
        actions.addView(delete, weightedButtonParams());
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
        view.setBackground(makeBg(PANEL, STROKE, 8));
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
                boolean readyNow = server.optBoolean("ready", !runningNow);
                boolean stoppingNow = server.optBoolean("stopping", false);
                boolean startingNow = server.optBoolean("starting", false);
                if (runningNow && readyNow) startingServer = false;
                if (!runningNow || !stoppingNow) stoppingServer = false;
                sampleDashboardHistory(server);
                updateServerWakeLock(runningNow || startingNow || stoppingNow || startingServer || stoppingServer);
                if (runningNow || startingNow || stoppingNow || startingServer || stoppingServer) {
                    MineMuxRuntime.acquireServiceWakeLock(this);
                }
                updateDashboardLiveViews();
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

    private void maybeRequestBatteryOptimizationExemption() {
        if (batteryPromptShown || Build.VERSION.SDK_INT < Build.VERSION_CODES.M) return;
        batteryPromptShown = true;
        try {
            PowerManager power = (PowerManager) getSystemService(POWER_SERVICE);
            if (power != null && !power.isIgnoringBatteryOptimizations(getPackageName())) {
                Intent intent = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS);
                intent.setData(Uri.parse("package:" + getPackageName()));
                startActivity(intent);
            }
        } catch (Exception e) {
            try {
                startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS));
            } catch (Exception ignored) {}
        }
    }

    private void applyPaletteFromPreferences() {
        themeMode = preferences == null ? "System" : preferences.getString(PREF_THEME_MODE, "System");
        boolean dark = shouldUseDarkPalette(themeMode);
        if (dark) {
            BG = 0xff050505;
            PANEL = 0xff111111;
            PANEL_ALT = 0xff1a1a1a;
            PANEL_SOFT = 0xff0d0d0d;
            STROKE = 0xff303030;
            TEXT = 0xfff4f4f4;
            MUTED = 0xffa8a8a8;
            TERTIARY = 0xff737373;
            ACCENT = GREEN;
            ACCENT_TEXT = 0xffffffff;
            WARNING = 0xffd6d6d6;
            ERROR = 0xfff4f4f4;
            RED = 0xff202020;
            PURPLE = 0xff8a8a8a;
        } else {
            BG = 0xfff6f6f6;
            PANEL = 0xffffffff;
            PANEL_ALT = 0xffeeeeee;
            PANEL_SOFT = 0xfffbfbfb;
            STROKE = 0xffd8d8d8;
            TEXT = 0xff111111;
            MUTED = 0xff626262;
            TERTIARY = 0xff8a8a8a;
            ACCENT = GREEN;
            ACCENT_TEXT = 0xffffffff;
            WARNING = 0xff4c4c4c;
            ERROR = 0xff111111;
            RED = 0xffe8e8e8;
            PURPLE = 0xff707070;
        }
    }

    private boolean shouldUseDarkPalette(String mode) {
        if ("Dark".equalsIgnoreCase(mode)) return true;
        if ("Light".equalsIgnoreCase(mode)) return false;
        int current = getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK;
        return current == Configuration.UI_MODE_NIGHT_YES;
    }

    private void rebuildWithTheme(String mode) {
        if (preferences != null) preferences.edit().putString(PREF_THEME_MODE, mode).apply();
        String page = currentPage;
        applyPaletteFromPreferences();
        setContentView(buildContent());
        if (!"dashboard".equals(page)) setPage(page);
    }

    private void updateServerWakeLock(boolean running) {
        if (running) acquireServerWakeLock();
        else releaseServerWakeLock();
    }

    private void acquireServerWakeLock() {
        try {
            if (serverWakeLock != null && serverWakeLock.isHeld()) return;
            PowerManager power = (PowerManager) getSystemService(POWER_SERVICE);
            if (power == null) return;
            serverWakeLock = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "MineMux:ServerWakeLock");
            serverWakeLock.setReferenceCounted(false);
            serverWakeLock.acquire();
        } catch (Exception ignored) {}
    }

    private void releaseServerWakeLock() {
        try {
            if (serverWakeLock != null && serverWakeLock.isHeld()) serverWakeLock.release();
        } catch (Exception ignored) {
        } finally {
            serverWakeLock = null;
        }
    }

    private void refreshLogs() {
        getJson("/api/server/logs", body -> {
            try {
                JSONArray lines = new JSONObject(body).optJSONArray("lines");
                if (lines == null || lines.length() == 0) {
                    if (logs != null) logs.setText("No logs yet.");
                    if (setupLogOutput != null) setupLogOutput.setText("Waiting for setup output...");
                    return;
                }
                StringBuilder out = new StringBuilder();
                int start = Math.max(0, lines.length() - 12);
                for (int i = start; i < lines.length(); i++) out.append(lines.getString(i)).append('\n');
                if (logs != null) logs.setText(out.toString());
                if (setupLogOutput != null) setupLogOutput.setText(out.toString());
                updateOperationFromLog(lines);
            } catch (Exception e) {
                if (logs != null) logs.setText(e.getMessage());
                if (setupLogOutput != null) setupLogOutput.setText(e.getMessage());
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
        String actionKey = path + ":" + json;
        if (operationRunning || isCoolingDown(button) || isActionCoolingDown(actionKey)) {
            setNotice("Still working. Wait a moment.", false);
            return;
        }
        actionCooldowns.put(actionKey, System.currentTimeMillis() + 1500);
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
        return original;
    }

    private void startServerFromUi(Button button) {
        long now = System.currentTimeMillis();
        if (lastStopRequestedAtMs > 0 && now - lastStopRequestedAtMs < 30000) {
            setNotice("Server just stopped. Wait " + shortDuration((30000 - (now - lastStopRequestedAtMs)) / 1000) + " before starting.", true);
            return;
        }
        startingServer = true;
        stoppingServer = false;
        lastStartRequestedAtMs = now;
        acquireServerWakeLock();
        MineMuxRuntime.acquireServiceWakeLock(this);
        actionButton(button, "/api/server/start", "{}", "Starting server...");
        updateDashboardLiveViews();
    }

    private void switchActiveServer(Button button, String serverId) {
        actionButton(button, "/api/servers/switch", "{\"serverId\":\"" + escapeJson(serverId) + "\"}", "Switching server...");
        lastStartRequestedAtMs = 0;
        lastStopRequestedAtMs = 0;
        startingServer = false;
        stoppingServer = false;
        actionCooldowns.clear();
        buttonCooldowns.clear();
        mainHandler.postDelayed(() -> {
            refreshStatus();
            if ("server-detail".equals(currentPage)) setPage("servers");
        }, 800);
    }

    private void confirmStop(Button button) {
        if (operationRunning) {
            setNotice("Still working. Wait a moment.", false);
            return;
        }
        long now = System.currentTimeMillis();
        if (lastStartRequestedAtMs > 0 && now - lastStartRequestedAtMs < 30000 && !serverReady()) {
            setNotice("Server is still starting. Wait " + shortDuration((30000 - (now - lastStartRequestedAtMs)) / 1000) + " before stopping.", true);
            return;
        }
        new AlertDialog.Builder(this)
            .setTitle("Stop server?")
            .setMessage("This disconnects players and stops the running world.")
            .setNegativeButton("Cancel", null)
            .setPositiveButton("Stop", (dialog, which) -> {
                stoppingServer = true;
                startingServer = false;
                lastStopRequestedAtMs = System.currentTimeMillis();
                actionButton(button, "/api/server/stop", "{}", "Stopping server...");
                releaseServerWakeLock();
                updateDashboardLiveViews();
            })
            .show();
    }

    private void confirmRestart(Button button) {
        if (operationRunning) {
            setNotice("Still working. Wait a moment.", false);
            return;
        }
        new AlertDialog.Builder(this)
            .setTitle("Restart server?")
            .setMessage("Players may be disconnected while the server restarts.")
            .setNegativeButton("Cancel", null)
            .setPositiveButton("Restart", (dialog, which) -> {
                startingServer = true;
                stoppingServer = false;
                MineMuxRuntime.acquireServiceWakeLock(this);
                actionButton(button, "/api/server/restart", "{}", "Restarting server...");
                updateDashboardLiveViews();
            })
            .show();
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
        if (globalProgress != null) {
            globalProgress.setIndeterminate(!setupProgressRunning);
            globalProgress.setVisibility(View.VISIBLE);
        }
        if (operationPanel != null) operationPanel.setVisibility(View.VISIBLE);
        if (operationTitle != null) operationTitle.setText(title == null || title.isEmpty() ? "Working" : title);
        if (operationDetail != null) operationDetail.setText(detail == null || detail.isEmpty() ? "Working..." : detail);
        setControllerActionsEnabled(false);
    }

    private void beginSetupProgress() {
        setupProgressRunning = true;
        setupStartedAtMs = System.currentTimeMillis();
        updateSetupProgress();
    }

    private void updateSetupProgress() {
        if (!setupProgressRunning) return;
        int percent = setupProgressPercent();
        String detail = setupProgressText();
        if (operationDetail != null) operationDetail.setText(detail);
        if (setupProgressDetail != null) setupProgressDetail.setText(detail);
        operationMessage = detail;
        if (globalProgress != null) {
            globalProgress.setIndeterminate(false);
            globalProgress.setMax(100);
            globalProgress.setProgress(percent);
            globalProgress.setVisibility(View.VISIBLE);
        }
        if (setupProgressBar != null) {
            setupProgressBar.setIndeterminate(false);
            setupProgressBar.setMax(100);
            setupProgressBar.setProgress(percent);
        }
        mainHandler.postDelayed(this::updateSetupProgress, 1500);
    }

    private int setupProgressPercent() {
        if (!setupProgressRunning || setupStartedAtMs <= 0) return 0;
        long elapsed = Math.max(0, System.currentTimeMillis() - setupStartedAtMs);
        return Math.min(95, (int) (elapsed * 100 / setupEstimateMs));
    }

    private String setupProgressText() {
        if (!setupProgressRunning || setupStartedAtMs <= 0) return "Waiting to start...";
        long elapsed = Math.max(0, System.currentTimeMillis() - setupStartedAtMs);
        long remaining = Math.max(0, setupEstimateMs - elapsed);
        return "Setting up server... " + setupProgressPercent() + "% - ETA " + shortDuration(remaining / 1000);
    }

    private void endOperation() {
        setupProgressRunning = false;
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

    private boolean isActionCoolingDown(String key) {
        Long until = actionCooldowns.get(key);
        return until != null && System.currentTimeMillis() < until;
    }

    private void postJson(String path, String json, String progress, Runnable done) {
        if (!operationRunning) setNotice(progress, false);
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

    private void deleteJson(String path, String progress, Runnable done) {
        setNotice(progress, false);
        new Thread(() -> {
            try {
                request(path, "DELETE", null);
                mainHandler.post(() -> {
                    setNotice("Deleted", false);
                    refreshStatus();
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

    private void putJson(String path, String json, String progress, Runnable done) {
        setNotice(progress, false);
        new Thread(() -> {
            try {
                request(path, "PUT", json);
                mainHandler.post(() -> {
                    setNotice("Saved", false);
                    done.run();
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void deleteServer(JSONObject server) {
        String id = server.optString("id", "");
        if (id.isEmpty()) return;
        new AlertDialog.Builder(this)
            .setTitle("Delete server?")
            .setMessage("This deletes the server and matching backups. This cannot be undone.")
            .setNegativeButton("Cancel", null)
            .setPositiveButton("Delete", (dialog, which) -> deleteJson("/api/servers/" + urlEncode(id), "Deleting server and backups...", () -> {
                selectedServerDetail = null;
                setPage("servers");
            }))
            .show();
    }

    private void pickBackupZip() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("application/zip");
        intent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{"application/zip", "application/x-zip-compressed", "application/octet-stream"});
        try {
            startActivityForResult(intent, REQUEST_PICK_BACKUP_ZIP);
        } catch (Exception e) {
            intent.setType("*/*");
            startActivityForResult(intent, REQUEST_PICK_BACKUP_ZIP);
        }
    }

    private void downloadBackup(String id) {
        setNotice("Downloading backup...", false);
        new Thread(() -> {
            try {
                URL url = new URL(MineMuxRuntime.DASHBOARD_URL + "/api/backups/download?id=" + urlEncode(id));
                HttpURLConnection connection = (HttpURLConnection) url.openConnection();
                connection.setConnectTimeout(5000);
                connection.setReadTimeout(600000);
                Uri target = createDownloadUri(id + ".zip", "application/zip");
                if (target == null) throw new Exception("Could not create Downloads file");
                try (InputStream in = connection.getInputStream(); OutputStream out = getContentResolver().openOutputStream(target)) {
                    if (out == null) throw new Exception("Could not open Downloads file");
                    byte[] buffer = new byte[8192];
                    int read;
                    while ((read = in.read(buffer)) >= 0) out.write(buffer, 0, read);
                }
                mainHandler.post(() -> setNotice("Downloaded to Downloads/" + id + ".zip", false));
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void downloadServerFile(String path, boolean directory) {
        setNotice("Downloading file...", false);
        new Thread(() -> {
            try {
                URL url = new URL(MineMuxRuntime.DASHBOARD_URL + "/api/server/file?path=" + urlEncode(path));
                HttpURLConnection connection = (HttpURLConnection) url.openConnection();
                connection.setConnectTimeout(5000);
                connection.setReadTimeout(600000);
                String name = path.contains("/") ? path.substring(path.lastIndexOf('/') + 1) : path;
                if (name == null || name.trim().isEmpty()) name = "server-files";
                if (directory && !name.endsWith(".zip")) name += ".zip";
                final String downloadName = name;
                Uri target = createDownloadUri(downloadName, directory ? "application/zip" : "application/octet-stream");
                if (target == null) throw new Exception("Could not create Downloads file");
                try (InputStream in = connection.getInputStream(); OutputStream out = getContentResolver().openOutputStream(target)) {
                    if (out == null) throw new Exception("Could not open Downloads file");
                    byte[] buffer = new byte[8192];
                    int read;
                    while ((read = in.read(buffer)) >= 0) out.write(buffer, 0, read);
                }
                mainHandler.post(() -> setNotice("Downloaded to Downloads/" + downloadName, false));
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void uploadServerFile(Uri uri, String targetPath) {
        setNotice("Uploading file...", false);
        new Thread(() -> {
            try {
                String fileName = displayName(uri);
                if (fileName == null || fileName.trim().isEmpty()) fileName = "upload.bin";
                String boundary = "minemux-file-" + System.currentTimeMillis();
                HttpURLConnection connection = (HttpURLConnection) new URL(MineMuxRuntime.DASHBOARD_URL + "/api/server/file/upload?path=" + urlEncode(targetPath)).openConnection();
                connection.setConnectTimeout(5000);
                connection.setReadTimeout(600000);
                connection.setRequestMethod("POST");
                connection.setDoOutput(true);
                connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
                try (OutputStream out = connection.getOutputStream(); InputStream in = getContentResolver().openInputStream(uri)) {
                    if (in == null) throw new Exception("Could not open selected file");
                    out.write(("--" + boundary + "\r\n").getBytes(StandardCharsets.UTF_8));
                    out.write(("Content-Disposition: form-data; name=\"file\"; filename=\"" + fileName.replace("\"", "") + "\"\r\n").getBytes(StandardCharsets.UTF_8));
                    out.write("Content-Type: application/octet-stream\r\n\r\n".getBytes(StandardCharsets.UTF_8));
                    byte[] buffer = new byte[8192];
                    int read;
                    while ((read = in.read(buffer)) >= 0) out.write(buffer, 0, read);
                    out.write(("\r\n--" + boundary + "--\r\n").getBytes(StandardCharsets.UTF_8));
                }
                int code = connection.getResponseCode();
                if (code < 200 || code >= 300) throw new Exception(readError(connection));
                mainHandler.post(() -> {
                    setNotice("File uploaded.", false);
                    setPage("server-files");
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void uploadSetupJar(Uri uri) {
        setNotice("Uploading setup JAR...", false);
        new Thread(() -> {
            try {
                String response = multipartUpload(uri, "/api/server/setup/jar", "file");
                JSONObject json = new JSONObject(response);
                wizardCustomJarId = json.optString("customJarId", "");
                wizardCustomJarName = json.optString("fileName", displayName(uri));
                wizardJarMode = "custom";
                mainHandler.post(() -> {
                    setNotice("Custom server.jar uploaded.", false);
                    setPage("setup");
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void uploadServerIcon(Uri uri, String serverId) {
        setNotice("Uploading server icon...", false);
        new Thread(() -> {
            try {
                String id = serverId == null || serverId.trim().isEmpty() ? "" : "?serverId=" + urlEncode(serverId);
                multipartUpload(uri, "/api/server/icon" + id, "file");
                mainHandler.post(() -> {
                    setNotice("Server icon uploaded.", false);
                    refreshStatus();
                    if ("server-detail".equals(currentPage)) setPage("server-detail");
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private void uploadResourcePack(Uri uri) {
        setNotice("Uploading resource pack...", false);
        new Thread(() -> {
            try {
                multipartUpload(uri, "/api/server/resource-pack", "file");
                mainHandler.post(() -> {
                    setNotice("Resource pack uploaded and linked.", false);
                    refreshStatus();
                    if ("server-detail".equals(currentPage)) setPage("server-detail");
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private String multipartUpload(Uri uri, String path, String fieldName) throws Exception {
        String fileName = displayName(uri);
        if (fileName == null || fileName.trim().isEmpty()) fileName = "upload.bin";
        String boundary = "minemux-upload-" + System.currentTimeMillis();
        HttpURLConnection connection = (HttpURLConnection) new URL(MineMuxRuntime.DASHBOARD_URL + path).openConnection();
        connection.setConnectTimeout(5000);
        connection.setReadTimeout(600000);
        connection.setRequestMethod("POST");
        connection.setDoOutput(true);
        connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
        try (OutputStream out = connection.getOutputStream(); InputStream in = getContentResolver().openInputStream(uri)) {
            if (in == null) throw new Exception("Could not open selected file");
            out.write(("--" + boundary + "\r\n").getBytes(StandardCharsets.UTF_8));
            out.write(("Content-Disposition: form-data; name=\"" + fieldName + "\"; filename=\"" + fileName.replace("\"", "") + "\"\r\n").getBytes(StandardCharsets.UTF_8));
            out.write("Content-Type: application/octet-stream\r\n\r\n".getBytes(StandardCharsets.UTF_8));
            byte[] buffer = new byte[8192];
            int read;
            while ((read = in.read(buffer)) >= 0) out.write(buffer, 0, read);
            out.write(("\r\n--" + boundary + "--\r\n").getBytes(StandardCharsets.UTF_8));
        }
        int code = connection.getResponseCode();
        if (code < 200 || code >= 300) throw new Exception(readError(connection));
        try (InputStream in = connection.getInputStream(); BufferedReader reader = new BufferedReader(new InputStreamReader(in))) {
            StringBuilder body = new StringBuilder();
            String line;
            while ((line = reader.readLine()) != null) body.append(line);
            return body.toString();
        }
    }

    private Uri createDownloadUri(String fileName, String mimeType) {
        ContentValues values = new ContentValues();
        values.put(MediaStore.Downloads.DISPLAY_NAME, fileName);
        values.put(MediaStore.Downloads.MIME_TYPE, mimeType);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            values.put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS);
            return getContentResolver().insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values);
        }
        File downloads = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS);
        if (!downloads.exists()) //noinspection ResultOfMethodCallIgnored
            downloads.mkdirs();
        File outFile = new File(downloads, fileName);
        return Uri.fromFile(outFile);
    }

    private void uploadBackupAndRestore(Uri uri) {
        setNotice("Uploading backup...", false);
        new Thread(() -> {
            try {
                String fileName = displayName(uri);
                if (fileName == null || fileName.trim().isEmpty()) fileName = "uploaded-backup.zip";
                if (!fileName.toLowerCase().endsWith(".zip")) fileName = fileName + ".zip";
                String boundary = "minemux-" + System.currentTimeMillis();
                HttpURLConnection connection = (HttpURLConnection) new URL(MineMuxRuntime.DASHBOARD_URL + "/api/backups/upload?restore=true").openConnection();
                connection.setConnectTimeout(5000);
                connection.setReadTimeout(600000);
                connection.setRequestMethod("POST");
                connection.setDoOutput(true);
                connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
                try (OutputStream out = connection.getOutputStream(); InputStream in = getContentResolver().openInputStream(uri)) {
                    if (in == null) throw new Exception("Could not open selected backup");
                    out.write(("--" + boundary + "\r\n").getBytes(StandardCharsets.UTF_8));
                    out.write(("Content-Disposition: form-data; name=\"file\"; filename=\"" + fileName.replace("\"", "") + "\"\r\n").getBytes(StandardCharsets.UTF_8));
                    out.write("Content-Type: application/zip\r\n\r\n".getBytes(StandardCharsets.UTF_8));
                    byte[] buffer = new byte[8192];
                    int read;
                    while ((read = in.read(buffer)) >= 0) out.write(buffer, 0, read);
                    out.write(("\r\n--" + boundary + "--\r\n").getBytes(StandardCharsets.UTF_8));
                }
                int code = connection.getResponseCode();
                if (code < 200 || code >= 300) throw new Exception(readError(connection));
                mainHandler.post(() -> {
                    setNotice("Backup uploaded and restored.", false);
                    refreshStatus();
                    setPage("backups");
                });
            } catch (Exception e) {
                mainHandler.post(() -> setNotice(e.getMessage(), true));
            }
        }).start();
    }

    private String displayName(Uri uri) {
        try (Cursor cursor = getContentResolver().query(uri, null, null, null, null)) {
            if (cursor != null && cursor.moveToFirst()) {
                int index = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME);
                if (index >= 0) return cursor.getString(index);
            }
        } catch (Exception ignored) {}
        String path = uri.getLastPathSegment();
        return path == null ? "uploaded-backup.zip" : path;
    }

    private String readError(HttpURLConnection connection) {
        try (InputStream stream = connection.getErrorStream();
             BufferedReader reader = new BufferedReader(new InputStreamReader(stream == null ? connection.getInputStream() : stream))) {
            StringBuilder builder = new StringBuilder();
            String line;
            while ((line = reader.readLine()) != null) builder.append(line);
            return builder.length() == 0 ? "Request failed" : builder.toString();
        } catch (Exception e) {
            return e.getMessage() == null ? "Request failed" : e.getMessage();
        }
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

    private boolean eulaAlreadyAccepted() {
        try {
            return latestStatus != null && latestStatus.optBoolean("eulaAccepted", false);
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

    private JSONObject playItStatus() {
        try {
            JSONObject server = latestStatus == null ? null : latestStatus.optJSONObject("server");
            JSONObject playit = server == null ? null : server.optJSONObject("playit");
            return playit == null ? new JSONObject() : playit;
        } catch (Exception e) {
            return new JSONObject();
        }
    }

    private JSONObject playItStatus(JSONObject serverSummary) {
        if (serverSummary == null) return new JSONObject();
        JSONObject nestedServer = serverSummary.optJSONObject("server");
        JSONObject playit = nestedServer == null ? serverSummary.optJSONObject("playit") : nestedServer.optJSONObject("playit");
        return playit == null ? new JSONObject() : playit;
    }

    private boolean hasPlayItOutput(JSONObject playit) {
        if (playit == null) return false;
        return !playit.optString("claimUrl", "").isEmpty()
            || !playit.optString("address", "").isEmpty()
            || !playit.optString("error", "").isEmpty();
    }

    private void copyToClipboard(String label, String value) {
        android.content.ClipboardManager clipboard = (android.content.ClipboardManager) getSystemService(CLIPBOARD_SERVICE);
        if (clipboard != null) clipboard.setPrimaryClip(android.content.ClipData.newPlainText(label, value));
        setNotice("Copied.", false);
    }

    private void openUrl(String url) {
        try {
            startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url)));
        } catch (Exception e) {
            setNotice(e.getMessage(), true);
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
            return new DecimalFormat("#.#").format(percent) + "%";
        } catch (Exception e) {
            return "-";
        }
    }

    private String ramText() {
        return systemStorageText("memory");
    }

    private String ramPercentText() {
        try {
            JSONObject stats = latestStatus.getJSONObject("system").getJSONObject("memory");
            return new DecimalFormat("#").format(stats.optDouble("percent", 0)) + "%";
        } catch (Exception e) {
            return "-";
        }
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
        spinner.setBackground(makeBg(PANEL_ALT, STROKE, 8));
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
        edit.setBackground(makeBg(PANEL_ALT, STROKE, 8));
        return edit;
    }

    private Button tabButton(String label, String page) {
        Button button = new Button(this);
        button.setText(label);
        button.setContentDescription(screenTitle(page));
        button.setAllCaps(false);
        button.setTextSize(36);
        button.setGravity(Gravity.CENTER);
        button.setIncludeFontPadding(false);
        button.setPadding(0, 0, 0, 0);
        button.setMinWidth(0);
        button.setMinimumWidth(0);
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        button.setOnClickListener(v -> setPage(page));
        return button;
    }

    private Button primaryButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(ACCENT_TEXT);
        button.setBackground(makeBg(ACCENT, GREEN, 12));
        return button;
    }

    private Button dangerButton(String label) {
        Button button = baseButton(label);
        button.setTextColor(0xffffffff);
        button.setBackground(makeBg(DANGER_RED, DANGER_RED, 12));
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

    private Button iconActionButton(int drawableRes, int bg, int stroke, int tint, String description) {
        Button button = baseButton("");
        button.setContentDescription(description);
        button.setBackground(makeBg(bg, stroke, 12));
        Drawable icon = getDrawable(drawableRes);
        if (icon != null) {
            icon = icon.mutate();
            icon.setTint(tint);
            button.setCompoundDrawablesWithIntrinsicBounds(icon, null, null, null);
        }
        button.setGravity(Gravity.CENTER);
        button.setIncludeFontPadding(false);
        button.setCompoundDrawablePadding(0);
        button.setPadding(0, 0, 0, 0);
        return button;
    }

    private void styleTab(Button button, boolean active) {
        if (button == null) return;
        button.setTextColor(active ? 0xffffffff : MUTED);
        button.setBackground(makeBg(active ? tintFor(GREEN) : 0x00000000, active ? GREEN : 0x00000000, 16));
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
        tabs.setPadding(dp(8), dp(7), dp(8), dp(7));
        tabs.setBackground(makeBg(PANEL, STROKE, 24));
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(76));
        params.setMargins(dp(12), dp(6), dp(12), dp(14));
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
        int bg = color == GREEN ? tintFor(GREEN) : PANEL_ALT;
        int stroke = color == GREEN ? GREEN : STROKE;
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
            int lineColor = i % 2 == 0 ? color : 0xff3a3a3a;
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
        GradientDrawable drawable = new GradientDrawable(GradientDrawable.Orientation.TL_BR, new int[]{0xffffffff, 0xffdcdcdc, 0xff0db50d, 0xff070707});
        drawable.setCornerRadius(dp(8));
        return drawable;
    }

    private TextView thumbnail(String kind) {
        int[] colors;
        if ("cave".equals(kind)) colors = new int[]{0xff4a4a4a, 0xff0b0b0b};
        else if ("nether".equals(kind)) colors = new int[]{0xff2a2a2a, 0xff050505};
        else colors = new int[]{0xfff5f5f5, 0xff8e8e8e, 0xff0db50d};
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

    private ImageView loaderIconView(String loader) {
        ImageView view = new ImageView(this);
        view.setImageResource(loaderIconRes(loader));
        view.setScaleType(ImageView.ScaleType.CENTER_INSIDE);
        view.setPadding(dp(9), dp(9), dp(9), dp(9));
        view.setBackground(makeBg(ACCENT, GREEN, 16));
        return view;
    }

    private int loaderIconRes(String loader) {
        String normalized = loader == null ? "" : loader.trim().toLowerCase();
        if ("paper".equals(normalized)) return R.drawable.ic_runtime_paper;
        if ("forge".equals(normalized)) return R.drawable.ic_runtime_forge;
        if ("neoforge".equals(normalized)) return R.drawable.ic_runtime_neoforge;
        if ("quilt".equals(normalized)) return R.drawable.ic_runtime_quilt;
        if ("fabric".equals(normalized)) return R.drawable.ic_runtime_fabric;
        if ("vanilla".equals(normalized)) return R.drawable.ic_runtime_vanilla;
        return R.drawable.ic_runtime_unknown;
    }

    private String dashboardStatusLabel() {
        if (serverStopping()) return "Shutting down...";
        if (serverStarting()) return "Starting...";
        if (startingServer) return "Starting...";
        if (stoppingServer) return "Shutting down...";
        if (isOverloaded()) return "Overloaded";
        if (activeServerRunning() && serverReady()) return "Running";
        if (activeServerInstalled()) return "Ready";
        return "Offline";
    }

    private String serverStatusLabel(JSONObject server) {
        JSONObject status = server.optJSONObject("server");
        boolean running = server.optBoolean("running");
        boolean ready = status == null ? server.optBoolean("ready", false) : status.optBoolean("ready", false);
        boolean starting = status != null && status.optBoolean("starting", false);
        boolean stopping = status != null && status.optBoolean("stopping", false);
        if (stopping) return "Stopping";
        if (starting || (running && !ready)) return "Starting";
        if (running && ready) return "Running";
        return server.optBoolean("installed") ? "Ready" : "Offline";
    }

    private int serverStatusColor(JSONObject server) {
        String label = serverStatusLabel(server);
        if ("Running".equals(label) || "Starting".equals(label) || "Stopping".equals(label)) return GREEN;
        return MUTED;
    }

    private int dashboardStatusColor() {
        if (isOverloaded()) return WARNING;
        if (startingServer || stoppingServer || serverStarting() || serverStopping() || activeServerRunning()) return GREEN;
        return MUTED;
    }

    private boolean serverReady() {
        try {
            return latestStatus != null && latestStatus.getJSONObject("server").optBoolean("ready", false);
        } catch (Exception e) {
            return false;
        }
    }

    private boolean serverStarting() {
        try {
            JSONObject server = latestStatus == null ? null : latestStatus.optJSONObject("server");
            return server != null && server.optBoolean("starting", false);
        } catch (Exception e) {
            return false;
        }
    }

    private boolean serverStopping() {
        try {
            JSONObject server = latestStatus == null ? null : latestStatus.optJSONObject("server");
            return server != null && server.optBoolean("stopping", false);
        } catch (Exception e) {
            return false;
        }
    }

    private void updateDashboardLiveViews() {
        if (!"dashboard".equals(currentPage)) return;
        TextView tps = dashboardMetricViews.get("TPS");
        if (tps != null) tps.setText(tpsText() + " / 20");
        TextView players = dashboardMetricViews.get("Players");
        if (players != null) players.setText(playerCountText() + " / " + maxPlayersText());
        TextView cpu = dashboardMetricViews.get("CPU");
        if (cpu != null) cpu.setText(cpuText());
        TextView memoryView = dashboardMetricViews.get("Memory");
        if (memoryView != null) memoryView.setText(ramPercentText());
        TextView uptime = dashboardMetricViews.get("Uptime");
        if (uptime != null) uptime.setText(uptimeText());
        TextView disk = dashboardMetricViews.get("Disk");
        if (disk != null) disk.setText(serverDiskText());
        updateDashboardGraph("TPS", tpsHistory, GREEN);
        updateDashboardGraph("Players", playerHistory, ACCENT);
        updateDashboardGraph("CPU", cpuHistory, TEXT);
        updateDashboardGraph("Memory", memoryHistory, PURPLE);
    }

    private void updateDashboardGraph(String key, ArrayList<Float> history, int color) {
        LinearLayout graph = dashboardGraphViews.get(key);
        if (graph != null) populateMetricGraph(graph, history, color, 44);
    }

    private boolean isOverloaded() {
        return lowTpsSinceMs > 0 && System.currentTimeMillis() - lowTpsSinceMs >= 15 * 60 * 1000L;
    }

    private void sampleDashboardHistory(JSONObject server) {
        float tps = (float) server.optDouble("tps", 0);
        if (tps > 0) {
            addHistory(tpsHistory, tps * 2);
            if (tps < 18) {
                if (lowTpsSinceMs <= 0) lowTpsSinceMs = System.currentTimeMillis();
            } else {
                lowTpsSinceMs = 0;
            }
        }
        addHistory(playerHistory, server.optInt("players", 0) * 4f + 8f);
        try {
            JSONObject system = latestStatus.getJSONObject("system");
            addHistory(cpuHistory, (float) system.getJSONObject("cpu").optDouble("percent", 0) / 2f + 8f);
            addHistory(memoryHistory, (float) system.getJSONObject("memory").optDouble("percent", 0) / 2f + 8f);
        } catch (Exception ignored) {}
    }

    private void addHistory(ArrayList<Float> values, float value) {
        values.add(clamp(Math.round(value), 8, 42) * 1f);
        while (values.size() > 1200) values.remove(0);
    }

    private String serverDiskText() {
        try {
            JSONObject server = latestStatus.getJSONObject("server");
            long value = server.optLong("storageBytes", 0);
            if (value > 0) return bytes(value);
        } catch (Exception ignored) {}
        return activeServerInstalled() ? "0 KB" : "-";
    }

    private String loaderLabel(@Nullable JSONObject profile) {
        if (profile == null) return "Loader: -";
        String loader = profile.optString("loader", "-");
        String version = profile.optString("minecraftVersion", "-");
        return "Loader: " + loader + "  /  " + version;
    }

    private String relativeTime(String iso) {
        if (iso == null || iso.isEmpty()) return "Never";
        try {
            String normalized = iso.replace("Z", "+0000");
            java.text.SimpleDateFormat format = new java.text.SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSSZ", java.util.Locale.US);
            long then = format.parse(normalized).getTime();
            long diff = Math.max(0, System.currentTimeMillis() - then);
            long minutes = diff / 60000;
            if (minutes < 1) return "Just now";
            if (minutes < 60) return minutes + "m ago";
            long hours = minutes / 60;
            if (hours < 24) return hours + "h ago";
            long days = hours / 24;
            return days + "d ago";
        } catch (Exception e) {
            return iso;
        }
    }

    private LinearLayout progressBar(int color, int percent) {
        LinearLayout outer = row();
        outer.setBackground(makeBg(0xff2f2f2f, 0xff2f2f2f, 99));
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
        cell.setBackground(makeBg(0xff151515, STROKE, 14));
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
        card.setBackground(makeBg(PANEL, STROKE, 18));
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
        row.setBackground(makeBg(PANEL, STROKE, 16));
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

    private LinearLayout themeSettingRow() {
        LinearLayout row = settingRowIcon("◐", "Theme", "Light / Dark / System", MUTED, null);
        row.removeViewAt(row.getChildCount() - 1);
        Spinner mode = spinner(new String[]{"System", "Dark", "Light"});
        row.addView(mode, new LinearLayout.LayoutParams(dp(112), dp(46)));
        return row;
    }

    private LinearLayout themeSettingRowV2() {
        LinearLayout row = settingRowIcon("M", "Theme", "Light / Dark / System", MUTED, null);
        row.removeViewAt(row.getChildCount() - 1);
        Spinner mode = spinner(new String[]{"System", "Dark", "Light"});
        String current = preferences == null ? "System" : preferences.getString(PREF_THEME_MODE, "System");
        if ("Dark".equalsIgnoreCase(current)) mode.setSelection(1);
        else if ("Light".equalsIgnoreCase(current)) mode.setSelection(2);
        else mode.setSelection(0);
        final boolean[] ready = new boolean[]{false};
        mode.setOnItemSelectedListener(new android.widget.AdapterView.OnItemSelectedListener() {
            @Override
            public void onItemSelected(android.widget.AdapterView<?> parent, View view, int position, long id) {
                String selected = String.valueOf(parent.getItemAtPosition(position));
                if (!ready[0]) {
                    ready[0] = true;
                    return;
                }
                if (!selected.equals(themeMode)) rebuildWithTheme(selected);
            }

            @Override
            public void onNothingSelected(android.widget.AdapterView<?> parent) {
            }
        });
        row.addView(mode, new LinearLayout.LayoutParams(dp(112), dp(46)));
        return row;
    }

    private int tintFor(int color) {
        if (color == GREEN) return shouldUseDarkPalette(themeMode) ? 0xff102510 : 0xffe5f8e5;
        return PANEL_ALT;
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

    private LinearLayout.LayoutParams weightedMetricParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        params.setMargins(dp(3), dp(3), dp(3), dp(8));
        return params;
    }

    private LinearLayout.LayoutParams dashboardMetricParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(118), 1);
        params.setMargins(dp(3), dp(3), dp(3), dp(8));
        return params;
    }

    private LinearLayout.LayoutParams weightedTabParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(58), 1);
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
        if (value <= 0) return "0 KB";
        if (value > 1024L * 1024L * 1024L) return new DecimalFormat("#.# GB").format(value / 1024.0 / 1024.0 / 1024.0);
        if (value > 1024L * 1024L) return new DecimalFormat("#.# MB").format(value / 1024.0 / 1024.0);
        return new DecimalFormat("# KB").format(value / 1024.0);
    }

    private String shortDuration(long seconds) {
        if (seconds <= 0) return "0s";
        long minutes = seconds / 60;
        long hours = minutes / 60;
        if (hours > 0) return hours + "h " + (minutes % 60) + "m";
        if (minutes > 0) return minutes + "m " + (seconds % 60) + "s";
        return seconds + "s";
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private interface ResultHandler {
        void handle(String value);
    }
}
