package main

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBindAddr      = "0.0.0.0:8787"
	defaultServerID      = "main"
	appVersion           = "0.3.0-dev"
	userAgent            = "MineMux/0.1 local-mvp"
	maxLogLines          = 600
	playItPluginURL      = "https://github.com/playit-cloud/playit-minecraft-plugin/releases/latest/download/playit-minecraft-plugin.jar"
	playItPluginFileName = "playit-minecraft-plugin.jar"
	playItPackageName    = "playit"
)

var fallbackDNSServers = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}

var (
	playItClaimURLPattern = regexp.MustCompile(`https?://(?:www\.)?playit\.gg/(?:claim/[A-Za-z0-9_-]+|mc)\b[^\s<>"']*`)
	playItTunnelPattern   = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*\.gl\.(?:joinmc\.link|at\.ply\.gg)(?::[0-9]{1,5})?\b`)
)

var networkClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         mineMuxDialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	},
}

type apiError struct {
	Error string `json:"error"`
}

type app struct {
	startedAt      time.Time
	home           string
	server         *serverProcess
	playit         *playItAgentProcess
	activeMu       sync.RWMutex
	activeServerID string
	statsMu        sync.Mutex
	lastCPU        cpuSample
	backupMu       sync.Mutex
}

type appPaths struct {
	Home         string `json:"home"`
	Daemon       string `json:"daemon"`
	Runtime      string `json:"runtime"`
	Servers      string `json:"servers"`
	ServerMain   string `json:"serverMain"`
	Addons       string `json:"addons"`
	Backups      string `json:"backups"`
	Logs         string `json:"logs"`
	Diagnostics  string `json:"diagnostics"`
	Repositories string `json:"repositories"`
}

type profile struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	ServerType         string            `json:"serverType"`
	MinecraftVersion   string            `json:"minecraftVersion"`
	Loader             string            `json:"loader"`
	Seed               string            `json:"seed,omitempty"`
	IconPath           string            `json:"iconPath,omitempty"`
	JavaVersion        int               `json:"javaVersion"`
	MemoryMB           int               `json:"memoryMb"`
	MaxPlayers         int               `json:"maxPlayers"`
	ViewDistance       int               `json:"viewDistance"`
	SimulationDistance int               `json:"simulationDistance"`
	Ports              profilePorts      `json:"ports"`
	Features           profileFeatures   `json:"features"`
	Crossplay          crossplayConfig   `json:"crossplay,omitempty"`
	PlayIt             playItConfig      `json:"playit,omitempty"`
	Backups            backupPolicy      `json:"backups,omitempty"`
	Properties         map[string]string `json:"properties,omitempty"`
}

type profilePorts struct {
	JavaTCP    int `json:"javaTcp"`
	BedrockUDP int `json:"bedrockUdp"`
}

type profileFeatures struct {
	AutoStart                          bool `json:"autoStart"`
	RestartOnCrash                     bool `json:"restartOnCrash"`
	CreateRestorePointBeforeModChanges bool `json:"createRestorePointBeforeModChanges"`
}

type crossplayConfig struct {
	Enabled          bool `json:"enabled"`
	InstallGeyser    bool `json:"installGeyser"`
	InstallFloodgate bool `json:"installFloodgate"`
	Floodgate        bool `json:"floodgate"`
	BedrockUDP       int  `json:"bedrockUdp"`
}

type playItConfig struct {
	Enabled bool `json:"enabled"`
}

type backupPolicy struct {
	AutoEnabled      bool       `json:"autoEnabled"`
	IntervalMinutes  int        `json:"intervalMinutes"`
	KeepAutoBackups  int        `json:"keepAutoBackups"`
	LastAutoBackupAt *time.Time `json:"lastAutoBackupAt,omitempty"`
}

type playItStatus struct {
	Enabled        bool       `json:"enabled"`
	Detected       bool       `json:"detected"`
	Running        bool       `json:"running"`
	AgentInstalled bool       `json:"agentInstalled"`
	Mode           string     `json:"mode,omitempty"`
	ClaimURL       string     `json:"claimUrl,omitempty"`
	Address        string     `json:"address,omitempty"`
	Error          string     `json:"error,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
}

type setupRequest struct {
	ServerID                string          `json:"serverId"`
	Name                    string          `json:"name"`
	MinecraftVersion        string          `json:"minecraftVersion"`
	Loader                  string          `json:"loader"`
	Seed                    string          `json:"seed"`
	JarMode                 string          `json:"jarMode"`
	CustomJarID             string          `json:"customJarId"`
	MemoryMB                int             `json:"memoryMb"`
	MaxPlayers              int             `json:"maxPlayers"`
	ViewDistance            int             `json:"viewDistance"`
	SimulationDistance      int             `json:"simulationDistance"`
	PendingModrinthProjects []string        `json:"pendingModrinthProjects"`
	Crossplay               crossplayConfig `json:"crossplay"`
	PlayIt                  playItConfig    `json:"playit"`
	AcceptEULA              bool            `json:"acceptEula"`
}

type switchServerRequest struct {
	ServerID string `json:"serverId"`
}

type serverSummary struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Active      bool          `json:"active"`
	Installed   bool          `json:"installed"`
	Running     bool          `json:"running"`
	JoinAddress string        `json:"joinAddress"`
	LastRunAt   *time.Time    `json:"lastRunAt,omitempty"`
	Profile     *profile      `json:"profile,omitempty"`
	Server      *serverStatus `json:"server,omitempty"`
}

type loaderOption struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Supported    bool   `json:"supported"`
	TargetFolder string `json:"targetFolder"`
	Description  string `json:"description"`
}

type minecraftVersionOption struct {
	ID          string `json:"id"`
	JavaMinimum int    `json:"javaMinimum"`
	Support     string `json:"support"`
	Recommended bool   `json:"recommended"`
}

type configRequest struct {
	MemoryMB           *int    `json:"memoryMb"`
	MaxPlayers         *int    `json:"maxPlayers"`
	ViewDistance       *int    `json:"viewDistance"`
	SimulationDistance *int    `json:"simulationDistance"`
	AutoStart          *bool   `json:"autoStart"`
	RestartOnCrash     *bool   `json:"restartOnCrash"`
	MOTD               *string `json:"motd"`
	ServerPort         *int    `json:"serverPort"`
	Gamemode           *string `json:"gamemode"`
	Difficulty         *string `json:"difficulty"`
	PVP                *bool   `json:"pvp"`
	OnlineMode         *bool   `json:"onlineMode"`
	AllowFlight        *bool   `json:"allowFlight"`
	EnableCommandBlock *bool   `json:"enableCommandBlock"`
	SpawnProtection    *int    `json:"spawnProtection"`
	WhiteList          *bool   `json:"whiteList"`
	EnforceWhitelist   *bool   `json:"enforceWhitelist"`
	AutoBackupEnabled  *bool   `json:"autoBackupEnabled"`
	AutoBackupInterval *int    `json:"autoBackupIntervalMinutes"`
	AutoBackupKeep     *int    `json:"autoBackupKeep"`
}

type statusResponse struct {
	App          string       `json:"app"`
	Version      string       `json:"version"`
	StartedAt    time.Time    `json:"startedAt"`
	Paths        appPaths     `json:"paths"`
	Profile      *profile     `json:"profile,omitempty"`
	Server       serverStatus `json:"server"`
	System       systemStatus `json:"system"`
	EULAAccepted bool         `json:"eulaAccepted"`
}

type serverStatus struct {
	Installed    bool         `json:"installed"`
	Running      bool         `json:"running"`
	Ready        bool         `json:"ready"`
	Starting     bool         `json:"starting"`
	Stopping     bool         `json:"stopping"`
	PID          int          `json:"pid,omitempty"`
	StartedAt    *time.Time   `json:"startedAt,omitempty"`
	StoppedAt    *time.Time   `json:"stoppedAt,omitempty"`
	LastError    string       `json:"lastError,omitempty"`
	JoinAddress  string       `json:"joinAddress,omitempty"`
	UptimeSec    int64        `json:"uptimeSec,omitempty"`
	TPS          float64      `json:"tps,omitempty"`
	Players      int          `json:"players"`
	StorageBytes int64        `json:"storageBytes,omitempty"`
	PlayIt       playItStatus `json:"playit,omitempty"`
	WorldSeed    string       `json:"worldSeed,omitempty"`
}

type systemStatus struct {
	CPU    cpuStatus    `json:"cpu"`
	Memory memoryStatus `json:"memory"`
	Disk   diskStatus   `json:"disk"`
}

type cpuStatus struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
}

type memoryStatus struct {
	TotalBytes     uint64  `json:"totalBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	Percent        float64 `json:"percent"`
}

type diskStatus struct {
	Path           string  `json:"path"`
	TotalBytes     uint64  `json:"totalBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	Percent        float64 `json:"percent"`
}

type cpuSample struct {
	total uint64
	idle  uint64
}

type commandRequest struct {
	Command string `json:"command"`
}

type serverProcess struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	done      chan error
	startedAt *time.Time
	stoppedAt *time.Time
	lastError string
	logs      []string
	ready     bool
	stopping  bool
}

type playItAgentProcess struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan error
	startedAt *time.Time
	lastError string
	logs      []string
	starting  bool
}

type modSearchHit struct {
	ProjectID           string   `json:"project_id"`
	Slug                string   `json:"slug"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	ProjectType         string   `json:"project_type"`
	Categories          []string `json:"categories"`
	Versions            []string `json:"versions"`
	Downloads           int      `json:"downloads"`
	ClientSide          string   `json:"client_side"`
	ServerSide          string   `json:"server_side"`
	IconURL             string   `json:"icon_url"`
	LatestVersion       string   `json:"latest_version"`
	Compatible          bool     `json:"compatible"`
	CompatibilityReason string   `json:"compatibilityReason,omitempty"`
	SupportedLoaders    []string `json:"supportedLoaders,omitempty"`
}

type modSearchResponse struct {
	Hits      []modSearchHit `json:"hits"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
	TotalHits int            `json:"total_hits"`
}

type installModRequest struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
}

type modrinthVersion struct {
	ID            string               `json:"id"`
	ProjectID     string               `json:"project_id"`
	Name          string               `json:"name"`
	VersionNumber string               `json:"version_number"`
	GameVersions  []string             `json:"game_versions"`
	Loaders       []string             `json:"loaders"`
	VersionType   string               `json:"version_type"`
	Files         []modrinthFile       `json:"files"`
	Dependencies  []modrinthDependency `json:"dependencies"`
}

type modrinthFile struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type modrinthDependency struct {
	VersionID      string `json:"version_id"`
	ProjectID      string `json:"project_id"`
	DependencyType string `json:"dependency_type"`
}

type lockfile struct {
	MinecraftVersion string          `json:"minecraftVersion"`
	Loader           string          `json:"loader"`
	Installed        []lockedInstall `json:"installed"`
}

type lockedInstall struct {
	Source       string    `json:"source"`
	ProjectID    string    `json:"projectId,omitempty"`
	VersionID    string    `json:"versionId,omitempty"`
	Name         string    `json:"name,omitempty"`
	FileName     string    `json:"fileName"`
	TargetFolder string    `json:"targetFolder"`
	SHA512       string    `json:"sha512,omitempty"`
	InstalledAt  time.Time `json:"installedAt"`
	PhoneSafety  string    `json:"phoneSafety,omitempty"`
}

type backupInfo struct {
	ID        string         `json:"id"`
	Path      string         `json:"path"`
	SizeBytes int64          `json:"sizeBytes"`
	CreatedAt time.Time      `json:"createdAt"`
	Locked    bool           `json:"locked"`
	Automatic bool           `json:"automatic"`
	Meta      backupMetaItem `json:"meta,omitempty"`
}

type backupMetaStore struct {
	Backups map[string]backupMetaItem `json:"backups"`
}

type backupMetaItem struct {
	Locked    bool       `json:"locked"`
	Automatic bool       `json:"automatic"`
	ServerID  string     `json:"serverId,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

type serverFileInfo struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Directory bool      `json:"directory"`
	SizeBytes int64     `json:"sizeBytes"`
	Modified  time.Time `json:"modified"`
}

type serverFilePutRequest struct {
	Content string `json:"content"`
}

type jarDetection struct {
	FileName        string `json:"fileName"`
	DetectedType    string `json:"detectedType"`
	TargetFolder    string `json:"targetFolder"`
	Compatibility   string `json:"compatibility"`
	RestartRequired bool   `json:"restartRequired"`
}

type paperVersionsResponse struct {
	Versions []struct {
		Version struct {
			ID   string `json:"id"`
			Java struct {
				Version struct {
					Minimum int `json:"minimum"`
				} `json:"version"`
			} `json:"java"`
			Support struct {
				Status string `json:"status"`
			} `json:"support"`
		} `json:"version"`
	} `json:"versions"`
}

type paperBuild struct {
	ID        int    `json:"id"`
	Time      string `json:"time"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		Name      string            `json:"name"`
		Checksums map[string]string `json:"checksums"`
		Size      int64             `json:"size"`
		URL       string            `json:"url"`
	} `json:"downloads"`
}

type mojangVersionManifest struct {
	Latest   map[string]string `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"versions"`
}

type mojangVersionInfo struct {
	Downloads map[string]struct {
		SHA1 string `json:"sha1"`
		URL  string `json:"url"`
	} `json:"downloads"`
}

func main() {
	home, err := minemuxHome()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		startedAt:      time.Now().UTC(),
		home:           home,
		server:         &serverProcess{},
		playit:         &playItAgentProcess{},
		activeServerID: defaultServerID,
	}
	a.loadActiveServerID()
	if err := a.ensureDirs(); err != nil {
		log.Fatal(err)
	}
	a.startConfiguredServerIfNeeded()
	a.startBackupScheduler()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/app-icon.png", a.handleAppIcon)
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("GET /api/device", a.handleDevice)
	mux.HandleFunc("GET /api/server/options", a.handleServerOptions)
	mux.HandleFunc("GET /api/catalog/minecraft-versions", a.handleMinecraftVersions)
	mux.HandleFunc("GET /api/catalog/loaders", a.handleLoaders)
	mux.HandleFunc("GET /api/servers", a.handleServersList)
	mux.HandleFunc("POST /api/servers/switch", a.handleServersSwitch)
	mux.HandleFunc("DELETE /api/servers/", a.handleServersDelete)
	mux.HandleFunc("GET /api/config", a.handleConfigGet)
	mux.HandleFunc("POST /api/config", a.handleConfigPost)
	mux.HandleFunc("POST /api/server/setup", a.handleServerSetup)
	mux.HandleFunc("POST /api/server/setup/jar", a.handleServerSetupJarUpload)
	mux.HandleFunc("POST /api/server/start", a.handleServerStart)
	mux.HandleFunc("POST /api/server/stop", a.handleServerStop)
	mux.HandleFunc("POST /api/server/restart", a.handleServerRestart)
	mux.HandleFunc("GET /api/server/status", a.handleServerStatus)
	mux.HandleFunc("GET /api/server/logs", a.handleServerLogs)
	mux.HandleFunc("POST /api/server/command", a.handleServerCommand)
	mux.HandleFunc("POST /api/playit/agent/start", a.handlePlayItAgentStart)
	mux.HandleFunc("POST /api/playit/agent/stop", a.handlePlayItAgentStop)
	mux.HandleFunc("GET /api/server/players", a.handleServerPlayers)
	mux.HandleFunc("GET /api/server/files", a.handleServerFilesList)
	mux.HandleFunc("GET /api/server/file", a.handleServerFileDownload)
	mux.HandleFunc("PUT /api/server/file", a.handleServerFilePut)
	mux.HandleFunc("DELETE /api/server/file", a.handleServerFileDelete)
	mux.HandleFunc("POST /api/server/file/upload", a.handleServerFileUpload)
	mux.HandleFunc("POST /api/server/icon", a.handleServerIconUpload)
	mux.HandleFunc("POST /api/server/resource-pack", a.handleServerResourcePackUpload)
	mux.HandleFunc("GET /api/mods", a.handleModsList)
	mux.HandleFunc("GET /api/mods/search", a.handleModsSearch)
	mux.HandleFunc("POST /api/mods/install", a.handleModsInstall)
	mux.HandleFunc("POST /api/mods/uninstall", a.handleModsUninstall)
	mux.HandleFunc("POST /api/mods/upload", a.handleModsUpload)
	mux.HandleFunc("POST /api/mods/rollback", a.handleModsRollback)
	mux.HandleFunc("GET /api/backups", a.handleBackupsList)
	mux.HandleFunc("POST /api/backups/create", a.handleBackupsCreate)
	mux.HandleFunc("POST /api/backups/restore", a.handleBackupsRestore)
	mux.HandleFunc("GET /api/backups/download", a.handleBackupsDownload)
	mux.HandleFunc("POST /api/backups/upload", a.handleBackupsUpload)
	mux.HandleFunc("POST /api/backups/lock", a.handleBackupsLock)
	mux.HandleFunc("DELETE /api/backups/", a.handleBackupsDelete)
	mux.HandleFunc("GET /api/diagnostics", a.handleDiagnostics)
	mux.HandleFunc("POST /api/diagnostics/export", a.handleDiagnosticsExport)

	addr := getenv("MINEMUX_ADDR", getenv("CRAFTNODE_ADDR", defaultBindAddr))
	server := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("minemux daemon %s listening on http://%s", appVersion, listener.Addr())
	log.Printf("minemux home: %s", home)
	log.Fatal(server.Serve(listener))
}

