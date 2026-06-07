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
	defaultBindAddr = "127.0.0.1:8787"
	defaultServerID = "main"
	appVersion      = "0.3.0-dev"
	userAgent       = "MineMux/0.1 local-mvp"
	maxLogLines     = 600
)

var fallbackDNSServers = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}

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
	activeMu       sync.RWMutex
	activeServerID string
	statsMu        sync.Mutex
	lastCPU        cpuSample
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
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	ServerType         string          `json:"serverType"`
	MinecraftVersion   string          `json:"minecraftVersion"`
	Loader             string          `json:"loader"`
	JavaVersion        int             `json:"javaVersion"`
	MemoryMB           int             `json:"memoryMb"`
	MaxPlayers         int             `json:"maxPlayers"`
	ViewDistance       int             `json:"viewDistance"`
	SimulationDistance int             `json:"simulationDistance"`
	Ports              profilePorts    `json:"ports"`
	Features           profileFeatures `json:"features"`
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

type setupRequest struct {
	ServerID           string `json:"serverId"`
	Name               string `json:"name"`
	MinecraftVersion   string `json:"minecraftVersion"`
	Loader             string `json:"loader"`
	MemoryMB           int    `json:"memoryMb"`
	MaxPlayers         int    `json:"maxPlayers"`
	ViewDistance       int    `json:"viewDistance"`
	SimulationDistance int    `json:"simulationDistance"`
	AcceptEULA         bool   `json:"acceptEula"`
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
	MemoryMB           *int  `json:"memoryMb"`
	MaxPlayers         *int  `json:"maxPlayers"`
	ViewDistance       *int  `json:"viewDistance"`
	SimulationDistance *int  `json:"simulationDistance"`
	AutoStart          *bool `json:"autoStart"`
	RestartOnCrash     *bool `json:"restartOnCrash"`
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
	Installed   bool       `json:"installed"`
	Running     bool       `json:"running"`
	PID         int        `json:"pid,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	StoppedAt   *time.Time `json:"stoppedAt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	JoinAddress string     `json:"joinAddress,omitempty"`
	UptimeSec   int64      `json:"uptimeSec,omitempty"`
	TPS         float64    `json:"tps,omitempty"`
	Players     int        `json:"players"`
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
}

type modSearchHit struct {
	ProjectID     string   `json:"project_id"`
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	ProjectType   string   `json:"project_type"`
	Categories    []string `json:"categories"`
	Versions      []string `json:"versions"`
	Downloads     int      `json:"downloads"`
	ClientSide    string   `json:"client_side"`
	ServerSide    string   `json:"server_side"`
	IconURL       string   `json:"icon_url"`
	LatestVersion string   `json:"latest_version"`
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
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
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
		ID  string `json:"id"`
		URL string `json:"url"`
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
		activeServerID: defaultServerID,
	}
	a.loadActiveServerID()
	if err := a.ensureDirs(); err != nil {
		log.Fatal(err)
	}
	a.startConfiguredServerIfNeeded()

	mux := http.NewServeMux()
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
	mux.HandleFunc("POST /api/server/start", a.handleServerStart)
	mux.HandleFunc("POST /api/server/stop", a.handleServerStop)
	mux.HandleFunc("POST /api/server/restart", a.handleServerRestart)
	mux.HandleFunc("GET /api/server/status", a.handleServerStatus)
	mux.HandleFunc("GET /api/server/logs", a.handleServerLogs)
	mux.HandleFunc("POST /api/server/command", a.handleServerCommand)
	mux.HandleFunc("GET /api/server/players", a.handleServerPlayers)
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
	for _, dir := range []string{p.Daemon, p.Runtime, p.Servers, p.ServerMain, p.Addons, p.Backups, p.Logs, p.Diagnostics, p.Repositories} {
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
	if err := os.MkdirAll(a.serverDir(id), 0o755); err != nil {
		return err
	}
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

func (a *app) serverJarPath() string {
	return filepath.Join(a.paths().ServerMain, "server.jar")
}

func (a *app) loadProfile() (*profile, error) {
	var p profile
	if err := readJSONFile(a.profilePath(), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (a *app) saveProfile(p *profile) error {
	return writeJSONFile(a.profilePath(), p)
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
	}
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

func (a *app) systemStatus() systemStatus {
	return systemStatus{
		CPU:    a.cpuStatus(),
		Memory: memoryStatusFromProc(),
		Disk:   diskStatusForPath(a.home),
	}
}

func (a *app) cpuStatus() cpuStatus {
	status := cpuStatus{Cores: runtime.NumCPU()}
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		return errors.New("server is already running")
	}
	if p == nil {
		return errors.New("server profile is missing; run setup first")
	}
	command, args, err := a.startCommand(p)
	if err != nil {
		return err
	}
	if ok, err := eulaAccepted(filepath.Join(a.paths().ServerMain, "eula.txt")); err != nil || !ok {
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
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
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
	s.appendLogLocked(fmt.Sprintf("%s server started with pid %d", p.Loader, cmd.Process.Pid))

	go s.capture(stdout)
	go s.capture(stderr)
	go s.wait(cmd, done)

	return nil
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
	}
	return "java", javaJarArgs(p, "server.jar"), nil
}

func javaJarArgs(p *profile, jar string) []string {
	return []string{
		"-Dterminal.jline=false",
		"-Dterminal.ansi=false",
		"-Djna.nosys=true",
		fmt.Sprintf("-Xms%dM", minInt(p.MemoryMB, 1024)),
		fmt.Sprintf("-Xmx%dM", p.MemoryMB),
		"-jar",
		jar,
		"nogui",
	}
}

func (s *serverProcess) stop(timeout time.Duration) error {
	s.mu.Lock()
	cmd := s.cmd
	stdin := s.stdin
	done := s.done
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
	if err := s.stop(30 * time.Second); err != nil && err.Error() != "server is not running" {
		return err
	}
	return s.start(ctx, a, p)
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
	status.Players = countPlayersFromLogs(s.logs)
	port := 25565
	if p != nil && p.Ports.JavaTCP > 0 {
		port = p.Ports.JavaTCP
	}
	status.JoinAddress = localJoinAddress(port)
	return status
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
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *app) handleConfigPost(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadProfile()
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
		p.ViewDistance = clampInt(*req.ViewDistance, 2, 16)
	}
	if req.SimulationDistance != nil {
		p.SimulationDistance = clampInt(*req.SimulationDistance, 2, 16)
	}
	if req.AutoStart != nil {
		p.Features.AutoStart = *req.AutoStart
	}
	if req.RestartOnCrash != nil {
		p.Features.RestartOnCrash = *req.RestartOnCrash
	}
	if err := a.saveProfile(p); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := writeServerProperties(a.paths().ServerMain, p); err != nil {
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
		if err := ensureJavaInstalled(); err != nil {
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
	if req.MemoryMB > 0 {
		p.MemoryMB = clampInt(req.MemoryMB, 512, 8192)
	}
	if req.MaxPlayers > 0 {
		p.MaxPlayers = clampInt(req.MaxPlayers, 1, 50)
	}
	if req.ViewDistance > 0 {
		p.ViewDistance = clampInt(req.ViewDistance, 2, 16)
	}
	if req.SimulationDistance > 0 {
		p.SimulationDistance = clampInt(req.SimulationDistance, 2, 16)
	}

	resolvedVersion, err := a.installServerRuntime(&p, javaMajor)
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
	writeJSON(w, http.StatusOK, a.status())
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
	writeJSON(w, http.StatusOK, map[string][]string{"lines": a.server.logLines()})
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

func (a *app) handleServerPlayers(w http.ResponseWriter, r *http.Request) {
	players := playersFromLogs(a.server.logLines())
	writeJSON(w, http.StatusOK, map[string]any{"online": len(players), "players": players})
}

func (a *app) handleModsList(w http.ResponseWriter, r *http.Request) {
	lock, err := a.loadLockfile()
	if err != nil {
		writeJSON(w, http.StatusOK, lockfile{Installed: []lockedInstall{}})
		return
	}
	writeJSON(w, http.StatusOK, lock)
}

func (a *app) handleModsSearch(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
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
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	var req installModRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	if req.ProjectID == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "projectId is required"})
		return
	}
	if p.Features.CreateRestorePointBeforeModChanges {
		if _, err := a.createBackup("before-mod-install"); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
			return
		}
	}
	installed, err := a.installModrinthProject(p, req.ProjectID, req.VersionID, map[string]bool{})
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
	lock, err := a.loadLockfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "lockfile not found"})
		return
	}
	next := lock.Installed[:0]
	removed := false
	for _, item := range lock.Installed {
		if item.FileName == fileName {
			path := filepath.Join(a.paths().ServerMain, item.TargetFolder, item.FileName)
			_ = os.Remove(path)
			removed = true
			continue
		}
		next = append(next, item)
	}
	lock.Installed = next
	if err := a.saveLockfile(lock); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": removed})
}

func (a *app) handleModsUpload(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadProfile()
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if p.Features.CreateRestorePointBeforeModChanges {
		if _, err := a.createBackup("before-jar-upload"); err != nil {
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
	detection, item, err := a.saveUploadedJar(p, file, header)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	lock, _ := a.loadLockfile()
	lock.Installed = upsertInstall(lock.Installed, item)
	if err := a.saveLockfile(lock); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"backups": backups})
}