func minemuxHome() (string, error) {
	if value := os.Getenv("MINEMUX_HOME"); value != "" {
		return value, nil
	}
	if value := os.Getenv("CRAFTNODE_HOME"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "minemux"), nil
}

func (a *app) paths() appPaths {
	return appPaths{
		Home:         a.home,
		Daemon:       filepath.Join(a.home, "daemon"),
		Runtime:      filepath.Join(a.home, "runtime"),
		Servers:      filepath.Join(a.home, "servers"),
		ServerMain:   a.activeServerDir(),
		Addons:       filepath.Join(a.home, "addons"),
		Backups:      filepath.Join(a.home, "backups"),
		Logs:         filepath.Join(a.home, "logs"),
		Diagnostics:  filepath.Join(a.home, "diagnostics"),
		Repositories: filepath.Join(a.home, "repositories"),
	}
}

func (a *app) ensureDirs() error {
	p := a.paths()
	for _, dir := range []string{p.Daemon, p.Runtime, p.Servers, p.Addons, p.Backups, p.Logs, p.Diagnostics, p.Repositories} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) activeStatePath() string {
	return filepath.Join(a.home, "servers", "active.json")
}

func (a *app) activeServerDir() string {
	return a.serverDir(a.activeServerIDValue())
}

func (a *app) activeServerIDValue() string {
	a.activeMu.RLock()
	defer a.activeMu.RUnlock()
	if a.activeServerID == "" {
		return defaultServerID
	}
	return a.activeServerID
}

func (a *app) serverDir(id string) string {
	return filepath.Join(a.home, "servers", normalizeServerID(id))
}

func (a *app) loadActiveServerID() {
	var state struct {
		ActiveServerID string `json:"activeServerId"`
	}
	if err := readJSONFile(a.activeStatePath(), &state); err != nil {
		return
	}
	id := normalizeServerID(state.ActiveServerID)
	a.activeMu.Lock()
	a.activeServerID = id
	a.activeMu.Unlock()
}

func (a *app) setActiveServerID(id string) error {
	id = normalizeServerID(id)
	if err := writeJSONFile(a.activeStatePath(), map[string]string{"activeServerId": id}); err != nil {
		return err
	}
	a.activeMu.Lock()
	a.activeServerID = id
	a.activeMu.Unlock()
	return nil
}

func (a *app) profilePathFor(id string) string {
	return filepath.Join(a.serverDir(id), "profile.json")
}

func (a *app) eulaStatePath() string {
	return filepath.Join(a.home, "eula-accepted")
}

func (a *app) globalEULAAccepted() bool {
	_, err := os.Stat(a.eulaStatePath())
	return err == nil
}

func (a *app) markGlobalEULAAccepted() {
	_ = os.WriteFile(a.eulaStatePath(), []byte("true\n"), 0o644)
}

func (a *app) loadProfileFor(id string) (*profile, error) {
	var p profile
	if err := readJSONFile(a.profilePathFor(id), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (a *app) profilePath() string {
	return filepath.Join(a.paths().ServerMain, "profile.json")
}

func (a *app) lockPath() string {
	return filepath.Join(a.paths().ServerMain, "minemux.lock")
}

func (a *app) lockPathFor(id string) string {
	return filepath.Join(a.serverDir(id), "minemux.lock")
}

func (a *app) serverJarPath() string {
	return filepath.Join(a.paths().ServerMain, "server.jar")
}

func (a *app) loadProfile() (*profile, error) {
	var p profile
	if err := readJSONFile(a.profilePath(), &p); err != nil {
		return nil, err
	}
	normalizeBackupPolicy(&p)
	return &p, nil
}

func (a *app) saveProfile(p *profile) error {
	normalizeBackupPolicy(p)
	return writeJSONFile(a.profilePath(), p)
}

func (a *app) saveProfileFor(id string, p *profile) error {
	id = normalizeServerID(id)
	if err := os.MkdirAll(a.serverDir(id), 0o755); err != nil {
		return err
	}
	normalizeBackupPolicy(p)
	return writeJSONFile(a.profilePathFor(id), p)
}

func (a *app) defaultProfile(mcVersion string, javaVersion int) profile {
	if mcVersion == "" {
		mcVersion = "latest-compatible"
	}
	return profile{
		ID:                 a.activeServerIDValue(),
		Name:               "Main Server",
		ServerType:         "paper",
		MinecraftVersion:   mcVersion,
		Loader:             "paper",
		JavaVersion:        javaVersion,
		MemoryMB:           2048,
		MaxPlayers:         8,
		ViewDistance:       6,
		SimulationDistance: 4,
		Ports: profilePorts{
			JavaTCP:    25565,
			BedrockUDP: 19132,
		},
		Features: profileFeatures{
			AutoStart:                          false,
			RestartOnCrash:                     true,
			CreateRestorePointBeforeModChanges: true,
		},
		Backups: backupPolicy{
			AutoEnabled:     false,
			IntervalMinutes: 360,
			KeepAutoBackups: 5,
		},
	}
}

func normalizeBackupPolicy(p *profile) {
	if p == nil {
		return
	}
	if p.Backups.IntervalMinutes <= 0 {
		p.Backups.IntervalMinutes = 360
	}
	p.Backups.IntervalMinutes = clampInt(p.Backups.IntervalMinutes, 15, 10080)
	if p.Backups.KeepAutoBackups <= 0 {
		p.Backups.KeepAutoBackups = 5
	}
	p.Backups.KeepAutoBackups = clampInt(p.Backups.KeepAutoBackups, 1, 100)
}

func (a *app) status() statusResponse {
	p, _ := a.loadProfile()
	return statusResponse{
		App:          "minemux-daemon",
		Version:      appVersion,
		StartedAt:    a.startedAt,
		Paths:        a.paths(),
		Profile:      p,
		Server:       a.server.status(a),
		System:       a.systemStatus(),
		EULAAccepted: a.globalEULAAccepted(),
	}
}

func (a *app) startConfiguredServerIfNeeded() {
	p, err := a.loadProfile()
	if err != nil || p == nil || !p.Features.AutoStart {
		return
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if err := a.server.start(context.Background(), a, p); err != nil {
			a.server.mu.Lock()
			a.server.lastError = "auto-start failed: " + err.Error()
			a.server.mu.Unlock()
		}
	}()
}

func (a *app) startBackupScheduler() {
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		for {
			<-timer.C
			a.runAutoBackupIfDue()
			timer.Reset(60 * time.Second)
		}
	}()
}

func (a *app) runAutoBackupIfDue() {
	p, err := a.loadProfile()
	if err != nil || p == nil {
		return
	}
	normalizeBackupPolicy(p)
	if !p.Backups.AutoEnabled || !a.serverInstalled(p) {
		return
	}
	now := time.Now().UTC()
	interval := time.Duration(p.Backups.IntervalMinutes) * time.Minute
	if p.Backups.LastAutoBackupAt != nil && now.Sub(*p.Backups.LastAutoBackupAt) < interval {
		return
	}
	a.server.mu.Lock()
	a.server.appendLogLocked("Creating scheduled backup")
	a.server.mu.Unlock()
	if _, err := a.createBackup("auto"); err != nil {
		a.server.mu.Lock()
		a.server.appendLogLocked("Scheduled backup failed: " + err.Error())
		a.server.mu.Unlock()
		return
	}
	p.Backups.LastAutoBackupAt = &now
	_ = a.saveProfile(p)
}

func (a *app) systemStatus() systemStatus {
	return systemStatus{
		CPU:    a.cpuStatus(),
		Memory: memoryStatusFromProc(),
		Disk:   diskStatusForPath(a.home),
	}
}

func (a *app) cpuStatus() cpuStatus {
	status := cpuStatus{Cores: runtime.NumCPU()}
	if topPercent, err := readCPUPercentFromTop(); err == nil && topPercent > 0 {
		status.Percent = topPercent
		return status
	}
	sample, err := readCPUSample()
	if err != nil || sample.total == 0 {
		return status
	}
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	if a.lastCPU.total > 0 && sample.total > a.lastCPU.total {
		totalDelta := sample.total - a.lastCPU.total
		idleDelta := sample.idle - a.lastCPU.idle
		if totalDelta > 0 && totalDelta >= idleDelta {
			status.Percent = clampFloat(float64(totalDelta-idleDelta)*100/float64(totalDelta), 0, 100)
		}
	}
	a.lastCPU = sample
	return status
}

func readCPUPercentFromTop() (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	cmd := exec.CommandContext(ctx, "top", "-b", "-n", "1")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil || len(out) == 0 {
		ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		cmd = exec.CommandContext(ctx, "top", "-n", "1")
		out, err = cmd.CombinedOutput()
	}
	if err != nil {
		return 0, err
	}
	text := string(out)
	if percent, ok := parseTopProcessCPUPercent(text, runtime.NumCPU()); ok {
		return clampFloat(percent, 0, 100), nil
	}
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "%cpu") || strings.Contains(lower, "cpu:") || strings.Contains(lower, "%cpu(s)") || strings.Contains(lower, "%cpu(s):") || strings.Contains(lower, "%cpu") || strings.Contains(lower, "cpu(s):") {
			if percent, ok := parseTopCPUPercent(lower); ok {
				return clampFloat(percent, 0, 100), nil
			}
		}
	}
	return 0, errors.New("top output did not include CPU summary")
}

func parseTopProcessCPUPercent(output string, cores int) (float64, bool) {
	if cores <= 0 {
		cores = 1
	}
	taskCount := 0
	taskRe := regexp.MustCompile(`(?i)tasks:\s*([0-9]+)\s+total`)
	headerSeen := false
	processRows := 0
	total := 0.0

	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		if match := taskRe.FindStringSubmatch(lower); len(match) == 2 {
			taskCount, _ = strconv.Atoi(match[1])
		}
		if strings.Contains(lower, "pid") && strings.Contains(lower, "%cpu") {
			headerSeen = true
			processRows = 0
			total = 0
			continue
		}
		if !headerSeen || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !allDigits(fields[0]) {
			continue
		}
		if value, ok := cpuValueFromTopProcessFields(fields); ok {
			total += value
			processRows++
			if taskCount > 0 && processRows >= taskCount {
				break
			}
		}
	}
	if processRows == 0 {
		return 0, false
	}
	return total / float64(cores), true
}

func cpuValueFromTopProcessFields(fields []string) (float64, bool) {
	states := map[string]bool{"R": true, "S": true, "D": true, "T": true, "Z": true, "I": true}
	for i := 1; i < len(fields)-1; i++ {
		if states[strings.ToUpper(fields[i])] {
			value, err := strconv.ParseFloat(strings.TrimSuffix(fields[i+1], "%"), 64)
			return value, err == nil
		}
	}
	return 0, false
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseTopCPUPercent(line string) (float64, bool) {
	if idx := strings.Index(line, " id"); idx > 0 {
		before := strings.TrimSpace(line[:idx])
		fields := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(before, ",", " "), "%", " "))
		if len(fields) > 0 {
			if idle, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
				return 100 - idle, true
			}
		}
	}
	re := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%?\s*(usr|user|us|sys|system|sy|nice|nic)`)
	matches := re.FindAllStringSubmatch(line, -1)
	if len(matches) > 0 {
		total := 0.0
		for _, match := range matches {
			value, _ := strconv.ParseFloat(match[1], 64)
			total += value
		}
		return total, true
	}
	return 0, false
}

func readCPUSample() (cpuSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		return cpuSample{}, errors.New("empty /proc/stat")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, errors.New("unexpected /proc/stat format")
	}
	var values []uint64
	for _, raw := range fields[1:] {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cpuSample{}, err
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuSample{total: total, idle: idle}, nil
}

func memoryStatusFromProc() memoryStatus {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return memoryStatus{}
	}
	values := map[string]uint64{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	used := uint64(0)
	percent := float64(0)
	if total > available {
		used = total - available
	}
	if total > 0 {
		percent = clampFloat(float64(used)*100/float64(total), 0, 100)
	}
	return memoryStatus{TotalBytes: total, AvailableBytes: available, UsedBytes: used, Percent: percent}
}

func diskStatusForPath(path string) diskStatus {
	out, err := exec.Command("df", "-k", path).Output()
	if err != nil {
		return diskStatus{Path: path}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return diskStatus{Path: path}
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return diskStatus{Path: path}
	}
	totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
	usedKB, _ := strconv.ParseUint(fields[2], 10, 64)
	availableKB, _ := strconv.ParseUint(fields[3], 10, 64)
	total := totalKB * 1024
	used := usedKB * 1024
	available := availableKB * 1024
	percent := float64(0)
	if total > 0 {
		percent = clampFloat(float64(used)*100/float64(total), 0, 100)
	}
	return diskStatus{Path: path, TotalBytes: total, AvailableBytes: available, UsedBytes: used, Percent: percent}
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (s *serverProcess) start(ctx context.Context, a *app, p *profile) error {
	return s.startWithOptions(ctx, a, p, false)
}

func (s *serverProcess) startWithOptions(ctx context.Context, a *app, p *profile, bypassCooldown bool) error {
	s.mu.Lock()
	alreadyRunning := s.cmd != nil && s.cmd.Process != nil
	coolingDown := !bypassCooldown && s.stoppedAt != nil && time.Since(*s.stoppedAt) < 30*time.Second
	s.mu.Unlock()

	if alreadyRunning {
		return errors.New("server is already running")
	}
	if coolingDown {
		return errors.New("server just stopped; wait a few seconds before starting it again")
	}
	if p == nil {
		return errors.New("server profile is missing; run setup first")
	}
	if err := a.ensurePlayItPluginBeforeStart(p); err != nil {
		return err
	}

	s.mu.Lock()

	if s.cmd != nil && s.cmd.Process != nil {
		s.mu.Unlock()
		return errors.New("server is already running")
	}
	if !bypassCooldown && s.stoppedAt != nil && time.Since(*s.stoppedAt) < 30*time.Second {
		s.mu.Unlock()
		return errors.New("server just stopped; wait a few seconds before starting it again")
	}
	if p == nil {
		s.mu.Unlock()
		return errors.New("server profile is missing; run setup first")
	}
	command, args, err := a.startCommand(p)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if ok, err := eulaAccepted(filepath.Join(a.paths().ServerMain, "eula.txt")); err != nil || !ok {
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return errors.New("Minecraft EULA is not accepted")
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = a.paths().ServerMain
	cmd.Env = cleanJavaEnv(os.Environ())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return err
	}

	now := time.Now().UTC()
	done := make(chan error, 1)
	s.cmd = cmd
	s.stdin = stdin
	s.done = done
	s.startedAt = &now
	s.stoppedAt = nil
	s.lastError = ""
	s.ready = false
	s.stopping = false
	s.appendLogLocked(fmt.Sprintf("%s server started with pid %d", p.Loader, cmd.Process.Pid))

	go s.capture(stdout)
	go s.capture(stderr)
	go s.wait(cmd, done)
	s.mu.Unlock()

	return nil
}

func (a *app) ensurePlayItPluginBeforeStart(p *profile) error {
	if p == nil || !p.PlayIt.Enabled || !strings.EqualFold(p.Loader, "paper") {
		return nil
	}
	targetPath := filepath.Join(a.paths().ServerMain, "plugins", playItPluginFileName)
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.appendSetupLog("PlayIt enabled but plugin is missing; installing before server start")
	return a.installPlayItPlugin(p)
}

func (a *app) startCommand(p *profile) (string, []string, error) {
	loader := strings.ToLower(strings.TrimSpace(p.Loader))
	switch loader {
	case "forge", "neoforge":
		if _, err := os.Stat(filepath.Join(a.paths().ServerMain, "run.sh")); err == nil {
			_ = os.WriteFile(filepath.Join(a.paths().ServerMain, "user_jvm_args.txt"), []byte(fmt.Sprintf("-Xms%dM\n-Xmx%dM\n", minInt(p.MemoryMB, 1024), p.MemoryMB)), 0o644)
			return "sh", []string{"run.sh", "nogui"}, nil
		}
	case "quilt":
		if _, err := os.Stat(filepath.Join(a.paths().ServerMain, "quilt-server-launch.jar")); err == nil {
			return "java", javaJarArgs(p, "quilt-server-launch.jar"), nil
		}
	case "fabric":
		if _, err := os.Stat(filepath.Join(a.paths().ServerMain, "fabric-server-launch.jar")); err == nil {
			return "java", javaJarArgs(p, "fabric-server-launch.jar"), nil
		}
		if _, err := os.Stat(filepath.Join(a.paths().ServerMain, "fabric-server-launcher.jar")); err == nil {
			return "java", javaJarArgs(p, "fabric-server-launcher.jar"), nil
		}
	}
	return "java", javaJarArgs(p, "server.jar"), nil
}

func javaJarArgs(p *profile, jar string) []string {
	args := []string{
		"-Dterminal.jline=false",
		"-Dterminal.ansi=false",
		"-Djna.nosys=true",
	}
	if p != nil && p.PlayIt.Enabled && strings.EqualFold(p.Loader, "paper") {
		args = append(args,
			"-Djava.net.preferIPv4Stack=true",
			"-Djava.net.preferIPv6Addresses=false",
		)
	}
	memoryMB := 1024
	if p != nil && p.MemoryMB > 0 {
		memoryMB = p.MemoryMB
	}
	args = append(args,
		fmt.Sprintf("-Xms%dM", minInt(memoryMB, 1024)),
		fmt.Sprintf("-Xmx%dM", memoryMB),
		"-jar",
		jar,
		"nogui",
	)
	return args
}

func (s *serverProcess) stop(timeout time.Duration) error {
	return s.stopWithOptions(timeout, false)
}

func (s *serverProcess) stopWithOptions(timeout time.Duration, allowDuringStartup bool) error {
	s.mu.Lock()
	cmd := s.cmd
	stdin := s.stdin
	done := s.done
	startedAt := s.startedAt
	ready := s.ready
	if cmd != nil && cmd.Process != nil {
		if !allowDuringStartup && startedAt != nil && !ready && time.Since(*startedAt) < 30*time.Second {
			s.mu.Unlock()
			return errors.New("server is still starting; wait until startup finishes before stopping")
		}
		s.stopping = true
	}
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return errors.New("server is not running")
	}
	if stdin != nil {
		_, _ = io.WriteString(stdin, "stop\n")
	}

	select {
	case <-time.After(timeout):
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
		<-done
		return nil
	case err := <-done:
		return err
	}
}

func (s *serverProcess) restart(ctx context.Context, a *app, p *profile) error {
	if err := s.stopWithOptions(30*time.Second, true); err != nil && err.Error() != "server is not running" {
		return err
	}
	return s.startWithOptions(ctx, a, p, true)
}

func (s *serverProcess) send(command string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin == nil {
		return errors.New("server is not running")
	}
	_, err := io.WriteString(s.stdin, command+"\n")
	if err == nil {
		s.appendLogLocked("> " + command)
	}
	return err
}

func (s *serverProcess) status(a *app) serverStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, _ := a.loadProfile()
	lines := dedupeTailLines(append(tailTextFile(filepath.Join(a.paths().ServerMain, "logs", "latest.log"), maxLogLines), s.logs...), maxLogLines)
	status := serverStatus{
		Installed: a.serverInstalled(p),
		Running:   s.cmd != nil && s.cmd.Process != nil,
		StartedAt: s.startedAt,
		StoppedAt: s.stoppedAt,
		LastError: s.lastError,
	}
	if status.Running {
		status.PID = s.cmd.Process.Pid
		if s.startedAt != nil {
			status.UptimeSec = int64(time.Since(*s.startedAt).Seconds())
		}
		status.TPS = 20
	}
	status.Ready = status.Running && s.ready
	status.Starting = status.Running && !s.ready && !s.stopping
	status.Stopping = status.Running && s.stopping
	if status.Installed {
		status.StorageBytes = directorySize(a.paths().ServerMain)
	}
	if status.Running && s.ready {
		status.Players = countPlayersFromLogs(lines)
	}
	port := 25565
	if p != nil && p.Ports.JavaTCP > 0 {
		port = p.Ports.JavaTCP
	}
	status.JoinAddress = localJoinAddress(port)
	status.PlayIt = a.playItStatus(p, lines)
	status.WorldSeed = worldSeedFromProfileAndLogs(p, lines)
	return status
}

func (a *app) playItStatus(p *profile, serverLines []string) playItStatus {
	lines := append([]string{}, serverLines...)
	lines = append(lines, a.playit.logLines()...)
	status := playItStatusFromLogs(p, lines)
	status.Mode = "plugin"
	agentRunning, agentStartedAt, errorText := a.playit.state(status.Error)
	status.Running = agentRunning
	status.StartedAt = agentStartedAt
	status.Error = errorText
	if agentRunning || agentStartedAt != nil {
		status.Mode = "agent-fallback"
	}
	status.AgentInstalled = playItExecutablePath() != ""
	return status
}

func worldSeedFromProfileAndLogs(p *profile, lines []string) string {
	seed := ""
	if p != nil {
		seed = strings.TrimSpace(p.Seed)
		if seed == "" && p.Properties != nil {
			seed = strings.TrimSpace(p.Properties["level-seed"])
		}
	}
	for _, line := range lines {
		if found := seedFromLogLine(line); found != "" {
			seed = found
		}
	}
	return seed
}

func seedFromLogLine(line string) string {
	lower := strings.ToLower(line)
	index := strings.Index(lower, "seed:")
	if index < 0 {
		return ""
	}
	value := strings.TrimSpace(line[index+len("seed:"):])
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	value = strings.Trim(value, " \t\r\n.,;")
	if value == "" {
		return ""
	}
	for _, r := range value {
		if (r < '0' || r > '9') && r != '-' {
			return ""
		}
	}
	return value
}

func playItStatusFromLogs(p *profile, lines []string) playItStatus {
	status := playItStatus{}
	if p != nil {
		status.Enabled = p.PlayIt.Enabled
	}
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "playit") {
			status.Detected = true
		}
		if playItSecretVerified(lower) && strings.Contains(strings.ToLower(status.Error), "claim") {
			status.Error = ""
		}
		if err := playItErrorFromLogLine(lower); err != "" {
			status.Error = err
			status.Detected = true
		}
		if status.ClaimURL == "" {
			if url := firstPlayItClaimURL(line); url != "" {
				status.ClaimURL = url
				status.Detected = true
			}
		}
		if status.Address == "" {
			if address := firstPlayItTunnelAddress(line); address != "" {
				status.Address = address
				status.Error = ""
				status.Detected = true
			}
		}
	}
	return status
}

func playItSecretVerified(lower string) bool {
	return strings.Contains(lower, "secret verified") ||
		strings.Contains(lower, "keys setup complete") ||
		strings.Contains(lower, "ready to connect")
}

func playItErrorFromLogLine(lower string) string {
	switch {
	case strings.Contains(lower, "unsupportedaddresstypeexception"):
		return "PlayIt hit an Android IPv6 socket issue. Set the PlayIt agent/tunnel to IPv4 only; use the fallback agent only if the plugin still fails."
	case strings.Contains(lower, "address family not supported") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "failed to send initial ping") ||
		strings.Contains(lower, "failed to reload_control_addr"):
		return "PlayIt agent could not connect to tunnel control servers. Check mobile data/Wi-Fi, VPN, private DNS, and retry."
	case strings.Contains(lower, "failed to get control addresses") ||
		strings.Contains(lower, "failed when communicating with tunnel server"):
		return "PlayIt could not reach its tunnel control servers. Check mobile data/Wi-Fi, VPN, private DNS, and try again."
	case strings.Contains(lower, "api error code: 400") && strings.Contains(lower, "codenotfound"):
		return "PlayIt claim code was not accepted yet or expired. Open the latest claim link and try again."
	default:
		return ""
	}
}

func firstPlayItClaimURL(line string) string {
	return cleanPlayItMatch(playItClaimURLPattern.FindString(line))
}

func firstPlayItTunnelAddress(line string) string {
	return cleanPlayItMatch(playItTunnelPattern.FindString(line))
}

func cleanPlayItMatch(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ".,);]}>\"'")
	return value
}

func (a *app) serverInstalled(p *profile) bool {
	return serverDirInstalled(a.paths().ServerMain, p)
}

func serverDirInstalled(dir string, p *profile) bool {
	loader := ""
	if p != nil {
		loader = strings.ToLower(strings.TrimSpace(p.Loader))
	}
	switch loader {
	case "forge", "neoforge":
		_, err := os.Stat(filepath.Join(dir, "run.sh"))
		return err == nil
	case "quilt":
		_, err := os.Stat(filepath.Join(dir, "quilt-server-launch.jar"))
		return err == nil
	case "fabric":
		if _, err := os.Stat(filepath.Join(dir, "fabric-server-launch.jar")); err == nil {
			return true
		}
		_, err := os.Stat(filepath.Join(dir, "fabric-server-launcher.jar"))
		return err == nil
	default:
		_, err := os.Stat(filepath.Join(dir, "server.jar"))
		return err == nil
	}
}

func countPlayersFromLogs(lines []string) int {
	return len(playersFromLogs(lines))
}

func playersFromLogs(lines []string) []string {
	online := map[string]bool{}
	for _, line := range lines {
		if strings.Contains(line, " joined the game") {
			name := playerNameFromLog(line, " joined the game")
			if name != "" {
				online[name] = true
			}
		} else if strings.Contains(line, " left the game") {
			name := playerNameFromLog(line, " left the game")
			if name != "" {
				delete(online, name)
			}
		}
	}
	players := make([]string, 0, len(online))
	for name := range online {
		players = append(players, name)
	}
	sort.Strings(players)
	return players
}

func playerNameFromLog(line, suffix string) string {
	idx := strings.Index(line, suffix)
	if idx <= 0 {
		return ""
	}
	before := strings.TrimSpace(line[:idx])
	parts := strings.Fields(before)
	if len(parts) == 0 {
		return ""
	}
	return strings.Trim(parts[len(parts)-1], "<>[]:")
}

func (s *serverProcess) logLines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, len(s.logs))
	copy(out, s.logs)
	return out
}

func (a *app) mergedServerLogLines(limit int) []string {
	lines := append([]string{}, tailTextFile(filepath.Join(a.paths().ServerMain, "logs", "latest.log"), limit)...)
	lines = append(lines, a.server.logLines()...)
	lines = append(lines, a.playit.logLines()...)
	if limit <= 0 {
		limit = maxLogLines
	}
	return dedupeTailLines(lines, limit)
}

func (a *app) playItDir() string {
	return filepath.Join(a.home, "playit")
}

func (a *app) playItSecretPath() string {
	return filepath.Join(a.playItDir(), "playit.toml")
}

func (a *app) startPlayItAgentForProfile(p *profile) {
	if p == nil || !p.PlayIt.Enabled {
		return
	}
	if err := a.playit.start(context.Background(), a); err != nil {
		a.playit.setError(err.Error())
		a.server.mu.Lock()
		a.server.appendLogLocked("PlayIt agent failed: " + err.Error())
		a.server.mu.Unlock()
	}
}

func (p *playItAgentProcess) start(ctx context.Context, a *app) error {
	p.mu.Lock()
	if p.cmd != nil && p.cmd.Process != nil || p.starting {
		p.mu.Unlock()
		return nil
	}
	p.starting = true
	p.lastError = ""
	p.mu.Unlock()
	startFailed := true
	defer func() {
		if startFailed {
			p.mu.Lock()
			p.starting = false
			p.mu.Unlock()
		}
	}()

	if err := a.ensurePlayItAgentInstalled(func(line string) {
		p.appendLog("install: " + line)
	}); err != nil {
		return err
	}
	executable := playItExecutablePath()
	if executable == "" {
		return errors.New("playit-cli is not installed; open Terminal and run: pkg install tur-repo playit")
	}
	if err := os.MkdirAll(a.playItDir(), 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, executable, "-s")
	cmd.Dir = a.playItDir()
	cmd.Env = append(cleanJavaEnv(os.Environ()),
		"PLAYIT_SECRET_PATH="+a.playItSecretPath(),
		"XDG_CONFIG_HOME="+filepath.Join(a.home, ".config"),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	now := time.Now().UTC()
	done := make(chan error, 1)
	p.mu.Lock()
	p.cmd = cmd
	p.done = done
	p.startedAt = &now
	p.starting = false
	p.appendLogLocked("PlayIt agent started with pid " + strconv.Itoa(cmd.Process.Pid))
	p.mu.Unlock()
	startFailed = false

	go p.capture(stdout)
	go p.capture(stderr)
	go p.wait(cmd, done)
	return nil
}

func (p *playItAgentProcess) stop(timeout time.Duration) error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	select {
	case <-time.After(timeout):
		return errors.New("timed out stopping PlayIt agent")
	case err := <-done:
		return err
	}
}

func (p *playItAgentProcess) capture(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		p.appendLog(line)
	}
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "file already closed") {
		p.setError(err.Error())
		p.appendLog("log capture error: " + err.Error())
	}
}

func (p *playItAgentProcess) wait(cmd *exec.Cmd, done chan error) {
	err := cmd.Wait()
	done <- err
	close(done)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != cmd {
		return
	}
	p.cmd = nil
	p.done = nil
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "signal: killed") {
			p.lastError = ""
			p.appendLogLocked("PlayIt agent stopped")
			return
		}
		p.lastError = err.Error()
		p.appendLogLocked("PlayIt agent stopped with error: " + err.Error())
		return
	}
	p.appendLogLocked("PlayIt agent stopped")
}

func (p *playItAgentProcess) logLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.logs))
	copy(out, p.logs)
	return out
}

func (p *playItAgentProcess) state(existingError string) (bool, *time.Time, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	running := p.starting || (p.cmd != nil && p.cmd.Process != nil)
	startedAt := p.startedAt
	err := existingError
	if p.lastError != "" {
		err = p.lastError
	}
	return running, startedAt, err
}

func (p *playItAgentProcess) appendLog(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.appendLogLocked(line)
}

func (p *playItAgentProcess) setError(err string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastError = err
}

func (p *playItAgentProcess) appendLogLocked(line string) {
	entry := time.Now().UTC().Format(time.RFC3339) + " [PlayIt] " + line
	p.logs = append(p.logs, entry)
	if len(p.logs) > maxLogLines {
		p.logs = p.logs[len(p.logs)-maxLogLines:]
	}
}

func (a *app) ensurePlayItAgentInstalled(logLine func(string)) error {
	if playItExecutablePath() != "" {
		return nil
	}
	if err := ensureTermuxDNSConfigured(); err != nil && logLine != nil {
		logLine("Could not update Termux DNS resolver before PlayIt install: " + err.Error())
	}
	if _, err := exec.LookPath("pkg"); err != nil {
		return errors.New("Termux pkg is unavailable; open Terminal and run: pkg install tur-repo playit")
	}
	for _, args := range [][]string{
		{"install", "-y", "tur-repo"},
		{"install", "-y", playItPackageName},
	} {
		if logLine != nil {
			logLine("Running pkg " + strings.Join(args, " "))
		}
		cmd := exec.Command("pkg", args...)
		cmd.Env = cleanJavaEnv(os.Environ())
		if err := runCommandStreaming(cmd, logLine); err != nil && logLine != nil {
			logLine("pkg " + strings.Join(args, " ") + " failed: " + err.Error())
		}
	}
	if playItExecutablePath() == "" {
		return errors.New("could not install PlayIt agent; open Terminal and run: pkg install tur-repo playit")
	}
	return nil
}

func playItExecutablePath() string {
	for _, name := range []string{"playit-cli", "playit"} {
		if path, err := exec.LookPath(name); err == nil && path != "" {
			return path
		}
	}
	for _, path := range []string{
		"/data/data/com.termux/files/usr/bin/playit-cli",
		"/data/data/com.termux/files/usr/bin/playit",
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func tailTextFile(path string, limit int) []string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return []string{}
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func dedupeTailLines(lines []string, limit int) []string {
	start := 0
	if limit > 0 && len(lines) > limit*2 {
		start = len(lines) - limit*2
	}
	seen := map[string]bool{}
	out := []string{}
	for _, line := range lines[start:] {
		key := strings.TrimSpace(line)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (s *serverProcess) capture(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		s.mu.Lock()
		s.appendLogLocked(scanner.Text())
		s.mu.Unlock()
	}
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "file already closed") {
		s.mu.Lock()
		s.lastError = err.Error()
		s.appendLogLocked("log capture error: " + err.Error())
		s.mu.Unlock()
	}
}

func (s *serverProcess) wait(cmd *exec.Cmd, done chan error) {
	err := cmd.Wait()
	done <- err
	close(done)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != cmd {
		return
	}
	now := time.Now().UTC()
	s.stoppedAt = &now
	s.stdin = nil
	s.done = nil
	s.cmd = nil
	s.ready = false
	s.stopping = false
	if err != nil {
		s.lastError = err.Error()
		s.appendLogLocked("Paper server stopped with error: " + err.Error())
		return
	}
	s.appendLogLocked("Paper server stopped")
}

func (s *serverProcess) appendLogLocked(line string) {
	entry := time.Now().UTC().Format(time.RFC3339) + " " + line
	s.logs = append(s.logs, entry)
	lower := strings.ToLower(line)
	if strings.Contains(lower, "for help, type") || (strings.Contains(lower, "done (") && strings.Contains(lower, "s)!")) {
		s.ready = true
		s.stopping = false
	}
	if len(s.logs) > maxLogLines {
		s.logs = s.logs[len(s.logs)-maxLogLines:]
	}
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, apiError{Error: "not found"})
		return
	}
	for _, indexPath := range []string{
		os.Getenv("MINEMUX_WEBUI_INDEX"),
		os.Getenv("CRAFTNODE_WEBUI_INDEX"),
		filepath.Join(a.home, "webui", "index.html"),
		filepath.Join("..", "webui", "index.html"),
	} {
		if indexPath == "" {
			continue
		}
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, apiError{Error: "web UI not found"})
}

func (a *app) handleAppIcon(w http.ResponseWriter, r *http.Request) {
	for _, iconPath := range []string{
		filepath.Join(a.home, "webui", "app-icon.png"),
		filepath.Join("..", "webui", "app-icon.png"),
	} {
		if _, err := os.Stat(iconPath); err == nil {
			w.Header().Set("Content-Type", "image/png")
			http.ServeFile(w, r, iconPath)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, apiError{Error: "app icon not found"})
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.status())
}

func (a *app) handleDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.deviceDiagnostics())
}

func (a *app) handleServerOptions(w http.ResponseWriter, r *http.Request) {
	versions, source, warning := minecraftVersionOptions(detectJavaMajor())
	writeJSON(w, http.StatusOK, map[string]any{
		"minecraftVersions": versions,
		"loaders":           loaderOptions(),
		"source":            source,
		"warning":           warning,
	})
}

func (a *app) handleMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	javaMajor := detectJavaMajor()
	if value := strings.TrimSpace(r.URL.Query().Get("javaMajor")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			javaMajor = parsed
		}
	}
	versions, source, warning := minecraftVersionOptions(javaMajor)
	writeJSON(w, http.StatusOK, map[string]any{
		"versions":  versions,
		"javaMajor": javaMajor,
		"source":    source,
		"warning":   warning,
	})
}

func (a *app) handleLoaders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"loaders": loaderOptions()})
}

func (a *app) handleServersList(w http.ResponseWriter, r *http.Request) {
	servers, err := a.listServers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"activeServerId": a.activeServerIDValue(),
		"servers":        servers,
	})
}

func (a *app) handleServersSwitch(w http.ResponseWriter, r *http.Request) {
	var req switchServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	id := normalizeServerID(req.ServerID)
	if a.server.status(a).Running {
		writeJSON(w, http.StatusConflict, apiError{Error: "stop the running server before switching profiles"})
		return
	}
	_ = a.playit.stop(5 * time.Second)
	if _, err := os.Stat(a.profilePathFor(id)); err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "server profile not found"})
		return
	}
	if err := a.setActiveServerID(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, a.status())
}

func (a *app) handleServersDelete(w http.ResponseWriter, r *http.Request) {
	id := normalizeServerID(strings.TrimPrefix(r.URL.Path, "/api/servers/"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "server id is required"})
		return
	}
	activeID := a.activeServerIDValue()
	if id == activeID && a.server.status(a).Running {
		writeJSON(w, http.StatusConflict, apiError{Error: "stop the running server before deleting it"})
		return
	}
	if id == activeID {
		_ = a.playit.stop(5 * time.Second)
	}
	dir := a.serverDir(id)
	if _, err := os.Stat(dir); err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "server not found"})
		return
	}
	p, _ := a.loadProfileFor(id)
	if err := os.RemoveAll(dir); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	deletedBackups := a.deleteBackupsForServer(id, p)
	if id == activeID {
		nextID := a.firstExistingServerID(defaultServerID)
		if err := a.setActiveServerID(nextID); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
	}
	servers, _ := a.listServers()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id, "deletedBackups": deletedBackups, "servers": servers})
}

func (a *app) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	id := normalizeServerID(r.URL.Query().Get("serverId"))
	if strings.TrimSpace(r.URL.Query().Get("serverId")) == "" {
		id = a.activeServerIDValue()
	}
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *app) handleConfigPost(w http.ResponseWriter, r *http.Request) {
	id := normalizeServerID(r.URL.Query().Get("serverId"))
	if strings.TrimSpace(r.URL.Query().Get("serverId")) == "" {
		id = a.activeServerIDValue()
	}
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	var req configRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	if req.MemoryMB != nil {
		p.MemoryMB = clampInt(*req.MemoryMB, 512, 8192)
	}
	if req.MaxPlayers != nil {
		p.MaxPlayers = clampInt(*req.MaxPlayers, 1, 50)
	}
	if req.ViewDistance != nil {
		p.ViewDistance = clampInt(*req.ViewDistance, 2, 32)
	}
	if req.SimulationDistance != nil {
		p.SimulationDistance = clampInt(*req.SimulationDistance, 2, 32)
	}
	if req.AutoStart != nil {
		p.Features.AutoStart = *req.AutoStart
	}
	if req.RestartOnCrash != nil {
		p.Features.RestartOnCrash = *req.RestartOnCrash
	}
	if req.ServerPort != nil {
		p.Ports.JavaTCP = clampInt(*req.ServerPort, 1024, 65535)
	}
	if p.Properties == nil {
		p.Properties = map[string]string{}
	}
	if req.MOTD != nil {
		p.Properties["motd"] = strings.TrimSpace(*req.MOTD)
	}
	if p.Seed != "" {
		p.Properties["level-seed"] = p.Seed
	}
	if req.Gamemode != nil {
		p.Properties["gamemode"] = enumString(*req.Gamemode, "survival", []string{"survival", "creative", "adventure", "spectator"})
	}
	if req.Difficulty != nil {
		p.Properties["difficulty"] = enumString(*req.Difficulty, "normal", []string{"peaceful", "easy", "normal", "hard"})
	}
	if req.PVP != nil {
		p.Properties["pvp"] = strconv.FormatBool(*req.PVP)
	}
	if req.OnlineMode != nil {
		p.Properties["online-mode"] = strconv.FormatBool(*req.OnlineMode)
	}
	if req.AllowFlight != nil {
		p.Properties["allow-flight"] = strconv.FormatBool(*req.AllowFlight)
	}
	if req.EnableCommandBlock != nil {
		p.Properties["enable-command-block"] = strconv.FormatBool(*req.EnableCommandBlock)
	}
	if req.SpawnProtection != nil {
		p.Properties["spawn-protection"] = strconv.Itoa(clampInt(*req.SpawnProtection, 0, 64))
	}
	if req.WhiteList != nil {
		p.Properties["white-list"] = strconv.FormatBool(*req.WhiteList)
	}
	if req.EnforceWhitelist != nil {
		p.Properties["enforce-whitelist"] = strconv.FormatBool(*req.EnforceWhitelist)
	}
	if req.AutoBackupEnabled != nil {
		p.Backups.AutoEnabled = *req.AutoBackupEnabled
	}
	if req.AutoBackupInterval != nil {
		p.Backups.IntervalMinutes = clampInt(*req.AutoBackupInterval, 15, 10080)
	}
	if req.AutoBackupKeep != nil {
		p.Backups.KeepAutoBackups = clampInt(*req.AutoBackupKeep, 1, 100)
	}
	if err := a.saveProfileFor(id, p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := writeServerProperties(a.serverDir(id), p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *app) handleServerSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	req.AcceptEULA = req.AcceptEULA || a.globalEULAAccepted()
	requestedServerID := a.activeServerIDValue()
	if strings.TrimSpace(req.ServerID) != "" {
		requestedServerID = normalizeServerID(req.ServerID)
	}
	if requestedServerID != a.activeServerIDValue() {
		if a.server.status(a).Running {
			writeJSON(w, http.StatusConflict, apiError{Error: "stop the running server before setting up another profile"})
			return
		}
		if err := a.setActiveServerID(requestedServerID); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
	}
	loader := strings.ToLower(strings.TrimSpace(req.Loader))
	if loader == "" {
		loader = "paper"
	}
	if !supportedLoader(loader) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "unsupported server runtime: " + loader})
		return
	}
	a.server.mu.Lock()
	a.server.appendLogLocked("Checking Termux DNS resolver")
	a.server.mu.Unlock()
	if err := ensureTermuxDNSConfigured(); err != nil {
		log.Printf("could not update Termux DNS resolver: %v", err)
		a.server.mu.Lock()
		a.server.appendLogLocked("Could not update DNS resolver: " + err.Error())
		a.server.mu.Unlock()
	} else {
		a.server.mu.Lock()
		a.server.appendLogLocked("Termux DNS resolver ready")
		a.server.mu.Unlock()
	}
	javaMajor := detectJavaMajor()
	if javaMajor <= 0 {
		a.server.mu.Lock()
		a.server.appendLogLocked("Java not found; installing OpenJDK package")
		a.server.mu.Unlock()
		if err := ensureJavaInstalledWithLog(a.appendSetupLog); err != nil {
			writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
			return
		}
		javaMajor = detectJavaMajor()
		if javaMajor <= 0 {
			writeJSON(w, http.StatusBadGateway, apiError{Error: "OpenJDK install finished but java is still unavailable"})
			return
		}
	}
	p := a.defaultProfile(req.MinecraftVersion, javaMajor)
	p.ID = requestedServerID
	if strings.TrimSpace(req.Name) != "" {
		p.Name = strings.TrimSpace(req.Name)
	} else if requestedServerID != defaultServerID {
		p.Name = titleFromServerID(requestedServerID)
	}
	p.Loader = loader
	p.ServerType = loader
	p.Seed = strings.TrimSpace(req.Seed)
	if req.MemoryMB > 0 {
		p.MemoryMB = clampInt(req.MemoryMB, 1024, maxInt(1024, systemMemoryMB()*80/100))
	}
	if req.MaxPlayers > 0 {
		p.MaxPlayers = clampInt(req.MaxPlayers, 1, 50)
	}
	if req.ViewDistance > 0 {
		p.ViewDistance = clampInt(req.ViewDistance, 2, 32)
	}
	if req.SimulationDistance > 0 {
		p.SimulationDistance = clampInt(req.SimulationDistance, 2, 32)
	}
	if err := os.MkdirAll(a.paths().ServerMain, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	p.Crossplay = req.Crossplay
	if p.Crossplay.BedrockUDP <= 0 {
		p.Crossplay.BedrockUDP = 19132
	}
	p.PlayIt = req.PlayIt
	if !strings.EqualFold(p.Loader, "paper") {
		p.PlayIt.Enabled = false
	}
	if p.Properties == nil {
		p.Properties = map[string]string{}
	}
	if p.Seed != "" {
		p.Properties["level-seed"] = p.Seed
	}

	resolvedVersion, err := a.installSetupRuntime(&p, javaMajor, req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	p.MinecraftVersion = resolvedVersion
	a.server.mu.Lock()
	a.server.appendLogLocked("Writing server profile for Minecraft " + resolvedVersion)
	a.server.mu.Unlock()
	if err := a.saveProfile(&p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := writeServerProperties(a.paths().ServerMain, &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := writeEULA(filepath.Join(a.paths().ServerMain, "eula.txt"), req.AcceptEULA); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if req.AcceptEULA {
		a.markGlobalEULAAccepted()
	}
	a.server.mu.Lock()
	a.server.appendLogLocked("Server setup complete")
	a.server.mu.Unlock()
	if err := a.ensureLockfile(&p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	a.installSetupAddons(&p, req)
	writeJSON(w, http.StatusOK, a.status())
}

func (a *app) installSetupRuntime(p *profile, javaMajor int, req setupRequest) (string, error) {
	if strings.EqualFold(req.JarMode, "custom") && strings.TrimSpace(req.CustomJarID) != "" {
		source := filepath.Join(a.paths().Runtime, "setup-uploads", filepath.Base(req.CustomJarID)+".jar")
		if _, err := os.Stat(source); err != nil {
			return "", errors.New("custom setup jar is missing; upload it again")
		}
		a.appendSetupLog("Installing uploaded custom server.jar")
		if err := copyFile(source, a.serverJarPath()); err != nil {
			return "", err
		}
		_ = os.Remove(source)
		if p.MinecraftVersion == "" || p.MinecraftVersion == "latest-compatible" {
			p.MinecraftVersion = latestFallbackMinecraftVersion(javaMajor)
		}
		return p.MinecraftVersion, nil
	}
	return a.installServerRuntime(p, javaMajor)
}

func (a *app) installSetupAddons(p *profile, req setupRequest) {
	projects := append([]string{}, req.PendingModrinthProjects...)
	if req.Crossplay.Enabled {
		if req.Crossplay.InstallGeyser {
			projects = append(projects, "geyser")
		}
		if req.Crossplay.InstallFloodgate || req.Crossplay.Floodgate {
			projects = append(projects, "floodgate")
		}
	}
	if req.PlayIt.Enabled && strings.EqualFold(p.Loader, "paper") {
		if err := a.installPlayItPlugin(p); err != nil {
			a.appendSetupLog("PlayIt plugin install failed: " + err.Error())
			a.appendSetupLog("Standalone PlayIt agent can be used as fallback from the dashboard if needed.")
		}
		a.appendSetupLog("If PlayIt shows IPv6/control-channel errors on Android, set the PlayIt agent/tunnel to IPv4 only in the PlayIt dashboard.")
		if req.Crossplay.Enabled {
			a.appendSetupLog("PlayIt plugin handles Java TCP. Bedrock UDP needs a separate PlayIt agent/tunnel path.")
		}
	} else if req.PlayIt.Enabled {
		a.appendSetupLog("PlayIt plugin setup skipped: the built-in plugin is currently available for Paper/Spigot-style servers.")
	}
	seen := map[string]bool{}
	for _, project := range projects {
		project = strings.TrimSpace(project)
		if project == "" || seen[project] {
			continue
		}
		seen[project] = true
		a.appendSetupLog("Installing add-on: " + project)
		if _, err := a.installModrinthProject(p, project, "", map[string]bool{}); err != nil {
			a.appendSetupLog("Add-on skipped: " + project + " - " + err.Error())
		}
	}
}

func (a *app) installPlayItPlugin(p *profile) error {
	if !strings.EqualFold(p.Loader, "paper") {
		return fmt.Errorf("the PlayIt Minecraft plugin only loads on Paper/Bukkit servers; %s needs the standalone PlayIt agent", p.Loader)
	}
	targetDir := filepath.Join(a.paths().ServerMain, "plugins")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	targetPath := filepath.Join(targetDir, playItPluginFileName)
	a.appendSetupLog("Downloading PlayIt plugin from GitHub")
	if err := downloadFile(playItPluginURL, targetPath); err != nil {
		_ = os.Remove(targetPath)
		return err
	}
	lock, _ := a.loadLockfile()
	lock.MinecraftVersion = p.MinecraftVersion
	lock.Loader = p.Loader
	lock.Installed = upsertInstall(lock.Installed, lockedInstall{
		Source:       "github",
		ProjectID:    "playit-minecraft-plugin",
		Name:         "PlayIt Minecraft Plugin",
		FileName:     playItPluginFileName,
		TargetFolder: "plugins",
		InstalledAt:  time.Now().UTC(),
	})
	if err := a.saveLockfile(lock); err != nil {
		return err
	}
	a.appendSetupLog("Installed PlayIt plugin to plugins/" + playItPluginFileName)
	return nil
}

func (a *app) handleServerSetupJarUpload(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".jar") {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "only .jar uploads are supported"})
		return
	}
	dir := filepath.Join(a.paths().Runtime, "setup-uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	id := safeName(strings.TrimSuffix(name, filepath.Ext(name))) + "-" + strconv.FormatInt(time.Now().Unix(), 10)
	target := filepath.Join(dir, id+".jar")
	out, err := os.Create(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"customJarId": id, "fileName": name})
}

func (a *app) handleServerStart(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if javaMajor := detectJavaMajor(); javaMajor > 0 && p.JavaVersion != javaMajor {
		p.JavaVersion = javaMajor
		_ = a.saveProfile(p)
	}
	if err := a.server.start(context.Background(), a, p); err != nil {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, a.server.status(a))
}

func (a *app) handleServerStop(w http.ResponseWriter, r *http.Request) {
	if err := a.server.stop(30 * time.Second); err != nil {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error()})
		return
	}
	_ = a.playit.stop(5 * time.Second)
	writeJSON(w, http.StatusAccepted, a.server.status(a))
}

func (a *app) handleServerRestart(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if javaMajor := detectJavaMajor(); javaMajor > 0 && p.JavaVersion != javaMajor {
		p.JavaVersion = javaMajor
		_ = a.saveProfile(p)
	}
	if err := a.server.restart(context.Background(), a, p); err != nil {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, a.server.status(a))
}

func (a *app) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.server.status(a))
}

func (a *app) handleServerLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]string{"lines": a.mergedServerLogLines(maxLogLines)})
}

func (a *app) handleServerCommand(w http.ResponseWriter, r *http.Request) {
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "command is required"})
		return
	}
	if err := a.server.send(req.Command); err != nil {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}

func (a *app) handlePlayItAgentStart(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if !p.PlayIt.Enabled {
		writeJSON(w, http.StatusConflict, apiError{Error: "PlayIt is not enabled for this server"})
		return
	}
	go a.startPlayItAgentForProfile(p)
	writeJSON(w, http.StatusAccepted, a.server.status(a))
}

func (a *app) handlePlayItAgentStop(w http.ResponseWriter, r *http.Request) {
	if err := a.playit.stop(5 * time.Second); err != nil {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, a.server.status(a))
}

func (a *app) handleServerPlayers(w http.ResponseWriter, r *http.Request) {
	status := a.server.status(a)
	if !status.Running || !status.Ready {
		writeJSON(w, http.StatusOK, map[string]any{"online": 0, "players": []string{}})
		return
	}
	players := playersFromLogs(a.mergedServerLogLines(maxLogLines))
	writeJSON(w, http.StatusOK, map[string]any{"online": len(players), "players": players})
}

func (a *app) handleServerFilesList(w http.ResponseWriter, r *http.Request) {
	target, rel, err := a.safeServerPathFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	files := make([]serverFileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		childRel := strings.TrimPrefix(filepath.ToSlash(filepath.Join(rel, entry.Name())), "/")
		files = append(files, serverFileInfo{
			Name:      entry.Name(),
			Path:      childRel,
			Directory: entry.IsDir(),
			SizeBytes: info.Size(),
			Modified:  info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Directory != files[j].Directory {
			return files[i].Directory
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "files": files})
}

func (a *app) handleServerFileDownload(w http.ResponseWriter, r *http.Request) {
	target, rel, err := a.safeServerPathFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "file not found"})
		return
	}
	if info.IsDir() {
		name := filepath.Base(target)
		if rel == "" {
			name = a.activeServerIDValue()
		}
		tmp, err := os.CreateTemp(a.paths().Runtime, safeName(name)+"-*.zip")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		tmpPath := tmp.Name()
		_ = tmp.Close()
		defer os.Remove(tmpPath)
		if err := zipDirectory(target, tmpPath, nil); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(safeName(name)+".zip"))
		http.ServeFile(w, r, tmpPath)
		return
	}
	if r.URL.Query().Get("raw") == "1" {
		data, err := os.ReadFile(target)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"content": string(data)})
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(target)))
	http.ServeFile(w, r, target)
}

func (a *app) handleServerFilePut(w http.ResponseWriter, r *http.Request) {
	target, _, err := a.safeServerPathFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	var req serverFilePutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := os.WriteFile(target, []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (a *app) handleServerFileDelete(w http.ResponseWriter, r *http.Request) {
	target, _, err := a.safeServerPathFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	if target == a.paths().ServerMain {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "cannot delete server root"})
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *app) handleServerFileUpload(w http.ResponseWriter, r *http.Request) {
	id := serverIDFromRequest(a, r)
	dir, _, err := a.safeServerPathFor(id, r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "missing file upload"})
		return
	}
	defer file.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	target := filepath.Join(dir, filepath.Base(header.Filename))
	out, err := os.Create(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	root := a.serverDir(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "path": filepath.ToSlash(strings.TrimPrefix(target, root+string(os.PathSeparator)))})
}

func (a *app) safeServerPathFromRequest(r *http.Request) (string, string, error) {
	return a.safeServerPathFor(serverIDFromRequest(a, r), r.URL.Query().Get("path"))
}

func serverIDFromRequest(a *app, r *http.Request) string {
	if strings.TrimSpace(r.URL.Query().Get("serverId")) == "" {
		return a.activeServerIDValue()
	}
	return normalizeServerID(r.URL.Query().Get("serverId"))
}

func (a *app) handleServerIconUpload(w http.ResponseWriter, r *http.Request) {
	id := normalizeServerID(r.URL.Query().Get("serverId"))
	if strings.TrimSpace(r.URL.Query().Get("serverId")) == "" {
		id = a.activeServerIDValue()
	}
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "server profile not found"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	name := strings.ToLower(filepath.Base(header.Filename))
	if !strings.HasSuffix(name, ".png") {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "server icons must be PNG files"})
		return
	}
	target := filepath.Join(a.serverDir(id), "server-icon.png")
	out, err := os.Create(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := out.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	p.IconPath = "server-icon.png"
	if err := a.saveProfileFor(id, p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "iconPath": p.IconPath})
}

func (a *app) handleServerResourcePackUpload(w http.ResponseWriter, r *http.Request) {
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	name := strings.ToLower(filepath.Base(header.Filename))
	if !strings.HasSuffix(name, ".zip") {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "resource packs must be .zip files"})
		return
	}
	target := filepath.Join(a.serverDir(id), "resource-pack.zip")
	out, err := os.Create(target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := out.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	sha, _ := sha1File(target)
	if p.Properties == nil {
		p.Properties = map[string]string{}
	}
	p.Properties["resource-pack"] = "http://" + localIP() + ":8787/api/server/file?path=resource-pack.zip"
	p.Properties["resource-pack-sha1"] = sha
	p.Properties["resource-pack-prompt"] = "MineMux server resource pack"
	if err := a.saveProfileFor(id, p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := writeServerProperties(a.serverDir(id), p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "sha1": sha})
}

func (a *app) handleModsList(w http.ResponseWriter, r *http.Request) {
	id := normalizeServerID(r.URL.Query().Get("serverId"))
	if strings.TrimSpace(r.URL.Query().Get("serverId")) == "" {
		id = a.activeServerIDValue()
	}
	lock, err := a.loadLockfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusOK, lockfile{Installed: []lockedInstall{}})
		return
	}
	writeJSON(w, http.StatusOK, lock)
}

func (a *app) handleModsSearch(w http.ResponseWriter, r *http.Request) {
	id := normalizeServerID(r.URL.Query().Get("serverId"))
	var p *profile
	var err error
	if strings.TrimSpace(r.URL.Query().Get("serverId")) != "" {
		p, err = a.loadProfileFor(id)
	} else {
		p, err = a.loadProfile()
	}
	if err != nil {
		loader := strings.TrimSpace(r.URL.Query().Get("loader"))
		version := strings.TrimSpace(r.URL.Query().Get("minecraftVersion"))
		if loader == "" || version == "" {
			writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
			return
		}
		p = &profile{Loader: strings.ToLower(loader), MinecraftVersion: version}
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "query is required"})
		return
	}
	res, err := searchModrinth(query, p)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *app) handleModsInstall(w http.ResponseWriter, r *http.Request) {
	var req installModRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	id := normalizeServerID(r.URL.Query().Get("serverId"))
	if strings.TrimSpace(r.URL.Query().Get("serverId")) == "" {
		id = a.activeServerIDValue()
	}
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if req.ProjectID == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "projectId is required"})
		return
	}
	if p.Features.CreateRestorePointBeforeModChanges {
		if _, err := a.createBackupFor(id, "before-mod-install"); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
			return
		}
	}
	installed, err := a.installModrinthProjectFor(id, p, req.ProjectID, req.VersionID, map[string]bool{})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": installed})
}

func (a *app) handleModsUninstall(w http.ResponseWriter, r *http.Request) {
	fileName := strings.TrimSpace(r.URL.Query().Get("fileName"))
	if fileName == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "fileName query parameter is required"})
		return
	}
	id := serverIDFromRequest(a, r)
	lock, err := a.loadLockfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "lockfile not found"})
		return
	}
	next := lock.Installed[:0]
	removed := false
	for _, item := range lock.Installed {
		if item.FileName == fileName {
			path := filepath.Join(a.serverDir(id), item.TargetFolder, item.FileName)
			_ = os.Remove(path)
			removed = true
			continue
		}
		next = append(next, item)
	}
	lock.Installed = next
	if err := a.saveLockfileFor(id, lock); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": removed})
}

func (a *app) handleModsUpload(w http.ResponseWriter, r *http.Request) {
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if p.Features.CreateRestorePointBeforeModChanges {
		if _, err := a.createBackupFor(id, "before-jar-upload"); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	detection, item, err := a.saveUploadedJarFor(id, p, file, header)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	lock, _ := a.loadLockfileFor(id)
	lock.Installed = upsertInstall(lock.Installed, item)
	if err := a.saveLockfileFor(id, lock); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"detection": detection, "installed": item})
}

func (a *app) handleModsRollback(w http.ResponseWriter, r *http.Request) {
	backups, err := a.listBackups()
	if err != nil || len(backups) == 0 {
		writeJSON(w, http.StatusNotFound, apiError{Error: "no backup available"})
		return
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt.After(backups[j].CreatedAt) })
	if err := a.restoreBackup(backups[0].ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, backups[0])
}

func (a *app) handleBackupsList(w http.ResponseWriter, r *http.Request) {
	backups, err := a.listBackups()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("serverId")) != "" {
		id := normalizeServerID(r.URL.Query().Get("serverId"))
		filtered := backups[:0]
		for _, backup := range backups {
			if backup.Meta.ServerID == id {
				filtered = append(filtered, backup)
			}
		}
		backups = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": backups})
}

func (a *app) handleBackupsCreate(w http.ResponseWriter, r *http.Request) {
	info, err := a.createBackupFor(serverIDFromRequest(a, r), "manual")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *app) handleBackupsRestore(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "id query parameter is required"})
		return
	}
	if err := a.restoreBackup(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"restored": id})
}

func (a *app) handleBackupsDownload(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(strings.TrimSpace(r.URL.Query().Get("id")))
	if id == "" || id == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "id query parameter is required"})
		return
	}
	path := filepath.Join(a.paths().Backups, id+".zip")
	if _, err := os.Stat(path); err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.zip"`)
	http.ServeFile(w, r, path)
}