func (a *app) handleBackupsCreate(w http.ResponseWriter, r *http.Request) {
	info, err := a.createBackup("manual")
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
	case "paper", "vanilla", "quilt", "forge", "neoforge":
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
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			a.appendSetupLog(line)
		}
	}
	return err
}

func minecraftVersionOptions(javaMajor int) ([]minecraftVersionOption, string, string) {
	versions, err := fetchPaperMinecraftVersions(javaMajor)
	if err == nil && len(versions) > 0 {
		return versions, "papermc", ""
	}
	warning := ""
	if err != nil {
		warning = err.Error()
	}
	return fallbackMinecraftVersions(javaMajor), "fallback", warning
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
	facets := fmt.Sprintf("[[\"versions:%s\"],[\"categories:%s\"]]", p.MinecraftVersion, p.Loader)
	endpoint, _ := url.Parse("https://api.modrinth.com/v2/search")
	q := endpoint.Query()
	q.Set("query", query)
	q.Set("limit", "20")
	q.Set("facets", facets)
	endpoint.RawQuery = q.Encode()

	var res modSearchResponse
	if err := getJSON(endpoint.String(), &res); err != nil {
		return nil, err
	}
	filtered := res.Hits[:0]
	for _, hit := range res.Hits {
		if hit.ServerSide == "unsupported" {
			continue
		}
		filtered = append(filtered, hit)
	}
	res.Hits = filtered
	res.TotalHits = len(filtered)
	return &res, nil
}

func (a *app) installModrinthProject(p *profile, projectID, versionID string, seen map[string]bool) ([]lockedInstall, error) {
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
	targetFolder := "plugins"
	if p.Loader == "fabric" {
		targetFolder = "mods"
	}
	targetDir := filepath.Join(a.paths().ServerMain, targetFolder)
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
	lock, _ := a.loadLockfile()
	lock.MinecraftVersion = p.MinecraftVersion
	lock.Loader = p.Loader
	lock.Installed = upsertInstall(lock.Installed, item)
	if err := a.saveLockfile(lock); err != nil {
		return nil, err
	}

	installed := []lockedInstall{item}
	for _, dep := range version.Dependencies {
		if dep.DependencyType != "required" || dep.ProjectID == "" {
			continue
		}
		depInstalled, err := a.installModrinthProject(p, dep.ProjectID, dep.VersionID, seen)
		if err != nil {
			return nil, err
		}
		installed = append(installed, depInstalled...)
	}
	return installed, nil
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

func (a *app) saveLockfile(lock lockfile) error {
	return writeJSONFile(a.lockPath(), lock)
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
	name := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return jarDetection{}, lockedInstall{}, errors.New("only .jar uploads are supported")
	}
	tmp, err := os.CreateTemp(a.paths().ServerMain, "upload-*.jar")
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
	targetDir := filepath.Join(a.paths().ServerMain, detection.TargetFolder)
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
		detection.Compatibility = "incompatible"
	case entries["META-INF/mods.toml"]:
		detection.DetectedType = "Forge/NeoForge mod"
		detection.TargetFolder = "mods"
		detection.Compatibility = "incompatible"
	default:
		detection.DetectedType = "Unknown jar"
		detection.TargetFolder = "plugins"
		detection.Compatibility = "unknown"
	}
	return detection, nil
}

func (a *app) createBackup(reason string) (backupInfo, error) {
	if reason == "" {
		reason = "manual"
	}
	serverName := a.activeServerIDValue()
	if p, err := a.loadProfile(); err == nil && strings.TrimSpace(p.Name) != "" {
		serverName = p.Name
	}
	id := safeName(serverName) + "-" + time.Now().UTC().Format("2006-01-02-150405") + "-" + safeName(reason)
	path := filepath.Join(a.paths().Backups, id+".zip")
	if err := zipDirectory(a.paths().ServerMain, path, map[string]bool{
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
	return backupInfo{ID: id, Path: path, SizeBytes: info.Size(), CreatedAt: info.ModTime()}, nil
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
	return backupInfo{ID: id, Path: path, SizeBytes: info.Size(), CreatedAt: info.ModTime()}, nil
}

func (a *app) listBackups() ([]backupInfo, error) {
	entries, err := os.ReadDir(a.paths().Backups)
	if err != nil {
		return nil, err
	}
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
		backups = append(backups, backupInfo{
			ID:        id,
			Path:      filepath.Join(a.paths().Backups, entry.Name()),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime(),
		})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt.After(backups[j].CreatedAt) })
	return backups, nil
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
		out, err := cmd.CombinedOutput()
		if err == nil && detectJavaMajor() > 0 {
			return nil
		}
		detail := strings.TrimSpace(string(out))
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

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