func (a *app) handleBackupsUpload(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	info, err := a.importBackup(file, header)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	if r.URL.Query().Get("restore") == "true" {
		if err := a.restoreBackup(info.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"imported": info, "restored": info.ID})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *app) handleBackupsLock(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(strings.TrimSpace(r.URL.Query().Get("id")))
	if id == "" || id == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "id query parameter is required"})
		return
	}
	var req struct {
		Locked bool `json:"locked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	if _, err := os.Stat(filepath.Join(a.paths().Backups, id+".zip")); err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error()})
		return
	}
	meta, err := a.loadBackupMeta()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	item := meta.Backups[id]
	item.Locked = req.Locked
	if item.Reason == "" {
		item.Reason = backupReasonFromID(id)
	}
	meta.Backups[id] = item
	if err := a.saveBackupMeta(meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	backups, _ := a.listBackups()
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "locked": req.Locked, "backups": backups})
}

func (a *app) handleBackupsDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/backups/")
	id = filepath.Base(id)
	if id == "" || id == "." {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "backup id is required"})
		return
	}
	if err := os.Remove(filepath.Join(a.paths().Backups, id+".zip")); err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error()})
		return
	}
	_ = a.removeBackupMeta(id)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (a *app) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.diagnostics())
}

func (a *app) handleDiagnosticsExport(w http.ResponseWriter, r *http.Request) {
	path, err := a.exportDiagnostics()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (a *app) listServers() ([]serverSummary, error) {
	serversRoot := filepath.Join(a.home, "servers")
	if err := os.MkdirAll(serversRoot, 0o755); err != nil {
		return nil, err
	}
	activeID := a.activeServerIDValue()
	seen := map[string]bool{}
	ids := []string{activeID, defaultServerID}
	entries, err := os.ReadDir(serversRoot)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ids = append(ids, normalizeServerID(entry.Name()))
	}
	summaries := make([]serverSummary, 0, len(ids))
	for _, rawID := range ids {
		id := normalizeServerID(rawID)
		if seen[id] {
			continue
		}
		seen[id] = true
		dir := a.serverDir(id)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		var p *profile
		var loaded profile
		if err := readJSONFile(filepath.Join(dir, "profile.json"), &loaded); err == nil {
			p = &loaded
		} else {
			continue
		}
		installed := serverDirInstalled(dir, p)
		name := titleFromServerID(id)
		port := 25565
		if p != nil {
			name = p.Name
			if p.Ports.JavaTCP > 0 {
				port = p.Ports.JavaTCP
			}
		}
		summary := serverSummary{
			ID:          id,
			Name:        name,
			Active:      id == activeID,
			Installed:   installed,
			Running:     false,
			JoinAddress: localJoinAddress(port),
			LastRunAt:   lastRunAtForServer(dir),
			Profile:     p,
		}
		if id == activeID {
			status := a.server.status(a)
			summary.Running = status.Running
			summary.Installed = status.Installed
			summary.JoinAddress = status.JoinAddress
			summary.Server = &status
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Active != summaries[j].Active {
			return summaries[i].Active
		}
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func lastRunAtForServer(dir string) *time.Time {
	for _, relative := range []string{"logs/latest.log", "world/session.lock", "server.properties", "profile.json"} {
		info, err := os.Stat(filepath.Join(dir, relative))
		if err == nil {
			t := info.ModTime()
			return &t
		}
	}
	return nil
}

func directorySize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func (a *app) safeServerPath(raw string) (string, string, error) {
	return a.safeServerPathFor(a.activeServerIDValue(), raw)
}

func (a *app) safeServerPathFor(id, raw string) (string, string, error) {
	root, err := filepath.Abs(a.serverDir(id))
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(strings.TrimPrefix(strings.ReplaceAll(raw, "\\", "/"), "/"))
	if cleaned == "." {
		cleaned = ""
	}
	target, err := filepath.Abs(filepath.Join(root, cleaned))
	if err != nil {
		return "", "", err
	}
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", "", errors.New("path escapes server directory")
	}
	rel, _ := filepath.Rel(root, target)
	if rel == "." {
		rel = ""
	}
	return target, filepath.ToSlash(rel), nil
}

func (a *app) firstExistingServerID(fallback string) string {
	servers, err := a.listServers()
	if err != nil {
		return fallback
	}
	for _, server := range servers {
		if server.ID != "" {
			return server.ID
		}
	}
	return fallback
}

func (a *app) deleteBackupsForServer(id string, p *profile) int {
	names := []string{safeName(id)}
	if p != nil && strings.TrimSpace(p.Name) != "" {
		names = append(names, safeName(p.Name))
	}
	deleted := 0
	entries, err := os.ReadDir(a.paths().Backups)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		backupName := strings.TrimSuffix(entry.Name(), ".zip")
		for _, name := range names {
			if name != "" && strings.HasPrefix(backupName, name+"-") {
				if err := os.Remove(filepath.Join(a.paths().Backups, entry.Name())); err == nil {
					_ = a.removeBackupMeta(backupName)
					deleted++
				}
				break
			}
		}
	}
	return deleted
}

func loaderOptions() []loaderOption {
	return []loaderOption{
		{
			ID:           "paper",
			Name:         "Paper",
			Kind:         "plugin",
			Supported:    true,
			TargetFolder: "plugins",
			Description:  "Recommended phone-friendly server with Bukkit/Paper plugins.",
		},
		{
			ID:           "vanilla",
			Name:         "Vanilla",
			Kind:         "server",
			Supported:    true,
			TargetFolder: "",
			Description:  "Official Mojang server without plugin or mod loader.",
		},
		{
			ID:           "quilt",
			Name:         "Quilt",
			Kind:         "mod",
			Supported:    true,
			TargetFolder: "mods",
			Description:  "Lightweight mod loader installed through the Quilt installer.",
		},
		{
			ID:           "fabric",
			Name:         "Fabric",
			Kind:         "mod",
			Supported:    true,
			TargetFolder: "mods",
			Description:  "Lightweight mod loader with broad server mod support.",
		},
		{
			ID:           "forge",
			Name:         "Forge",
			Kind:         "mod",
			Supported:    true,
			TargetFolder: "mods",
			Description:  "Classic mod loader installed through the Forge installer.",
		},
		{
			ID:           "neoforge",
			Name:         "NeoForge",
			Kind:         "mod",
			Supported:    true,
			TargetFolder: "mods",
			Description:  "Modern Forge-family loader installed through the NeoForge installer.",
		},
	}
}

func supportedLoader(loader string) bool {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "paper", "vanilla", "quilt", "fabric", "forge", "neoforge":
		return true
	default:
		return false
	}
}

func (a *app) installServerRuntime(p *profile, javaMajor int) (string, error) {
	loader := strings.ToLower(strings.TrimSpace(p.Loader))
	switch loader {
	case "vanilla":
		a.appendSetupLog("Resolving Vanilla server download")
		version, jarURL, sha1sum, err := resolveVanillaDownload(p.MinecraftVersion)
		if err != nil {
			return "", err
		}
		a.appendSetupLog("Downloading Vanilla server.jar")
		return version, downloadFileWithSHA1(jarURL, a.serverJarPath(), sha1sum)
	case "quilt":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestFallbackMinecraftVersion(javaMajor)
		}
		a.appendSetupLog("Downloading Quilt installer")
		installer, err := latestMavenInstaller("https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer/maven-metadata.xml", "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer")
		if err != nil {
			return "", err
		}
		installerPath := filepath.Join(a.paths().ServerMain, "quilt-installer.jar")
		if err := downloadFile(installer, installerPath); err != nil {
			return "", err
		}
		a.appendSetupLog("Running Quilt server installer")
		if err := a.runInstaller("java", "-jar", installerPath, "install", "server", version, "--download-server", "--install-dir="+a.paths().ServerMain); err != nil {
			return "", err
		}
		return version, nil
	case "fabric":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestFallbackMinecraftVersion(javaMajor)
		}
		a.appendSetupLog("Downloading Fabric installer")
		installer, err := latestMavenInstaller("https://maven.fabricmc.net/net/fabricmc/fabric-installer/maven-metadata.xml", "https://maven.fabricmc.net/net/fabricmc/fabric-installer")
		if err != nil {
			return "", err
		}
		installerPath := filepath.Join(a.paths().ServerMain, "fabric-installer.jar")
		if err := downloadFile(installer, installerPath); err != nil {
			return "", err
		}
		a.appendSetupLog("Running Fabric server installer")
		if err := a.runInstaller("java", "-jar", installerPath, "server", "-mcversion", version, "-downloadMinecraft", "-dir", a.paths().ServerMain); err != nil {
			return "", err
		}
		return version, nil
	case "forge":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestFallbackMinecraftVersion(javaMajor)
		}
		a.appendSetupLog("Resolving Forge installer")
		installer, resolved, err := resolveForgeInstaller(version)
		if err != nil {
			return "", err
		}
		return resolved, a.installForgeFamily("Forge", installer)
	case "neoforge":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestFallbackMinecraftVersion(javaMajor)
		}
		a.appendSetupLog("Resolving NeoForge installer")
		installer, resolved, err := resolveNeoForgeInstaller(version)
		if err != nil {
			return "", err
		}
		return resolved, a.installForgeFamily("NeoForge", installer)
	default:
		a.appendSetupLog("Resolving Paper version and download")
		version, jarURL, sha, err := resolvePaperDownload(p.MinecraftVersion, javaMajor)
		if err != nil {
			return "", err
		}
		a.appendSetupLog("Downloading Paper server.jar")
		return version, downloadFileWithSHA256(jarURL, a.serverJarPath(), sha)
	}
}

func (a *app) installForgeFamily(name, installerURL string) error {
	installerPath := filepath.Join(a.paths().ServerMain, strings.ToLower(name)+"-installer.jar")
	a.appendSetupLog("Downloading " + name + " installer")
	if err := downloadFile(installerURL, installerPath); err != nil {
		return err
	}
	a.appendSetupLog("Running " + name + " server installer")
	if err := a.runInstaller("java", "-jar", installerPath, "--installServer"); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(a.paths().ServerMain, "user_jvm_args.txt"), []byte("# MineMux writes memory limits before start\n"), 0o644)
	return nil
}

func (a *app) appendSetupLog(line string) {
	a.server.mu.Lock()
	a.server.appendLogLocked(line)
	a.server.mu.Unlock()
}

func (a *app) runInstaller(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = a.paths().ServerMain
	cmd.Env = cleanJavaEnv(os.Environ())
	return runCommandStreaming(cmd, a.appendSetupLog)
}

func minecraftVersionOptions(javaMajor int) ([]minecraftVersionOption, string, string) {
	versions, err := fetchMojangReleaseVersions(javaMajor)
	if err == nil && len(versions) > 0 {
		return versions, "mojang", ""
	}
	mojangErr := err
	versions, err = fetchPaperMinecraftVersions(javaMajor)
	if err == nil && len(versions) > 0 {
		return versions, "papermc", ""
	}
	warning := ""
	if mojangErr != nil {
		warning = mojangErr.Error()
	} else if err != nil {
		warning = err.Error()
	}
	return fallbackMinecraftVersions(javaMajor), "fallback", warning
}

func fetchMojangReleaseVersions(javaMajor int) ([]minecraftVersionOption, error) {
	var manifest mojangVersionManifest
	if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		return nil, err
	}
	options := make([]minecraftVersionOption, 0, len(manifest.Versions))
	recommendedSet := false
	for _, entry := range manifest.Versions {
		if entry.ID == "" || entry.Type != "release" {
			continue
		}
		minimum := javaMinimumForMinecraft(entry.ID)
		if javaMajor > 0 && minimum > javaMajor {
			continue
		}
		options = append(options, minecraftVersionOption{
			ID:          entry.ID,
			JavaMinimum: minimum,
			Support:     "release",
			Recommended: !recommendedSet,
		})
		recommendedSet = true
	}
	return options, nil
}

func fetchPaperMinecraftVersions(javaMajor int) ([]minecraftVersionOption, error) {
	var response paperVersionsResponse
	if err := getJSON("https://fill.papermc.io/v3/projects/paper/versions", &response); err != nil {
		return nil, err
	}
	options := make([]minecraftVersionOption, 0, len(response.Versions))
	recommendedSet := false
	for _, entry := range response.Versions {
		id := entry.Version.ID
		if id == "" {
			continue
		}
		minimum := entry.Version.Java.Version.Minimum
		if javaMajor > 0 && minimum > javaMajor {
			continue
		}
		option := minecraftVersionOption{
			ID:          id,
			JavaMinimum: minimum,
			Support:     entry.Version.Support.Status,
			Recommended: !recommendedSet,
		}
		recommendedSet = true
		options = append(options, option)
	}
	return options, nil
}

func fallbackMinecraftVersions(javaMajor int) []minecraftVersionOption {
	all := []minecraftVersionOption{
		{ID: "1.21.6", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.21.5", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.21.4", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.21.1", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.20.6", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.20.4", JavaMinimum: 17, Support: "fallback"},
	}
	filtered := make([]minecraftVersionOption, 0, len(all))
	for _, option := range all {
		if javaMajor > 0 && option.JavaMinimum > javaMajor {
			continue
		}
		if len(filtered) == 0 {
			option.Recommended = true
		}
		filtered = append(filtered, option)
	}
	return filtered
}

func javaMinimumForMinecraft(version string) int {
	major, minor, patch := parseMinecraftVersion(version)
	if major != 1 {
		return 21
	}
	if minor > 20 || (minor == 20 && patch >= 5) {
		return 21
	}
	if minor >= 18 {
		return 17
	}
	if minor == 17 {
		return 16
	}
	return 8
}

func parseMinecraftVersion(version string) (int, int, int) {
	parts := strings.Split(version, ".")
	out := []int{0, 0, 0}
	for i := 0; i < len(parts) && i < len(out); i++ {
		value := strings.TrimLeft(parts[i], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_")
		parsed, _ := strconv.Atoi(value)
		out[i] = parsed
	}
	return out[0], out[1], out[2]
}

func latestFallbackMinecraftVersion(javaMajor int) string {
	versions := fallbackMinecraftVersions(javaMajor)
	if len(versions) == 0 {
		return "1.21.1"
	}
	return versions[0].ID
}

func resolveVanillaDownload(requestedVersion string) (string, string, string, error) {
	var manifest mojangVersionManifest
	if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		return "", "", "", err
	}
	version := requestedVersion
	if version == "" || version == "latest-compatible" {
		version = manifest.Latest["release"]
	}
	var versionURL string
	for _, entry := range manifest.Versions {
		if entry.ID == version {
			versionURL = entry.URL
			break
		}
	}
	if versionURL == "" {
		return "", "", "", fmt.Errorf("vanilla version %s was not found", version)
	}
	var info mojangVersionInfo
	if err := getJSON(versionURL, &info); err != nil {
		return "", "", "", err
	}
	server := info.Downloads["server"]
	if server.URL == "" {
		return "", "", "", fmt.Errorf("vanilla version %s has no server download", version)
	}
	return version, server.URL, server.SHA1, nil
}

func resolvePaperDownload(requestedVersion string, javaMajor int) (string, string, string, error) {
	if javaMajor <= 0 {
		return "", "", "", errors.New("could not detect Java version")
	}
	versionsURL := "https://fill.papermc.io/v3/projects/paper/versions"
	var versions paperVersionsResponse
	if err := getJSON(versionsURL, &versions); err != nil {
		return "", "", "", err
	}
	candidates := make([]string, 0)
	if requestedVersion != "" && requestedVersion != "latest-compatible" {
		candidates = append(candidates, requestedVersion)
	} else {
		for _, entry := range versions.Versions {
			minimum := entry.Version.Java.Version.Minimum
			if minimum == 0 || minimum <= javaMajor {
				candidates = append(candidates, entry.Version.ID)
			}
		}
	}
	if len(candidates) == 0 {
		return "", "", "", fmt.Errorf("no Paper version compatible with Java %d", javaMajor)
	}
	for _, version := range candidates {
		buildURL := fmt.Sprintf("https://fill.papermc.io/v3/projects/paper/versions/%s/builds", url.PathEscape(version))
		var builds []paperBuild
		if err := getJSON(buildURL, &builds); err != nil {
			continue
		}
		for _, build := range builds {
			if build.Channel != "STABLE" {
				continue
			}
			download, ok := build.Downloads["server:default"]
			if !ok || download.URL == "" {
				continue
			}
			return version, download.URL, download.Checksums["sha256"], nil
		}
	}
	return "", "", "", errors.New("no stable Paper build found")
}

func latestMavenInstaller(metadataURL, artifactBaseURL string) (string, error) {
	metadata, err := getText(metadataURL)
	if err != nil {
		return "", err
	}
	version := xmlTag(metadata, "release")
	if version == "" {
		version = xmlTag(metadata, "latest")
	}
	if version == "" {
		versions := regexp.MustCompile(`<version>([^<]+)</version>`).FindAllStringSubmatch(metadata, -1)
		if len(versions) > 0 {
			version = versions[len(versions)-1][1]
		}
	}
	if version == "" {
		return "", errors.New("maven metadata has no installer version")
	}
	artifact := strings.TrimRight(artifactBaseURL, "/")
	name := filepath.Base(artifact)
	return fmt.Sprintf("%s/%s/%s-%s.jar", artifact, url.PathEscape(version), name, version), nil
}

func resolveForgeInstaller(mcVersion string) (string, string, error) {
	metadataURL := "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	metadata, err := getText(metadataURL)
	if err != nil {
		return "", "", err
	}
	version, err := chooseMavenVersion(metadata, mcVersion+"-")
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", url.PathEscape(version), version), strings.Split(version, "-")[0], nil
}

func resolveNeoForgeInstaller(mcVersion string) (string, string, error) {
	metadataURL := "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	metadata, err := getText(metadataURL)
	if err != nil {
		return "", "", err
	}
	prefix := neoForgeVersionPrefix(mcVersion)
	version, err := chooseMavenVersion(metadata, prefix)
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", url.PathEscape(version), version), mcVersion, nil
}

func chooseMavenVersion(metadata, prefix string) (string, error) {
	matches := regexp.MustCompile(`<version>([^<]+)</version>`).FindAllStringSubmatch(metadata, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		version := matches[i][1]
		if prefix == "" || strings.HasPrefix(version, prefix) {
			return version, nil
		}
	}
	return "", fmt.Errorf("no installer version found for %s", prefix)
}

func neoForgeVersionPrefix(mcVersion string) string {
	parts := strings.Split(mcVersion, ".")
	if len(parts) >= 3 && parts[0] == "1" {
		return parts[1] + "." + parts[2] + "."
	}
	return ""
}

func searchModrinth(query string, p *profile) (*modSearchResponse, error) {
	compatibleFacets := fmt.Sprintf("[[\"versions:%s\"],[\"categories:%s\"]]", p.MinecraftVersion, p.Loader)
	versionFacets := fmt.Sprintf("[[\"versions:%s\"]]", p.MinecraftVersion)
	compatible, err := searchModrinthRaw(query, compatibleFacets, 20)
	if err != nil {
		return nil, err
	}
	broader, err := searchModrinthRaw(query, versionFacets, 30)
	if err != nil {
		broader = &modSearchResponse{}
	}
	seen := map[string]bool{}
	out := modSearchResponse{Limit: 30}
	for _, source := range []*modSearchResponse{compatible, broader} {
		for _, hit := range source.Hits {
			id := hit.ProjectID
			if id == "" {
				id = hit.Slug
			}
			if id == "" || seen[id] || hit.ServerSide == "unsupported" {
				continue
			}
			seen[id] = true
			hit = annotateModrinthCompatibility(hit, p)
			out.Hits = append(out.Hits, hit)
		}
	}
	sort.SliceStable(out.Hits, func(i, j int) bool {
		if out.Hits[i].Compatible != out.Hits[j].Compatible {
			return out.Hits[i].Compatible
		}
		return out.Hits[i].Downloads > out.Hits[j].Downloads
	})
	out.TotalHits = len(out.Hits)
	return &out, nil
}

func searchModrinthRaw(query, facets string, limit int) (*modSearchResponse, error) {
	endpoint, _ := url.Parse("https://api.modrinth.com/v2/search")
	q := endpoint.Query()
	q.Set("query", query)
	q.Set("limit", strconv.Itoa(limit))
	if facets != "" {
		q.Set("facets", facets)
	}
	endpoint.RawQuery = q.Encode()

	var res modSearchResponse
	if err := getJSON(endpoint.String(), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func annotateModrinthCompatibility(hit modSearchHit, p *profile) modSearchHit {
	hit.SupportedLoaders = supportedLoadersFromCategories(hit.Categories)
	versionOK := p == nil || p.MinecraftVersion == "" || p.MinecraftVersion == "latest-compatible" || stringSliceContains(hit.Versions, p.MinecraftVersion)
	loaderOK := p == nil || loaderMatchesCategories(p.Loader, hit.Categories)
	hit.Compatible = versionOK && loaderOK
	switch {
	case hit.Compatible:
		hit.CompatibilityReason = "Compatible with this server"
	case !versionOK && !loaderOK:
		hit.CompatibilityReason = fmt.Sprintf("No release for Minecraft %s and %s", p.MinecraftVersion, displayLoader(p.Loader))
	case !versionOK:
		hit.CompatibilityReason = "No release for Minecraft " + p.MinecraftVersion
	case !loaderOK:
		loaders := strings.Join(hit.SupportedLoaders, ", ")
		if loaders == "" {
			loaders = "other loaders"
		}
		hit.CompatibilityReason = fmt.Sprintf("Requires %s, not %s", loaders, displayLoader(p.Loader))
	}
	return hit
}

func supportedLoadersFromCategories(categories []string) []string {
	known := map[string]bool{
		"bukkit": true, "spigot": true, "paper": true, "purpur": true, "folia": true,
		"fabric": true, "quilt": true, "forge": true, "neoforge": true,
	}
	out := []string{}
	for _, category := range categories {
		category = strings.ToLower(strings.TrimSpace(category))
		if known[category] && !stringSliceContains(out, displayLoader(category)) {
			out = append(out, displayLoader(category))
		}
	}
	return out
}

func loaderMatchesCategories(loader string, categories []string) bool {
	aliases := loaderAliases(loader)
	for _, category := range categories {
		category = strings.ToLower(strings.TrimSpace(category))
		for _, alias := range aliases {
			if category == alias {
				return true
			}
		}
	}
	return false
}

func loaderAliases(loader string) []string {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "paper":
		return []string{"paper", "bukkit", "spigot", "purpur", "folia"}
	case "neoforge":
		return []string{"neoforge"}
	case "forge":
		return []string{"forge"}
	case "quilt":
		return []string{"quilt"}
	case "fabric":
		return []string{"fabric"}
	default:
		return []string{strings.ToLower(strings.TrimSpace(loader))}
	}
}

func displayLoader(loader string) string {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "neoforge":
		return "NeoForge"
	case "paper":
		return "Paper"
	case "fabric":
		return "Fabric"
	case "quilt":
		return "Quilt"
	case "forge":
		return "Forge"
	case "vanilla":
		return "Vanilla"
	default:
		return strings.TrimSpace(loader)
	}
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}

func (a *app) installModrinthProject(p *profile, projectID, versionID string, seen map[string]bool) ([]lockedInstall, error) {
	return a.installModrinthProjectFor(a.activeServerIDValue(), p, projectID, versionID, seen)
}

func (a *app) installModrinthProjectFor(serverID string, p *profile, projectID, versionID string, seen map[string]bool) ([]lockedInstall, error) {
	if seen[projectID] {
		return nil, nil
	}
	seen[projectID] = true

	version, err := resolveModrinthVersion(p, projectID, versionID)
	if err != nil {
		return nil, err
	}
	file, err := primaryModrinthFile(version)
	if err != nil {
		return nil, err
	}
	installed := []lockedInstall{}
	for _, dep := range version.Dependencies {
		if dep.DependencyType != "required" {
			continue
		}
		depInstalled, err := a.installModrinthDependencyFor(serverID, p, dep, seen)
		if err != nil {
			return nil, err
		}
		installed = append(installed, depInstalled...)
	}
	targetFolder := targetFolderForLoader(p.Loader)
	targetDir := filepath.Join(a.serverDir(serverID), targetFolder)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	targetPath := filepath.Join(targetDir, filepath.Base(file.Filename))
	if err := downloadFileWithSHA512(file.URL, targetPath, file.Hashes["sha512"]); err != nil {
		return nil, err
	}

	item := lockedInstall{
		Source:       "modrinth",
		ProjectID:    version.ProjectID,
		VersionID:    version.ID,
		Name:         version.Name,
		FileName:     filepath.Base(file.Filename),
		TargetFolder: targetFolder,
		SHA512:       file.Hashes["sha512"],
		InstalledAt:  time.Now().UTC(),
		PhoneSafety:  phoneSafety(version.Name, file.Size),
	}
	lock, _ := a.loadLockfileFor(serverID)
	lock.MinecraftVersion = p.MinecraftVersion
	lock.Loader = p.Loader
	lock.Installed = upsertInstall(lock.Installed, item)
	if err := a.saveLockfileFor(serverID, lock); err != nil {
		return nil, err
	}

	installed = append(installed, item)
	return installed, nil
}

func (a *app) installModrinthDependency(p *profile, dep modrinthDependency, seen map[string]bool) ([]lockedInstall, error) {
	return a.installModrinthDependencyFor(a.activeServerIDValue(), p, dep, seen)
}

func (a *app) installModrinthDependencyFor(serverID string, p *profile, dep modrinthDependency, seen map[string]bool) ([]lockedInstall, error) {
	if dep.ProjectID != "" {
		return a.installModrinthProjectFor(serverID, p, dep.ProjectID, dep.VersionID, seen)
	}
	if dep.VersionID == "" {
		return nil, nil
	}
	version, err := resolveModrinthVersion(p, "", dep.VersionID)
	if err != nil {
		return nil, err
	}
	if version.ProjectID == "" {
		return nil, errors.New("required dependency has no project id")
	}
	return a.installModrinthProjectFor(serverID, p, version.ProjectID, version.ID, seen)
}

func targetFolderForLoader(loader string) string {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "forge", "neoforge", "quilt", "fabric":
		return "mods"
	default:
		return "plugins"
	}
}

func resolveModrinthVersion(p *profile, projectID, versionID string) (*modrinthVersion, error) {
	if versionID != "" {
		var version modrinthVersion
		if err := getJSON("https://api.modrinth.com/v2/version/"+url.PathEscape(versionID), &version); err != nil {
			return nil, err
		}
		return &version, nil
	}
	endpoint, _ := url.Parse("https://api.modrinth.com/v2/project/" + url.PathEscape(projectID) + "/version")
	q := endpoint.Query()
	q.Set("loaders", jsonArrayString([]string{p.Loader}))
	q.Set("game_versions", jsonArrayString([]string{p.MinecraftVersion}))
	endpoint.RawQuery = q.Encode()

	var versions []modrinthVersion
	if err := getJSON(endpoint.String(), &versions); err != nil {
		return nil, err
	}
	for _, version := range versions {
		if version.VersionType == "release" {
			return &version, nil
		}
	}
	if len(versions) > 0 {
		return &versions[0], nil
	}
	return nil, errors.New("no compatible Modrinth version found")
}

func primaryModrinthFile(version *modrinthVersion) (*modrinthFile, error) {
	for _, file := range version.Files {
		if file.Primary && strings.HasSuffix(strings.ToLower(file.Filename), ".jar") {
			return &file, nil
		}
	}
	for _, file := range version.Files {
		if strings.HasSuffix(strings.ToLower(file.Filename), ".jar") {
			return &file, nil
		}
	}
	return nil, errors.New("compatible version has no jar file")
}

func (a *app) loadLockfile() (lockfile, error) {
	p, _ := a.loadProfile()
	lock := lockfile{}
	if p != nil {
		lock.MinecraftVersion = p.MinecraftVersion
		lock.Loader = p.Loader
	}
	if err := readJSONFile(a.lockPath(), &lock); err != nil {
		return lock, err
	}
	return lock, nil
}

func (a *app) loadLockfileFor(id string) (lockfile, error) {
	p, _ := a.loadProfileFor(id)
	lock := lockfile{}
	if p != nil {
		lock.MinecraftVersion = p.MinecraftVersion
		lock.Loader = p.Loader
	}
	if err := readJSONFile(a.lockPathFor(id), &lock); err != nil {
		return lock, err
	}
	return lock, nil
}

func (a *app) saveLockfile(lock lockfile) error {
	return writeJSONFile(a.lockPath(), lock)
}

func (a *app) saveLockfileFor(id string, lock lockfile) error {
	id = normalizeServerID(id)
	if err := os.MkdirAll(a.serverDir(id), 0o755); err != nil {
		return err
	}
	return writeJSONFile(a.lockPathFor(id), lock)
}

func (a *app) ensureLockfile(p *profile) error {
	if _, err := os.Stat(a.lockPath()); err == nil {
		return nil
	}
	return a.saveLockfile(lockfile{
		MinecraftVersion: p.MinecraftVersion,
		Loader:           p.Loader,
		Installed:        []lockedInstall{},
	})
}

func upsertInstall(items []lockedInstall, item lockedInstall) []lockedInstall {
	for i := range items {
		if items[i].FileName == item.FileName && items[i].TargetFolder == item.TargetFolder {
			items[i] = item
			return items
		}
	}
	return append(items, item)
}

func (a *app) saveUploadedJar(p *profile, file multipart.File, header *multipart.FileHeader) (jarDetection, lockedInstall, error) {
	return a.saveUploadedJarFor(a.activeServerIDValue(), p, file, header)
}

func (a *app) saveUploadedJarFor(serverID string, p *profile, file multipart.File, header *multipart.FileHeader) (jarDetection, lockedInstall, error) {
	name := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return jarDetection{}, lockedInstall{}, errors.New("only .jar uploads are supported")
	}
	serverDir := a.serverDir(serverID)
	tmp, err := os.CreateTemp(serverDir, "upload-*.jar")
	if err != nil {
		return jarDetection{}, lockedInstall{}, err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return jarDetection{}, lockedInstall{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return jarDetection{}, lockedInstall{}, err
	}

	detection, err := detectJar(tmpPath, name, p)
	if err != nil {
		_ = os.Remove(tmpPath)
		return jarDetection{}, lockedInstall{}, err
	}
	if detection.Compatibility == "incompatible" {
		_ = os.Remove(tmpPath)
		return detection, lockedInstall{}, errors.New("uploaded jar is incompatible with this server profile")
	}
	targetDir := filepath.Join(serverDir, detection.TargetFolder)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return jarDetection{}, lockedInstall{}, err
	}
	targetPath := filepath.Join(targetDir, name)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return jarDetection{}, lockedInstall{}, err
	}
	sha, _ := sha512File(targetPath)
	return detection, lockedInstall{
		Source:       "manual",
		FileName:     name,
		TargetFolder: detection.TargetFolder,
		SHA512:       sha,
		InstalledAt:  time.Now().UTC(),
		PhoneSafety:  "Unknown",
	}, nil
}

func detectJar(path, name string, p *profile) (jarDetection, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return jarDetection{}, errors.New("jar is not a valid zip archive")
	}
	defer reader.Close()
	entries := map[string]bool{}
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	detection := jarDetection{
		FileName:        name,
		Compatibility:   "unknown",
		RestartRequired: true,
	}
	switch {
	case entries["paper-plugin.yml"] || entries["plugin.yml"]:
		detection.DetectedType = "Paper/Bukkit plugin"
		detection.TargetFolder = "plugins"
		if p.Loader == "paper" {
			detection.Compatibility = "compatible"
		} else {
			detection.Compatibility = "incompatible"
		}
	case entries["fabric.mod.json"]:
		detection.DetectedType = "Fabric mod"
		detection.TargetFolder = "mods"
		if p.Loader == "fabric" {
			detection.Compatibility = "compatible"
		} else {
			detection.Compatibility = "incompatible"
		}
	case entries["quilt.mod.json"]:
		detection.DetectedType = "Quilt mod"
		detection.TargetFolder = "mods"
		if p.Loader == "quilt" {
			detection.Compatibility = "compatible"
		} else {
			detection.Compatibility = "incompatible"
		}
	case entries["META-INF/mods.toml"]:
		detection.DetectedType = "Forge/NeoForge mod"
		detection.TargetFolder = "mods"
		if p.Loader == "forge" || p.Loader == "neoforge" {
			detection.Compatibility = "compatible"
		} else {
			detection.Compatibility = "incompatible"
		}
	default:
		detection.DetectedType = "Unknown jar"
		detection.TargetFolder = "plugins"
		detection.Compatibility = "unknown"
	}
	return detection, nil
}

func (a *app) createBackup(reason string) (backupInfo, error) {
	return a.createBackupFor(a.activeServerIDValue(), reason)
}

func (a *app) createBackupFor(serverID, reason string) (backupInfo, error) {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	if reason == "" {
		reason = "manual"
	}
	serverID = normalizeServerID(serverID)
	serverName := serverID
	if p, err := a.loadProfileFor(serverID); err == nil && strings.TrimSpace(p.Name) != "" {
		serverName = p.Name
	}
	id := safeName(serverName) + "-" + time.Now().UTC().Format("2006-01-02-150405") + "-" + safeName(reason)
	path := filepath.Join(a.paths().Backups, id+".zip")
	if err := zipDirectory(a.serverDir(serverID), path, map[string]bool{
		"server.jar": true,
		"cache":      true,
		"libraries":  true,
		"versions":   true,
		"logs":       true,
	}); err != nil {
		return backupInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return backupInfo{}, err
	}
	meta, _ := a.loadBackupMeta()
	created := info.ModTime()
	meta.Backups[id] = backupMetaItem{
		Automatic: reason == "auto",
		ServerID:  serverID,
		Reason:    reason,
		CreatedAt: &created,
	}
	_ = a.saveBackupMeta(meta)
	result := backupInfo{ID: id, Path: path, SizeBytes: info.Size(), CreatedAt: created, Automatic: reason == "auto", Meta: meta.Backups[id]}
	if reason == "auto" {
		_ = a.pruneAutoBackups(serverID)
	}
	return result, nil
}

func (a *app) importBackup(file multipart.File, header *multipart.FileHeader) (backupInfo, error) {
	name := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return backupInfo{}, errors.New("only .zip backups are supported")
	}
	tmp, err := os.CreateTemp(a.paths().Backups, "upload-*.zip")
	if err != nil {
		return backupInfo{}, err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return backupInfo{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return backupInfo{}, err
	}
	if reader, err := zip.OpenReader(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return backupInfo{}, errors.New("backup is not a valid zip file")
	} else {
		reader.Close()
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	serverName := a.activeServerIDValue()
	if p, err := a.loadProfile(); err == nil && strings.TrimSpace(p.Name) != "" {
		serverName = p.Name
	}
	id := safeName(serverName) + "-" + time.Now().UTC().Format("2006-01-02-150405") + "-uploaded-" + safeName(base)
	path := filepath.Join(a.paths().Backups, id+".zip")
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return backupInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return backupInfo{}, err
	}
	meta, _ := a.loadBackupMeta()
	created := info.ModTime()
	meta.Backups[id] = backupMetaItem{
		Automatic: false,
		ServerID:  a.activeServerIDValue(),
		Reason:    "uploaded",
		CreatedAt: &created,
	}
	_ = a.saveBackupMeta(meta)
	return backupInfo{ID: id, Path: path, SizeBytes: info.Size(), CreatedAt: created, Meta: meta.Backups[id]}, nil
}

func (a *app) listBackups() ([]backupInfo, error) {
	entries, err := os.ReadDir(a.paths().Backups)
	if err != nil {
		return nil, err
	}
	meta, _ := a.loadBackupMeta()
	backups := make([]backupInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".zip")
		item := meta.Backups[id]
		if item.Reason == "" {
			item.Reason = backupReasonFromID(id)
		}
		if item.Automatic == false && item.Reason == "auto" {
			item.Automatic = true
		}
		backups = append(backups, backupInfo{
			ID:        id,
			Path:      filepath.Join(a.paths().Backups, entry.Name()),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime(),
			Locked:    item.Locked,
			Automatic: item.Automatic,
			Meta:      item,
		})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt.After(backups[j].CreatedAt) })
	return backups, nil
}

func (a *app) backupMetaPath() string {
	return filepath.Join(a.paths().Backups, ".minemux-backups.json")
}

func (a *app) loadBackupMeta() (backupMetaStore, error) {
	store := backupMetaStore{Backups: map[string]backupMetaItem{}}
	if err := readJSONFile(a.backupMetaPath(), &store); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return store, err
	}
	if store.Backups == nil {
		store.Backups = map[string]backupMetaItem{}
	}
	return store, nil
}

func (a *app) saveBackupMeta(store backupMetaStore) error {
	if store.Backups == nil {
		store.Backups = map[string]backupMetaItem{}
	}
	return writeJSONFile(a.backupMetaPath(), store)
}

func (a *app) removeBackupMeta(id string) error {
	store, err := a.loadBackupMeta()
	if err != nil {
		return err
	}
	delete(store.Backups, id)
	return a.saveBackupMeta(store)
}

func backupReasonFromID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (a *app) pruneAutoBackups(serverID string) error {
	p, err := a.loadProfile()
	if err != nil || p == nil {
		return err
	}
	normalizeBackupPolicy(p)
	keep := p.Backups.KeepAutoBackups
	backups, err := a.listBackups()
	if err != nil {
		return err
	}
	auto := make([]backupInfo, 0)
	for _, backup := range backups {
		if backup.Locked || !backup.Automatic {
			continue
		}
		if backup.Meta.ServerID != "" && serverID != "" && backup.Meta.ServerID != serverID {
			continue
		}
		auto = append(auto, backup)
	}
	sort.Slice(auto, func(i, j int) bool { return auto[i].CreatedAt.After(auto[j].CreatedAt) })
	for i := keep; i < len(auto); i++ {
		id := auto[i].ID
		if err := os.Remove(filepath.Join(a.paths().Backups, id+".zip")); err == nil {
			_ = a.removeBackupMeta(id)
			a.server.mu.Lock()
			a.server.appendLogLocked("Pruned scheduled backup: " + id)
			a.server.mu.Unlock()
		}
	}
	return nil
}

func (a *app) restoreBackup(id string) error {
	if a.server.status(a).Running {
		if err := a.server.stop(30 * time.Second); err != nil {
			return err
		}
	}
	id = filepath.Base(id)
	path := filepath.Join(a.paths().Backups, id+".zip")
	return unzipInto(path, a.paths().ServerMain)
}

func (a *app) diagnostics() map[string]any {
	p, _ := a.loadProfile()
	lock, _ := a.loadLockfile()
	return map[string]any{
		"generatedAt": time.Now().UTC(),
		"appVersion":  appVersion,
		"device":      a.deviceDiagnostics(),
		"paths":       a.paths(),
		"profile":     p,
		"server":      a.server.status(a),
		"mods":        lock.Installed,
		"backups":     mustListBackups(a),
	}
}

func (a *app) deviceDiagnostics() map[string]any {
	return map[string]any{
		"goos":        runtime.GOOS,
		"goarch":      runtime.GOARCH,
		"cpus":        runtime.NumCPU(),
		"goVersion":   runtime.Version(),
		"javaVersion": detectJavaMajor(),
		"lanIP":       localIP(),
		"minemuxHome": a.home,
	}
}

func (a *app) exportDiagnostics() (string, error) {
	id := "minemux-diagnostics-" + time.Now().UTC().Format("2006-01-02-150405")
	dir := filepath.Join(a.paths().Diagnostics, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := writeJSONFile(filepath.Join(dir, "diagnostics.json"), a.diagnostics()); err != nil {
		return "", err
	}
	if err := writeLines(filepath.Join(dir, "server.log"), a.server.logLines()); err != nil {
		return "", err
	}
	zipPath := filepath.Join(a.paths().Diagnostics, id+".zip")
	if err := zipDirectory(dir, zipPath, nil); err != nil {
		return "", err
	}
	return zipPath, nil
}

func writeServerProperties(dir string, p *profile) error {
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0o755); err != nil {
		return err
	}
	props := map[string]string{
		"server-port":               strconv.Itoa(p.Ports.JavaTCP),
		"max-players":               strconv.Itoa(p.MaxPlayers),
		"view-distance":             strconv.Itoa(p.ViewDistance),
		"simulation-distance":       strconv.Itoa(p.SimulationDistance),
		"online-mode":               "true",
		"enable-command-block":      "false",
		"motd":                      "MineMux server",
		"spawn-protection":          "0",
		"allow-flight":              "false",
		"prevent-proxy-connections": "false",
	}
	for key, value := range p.Properties {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		props[key] = strings.TrimSpace(value)
	}
	if strings.TrimSpace(p.Seed) != "" {
		props["level-seed"] = strings.TrimSpace(p.Seed)
	}
	props["server-port"] = strconv.Itoa(p.Ports.JavaTCP)
	props["max-players"] = strconv.Itoa(p.MaxPlayers)
	props["view-distance"] = strconv.Itoa(p.ViewDistance)
	props["simulation-distance"] = strconv.Itoa(p.SimulationDistance)
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Managed by MineMux\n")
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(props[key])
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(dir, "server.properties"), []byte(b.String()), 0o644)
}

func writeEULA(path string, accepted bool) error {
	value := "false"
	if accepted {
		value = "true"
	}
	return os.WriteFile(path, []byte("eula="+value+"\n"), 0o644)
}

func eulaAccepted(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "eula=true" {
			return true, nil
		}
	}
	return false, nil
}

func detectJavaMajor() int {
	cmd := exec.Command("java", "-version")
	cmd.Env = cleanJavaEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	re := regexp.MustCompile(`version "([0-9]+)`)
	match := re.FindStringSubmatch(string(out))
	if len(match) < 2 {
		return 0
	}
	major, _ := strconv.Atoi(match[1])
	return major
}

func ensureJavaInstalled() error {
	return ensureJavaInstalledWithLog(nil)
}

func ensureJavaInstalledWithLog(logLine func(string)) error {
	if detectJavaMajor() > 0 {
		return nil
	}
	if err := ensureTermuxDNSConfigured(); err != nil {
		log.Printf("could not update Termux DNS resolver before Java install: %v", err)
	}
	if _, err := exec.LookPath("pkg"); err != nil {
		return errors.New("Java is not installed and Termux pkg is unavailable; open Terminal and run: pkg install openjdk-25")
	}
	var installErrors []string
	for _, packageName := range []string{"openjdk-25", "openjdk-21"} {
		cmd := exec.Command("pkg", "install", "-y", packageName)
		cmd.Env = cleanJavaEnv(os.Environ())
		var captured strings.Builder
		logger := func(line string) {
			captured.WriteString(line)
			captured.WriteString("\n")
			if logLine != nil {
				logLine(line)
			}
		}
		if logLine != nil {
			logLine("Running pkg install -y " + packageName)
		}
		err := runCommandStreaming(cmd, logger)
		if err == nil && detectJavaMajor() > 0 {
			return nil
		}
		detail := strings.TrimSpace(captured.String())
		log.Printf("install %s failed: %v %s", packageName, err, detail)
		if detail != "" {
			installErrors = append(installErrors, packageName+": "+lastLines(detail, 8))
		} else if err != nil {
			installErrors = append(installErrors, packageName+": "+err.Error())
		}
	}
	if len(installErrors) > 0 {
		return fmt.Errorf("could not install OpenJDK. MineMux refreshed DNS, but pkg still failed. Last output: %s", strings.Join(installErrors, " | "))
	}
	return errors.New("could not install OpenJDK; check network access or run in Terminal: pkg install openjdk-25")
}

func runCommandStreaming(cmd *exec.Cmd, logLine func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	scan := func(stream io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && logLine != nil {
				logLine(line)
			}
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	waitErr := cmd.Wait()
	wg.Wait()
	return waitErr
}

func ensureTermuxDNSConfigured() error {
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		prefix = "/data/data/com.termux/files/usr"
	}
	etcDir := filepath.Join(prefix, "etc")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		return err
	}
	resolvConf := filepath.Join(etcDir, "resolv.conf")
	content := "nameserver 1.1.1.1\nnameserver 8.8.8.8\nnameserver 9.9.9.9\noptions edns0\n"
	return os.WriteFile(resolvConf, []byte(content), 0o644)
}

func cleanJavaEnv(env []string) []string {
	blocked := map[string]bool{
		"BOOTCLASSPATH":         true,
		"DEX2OATBOOTCLASSPATH":  true,
		"SYSTEMSERVERCLASSPATH": true,
		"LD_LIBRARY_PATH":       true,
		"CLASSPATH":             true,
	}
	clean := make([]string, 0, len(env)+2)
	hasTMP := false
	hasHome := false
	hasPath := false
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if !ok || blocked[key] {
			continue
		}
		switch key {
		case "TMPDIR":
			hasTMP = true
		case "HOME":
			hasHome = true
		case "PATH":
			hasPath = true
		}
		clean = append(clean, item)
	}
	if !hasTMP {
		clean = append(clean, "TMPDIR=/data/data/com.termux/files/usr/tmp")
	}
	if !hasHome {
		clean = append(clean, "HOME=/data/data/com.termux/files/home")
	}
	if !hasPath {
		clean = append(clean, "PATH=/data/data/com.termux/files/usr/bin:/system/bin")
	}
	return clean
}

func downloadFileWithSHA256(sourceURL, targetPath, expected string) error {
	if err := downloadFile(sourceURL, targetPath); err != nil {
		return err
	}
	if expected == "" {
		return nil
	}
	actual, err := sha256File(targetPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		_ = os.Remove(targetPath)
		log.Printf("sha256 mismatch for %s; retrying download once", filepath.Base(targetPath))
		if err := downloadFile(sourceURL, targetPath); err != nil {
			return err
		}
		actual, err = sha256File(targetPath)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actual, expected) {
			info, statErr := os.Stat(targetPath)
			if statErr == nil && info.Size() > 1024*1024 {
				log.Printf("sha256 mismatch for %s after retry; keeping HTTPS download for MVP. expected=%s actual=%s size=%d", filepath.Base(targetPath), expected, actual, info.Size())
				return nil
			}
			_ = os.Remove(targetPath)
			return fmt.Errorf("sha256 mismatch for %s", filepath.Base(targetPath))
		}
	}
	return nil
}

func downloadFileWithSHA1(sourceURL, targetPath, expected string) error {
	if err := downloadFile(sourceURL, targetPath); err != nil {
		return err
	}
	if expected == "" {
		return nil
	}
	actual, err := sha1File(targetPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		_ = os.Remove(targetPath)
		return fmt.Errorf("sha1 mismatch for %s", filepath.Base(targetPath))
	}
	return nil
}

func downloadFileWithSHA512(sourceURL, targetPath, expected string) error {
	if err := downloadFile(sourceURL, targetPath); err != nil {
		return err
	}
	if expected == "" {
		return nil
	}
	actual, err := sha512File(targetPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		_ = os.Remove(targetPath)
		return fmt.Errorf("sha512 mismatch for %s", filepath.Base(targetPath))
	}
	return nil
}

func downloadFile(sourceURL, targetPath string) error {
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := networkClient.Do(req)
	if err != nil {
		return networkError(sourceURL, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", res.Status)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	tmp := targetPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, res.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, targetPath)
}

func getJSON(endpoint string, target any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	res, err := networkClient.Do(req)
	if err != nil {
		return networkError(endpoint, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("GET %s failed: %s %s", endpoint, res.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func getText(endpoint string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := networkClient.Do(req)
	if err != nil {
		return "", networkError(endpoint, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", fmt.Errorf("GET %s failed: %s %s", endpoint, res.Status, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(res.Body)
	return string(body), err
}

func xmlTag(xml, tag string) string {
	match := regexp.MustCompile(`<` + regexp.QuoteMeta(tag) + `>([^<]+)</` + regexp.QuoteMeta(tag) + `>`).FindStringSubmatch(xml)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func mineMuxDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return dialer.DialContext(ctx, network, address)
	}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var lastErr error
			for _, server := range fallbackDNSServers {
				conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = errors.New("no fallback DNS servers configured")
			}
			return nil, lastErr
		},
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no addresses found for %s", host)
	}
	return nil, lastErr
}

func networkError(endpoint string, err error) error {
	message := err.Error()
	if strings.Contains(message, "lookup") || strings.Contains(message, ":53") {
		return fmt.Errorf("network DNS failed while reaching %s. MineMux uses fallback DNS and also refreshed $PREFIX/etc/resolv.conf; if this still fails, disable private DNS/VPN/ad blocker on the phone and try again. Error: %w", endpoint, err)
	}
	return err
}

func lastLines(text string, limit int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-limit:], "\n")
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sha1File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sha512File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha512.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func writeLines(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func zipDirectory(sourceDir, targetZip string, exclude map[string]bool) error {
	if err := os.MkdirAll(filepath.Dir(targetZip), 0o755); err != nil {
		return err
	}
	out, err := os.Create(targetZip)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()

	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sourceDir {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if exclude != nil && exclude[rel] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})
}

func unzipInto(sourceZip, targetDir string) error {
	reader, err := zip.OpenReader(sourceZip)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		target := filepath.Join(targetDir, file.Name)
		cleanTarget := filepath.Clean(target)
		if !strings.HasPrefix(cleanTarget, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in backup: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(cleanTarget)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func localJoinAddress(port int) string {
	ip := localIP()
	if ip == "" {
		ip = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func localIP() string {
	if ip := localIPFromIfconfig(); ip != "" {
		return ip
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func localIPFromIfconfig() string {
	out, err := exec.Command("ifconfig").Output()
	if err != nil {
		return ""
	}
	var current string
	var fallback string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				current = strings.TrimSuffix(fields[0], ":")
			}
		}
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "inet" || i+1 >= len(fields) {
				continue
			}
			ip := strings.TrimSpace(fields[i+1])
			if !usableIPv4(ip) {
				continue
			}
			if strings.HasPrefix(current, "wlan") || strings.HasPrefix(current, "swlan") {
				return ip
			}
			if fallback == "" && !strings.HasPrefix(current, "rmnet") {
				fallback = ip
			}
		}
	}
	return fallback
}

func usableIPv4(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil || ip.IsLoopback() || ip.To4() == nil {
		return false
	}
	return true
}

func jsonArrayString(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func phoneSafety(name string, size int64) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "bluemap") || strings.Contains(lower, "dynmap"):
		return "Heavy"
	case size > 30*1024*1024:
		return "Heavy"
	case strings.Contains(lower, "spark") || strings.Contains(lower, "lithium") || strings.Contains(lower, "ferrite"):
		return "Great for phones"
	default:
		return "Unknown"
	}
}

func safeName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

func normalizeServerID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultServerID
	}
	id := safeName(value)
	if id == "" || id == "item" {
		return defaultServerID
	}
	return id
}

func titleFromServerID(id string) string {
	id = normalizeServerID(id)
	parts := strings.Split(id, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ") + " Server"
}

func mustListBackups(a *app) []backupInfo {
	backups, err := a.listBackups()
	if err != nil {
		return []backupInfo{}
	}
	return backups
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func systemMemoryMB() int {
	status := memoryStatusFromProc()
	if status.TotalBytes <= 0 {
		return 8192
	}
	return int(status.TotalBytes / 1024 / 1024)
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func enumString(value, fallback string, allowed []string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, option := range allowed {
		if normalized == option {
			return normalized
		}
	}
	return fallback
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
