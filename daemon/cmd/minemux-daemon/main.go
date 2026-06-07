package main

import (
	"archive/zip"
	"bufio"
	"context"
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
	appVersion      = "0.3.0-dev"
	userAgent       = "MineMux/0.1 local-mvp"
	maxLogLines     = 600
)

type apiError struct {
	Error string `json:"error"`
}

type app struct {
	startedAt time.Time
	home      string
	server    *serverProcess
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
	MinecraftVersion   string `json:"minecraftVersion"`
	MemoryMB           int    `json:"memoryMb"`
	MaxPlayers         int    `json:"maxPlayers"`
	ViewDistance       int    `json:"viewDistance"`
	SimulationDistance int    `json:"simulationDistance"`
	AcceptEULA         bool   `json:"acceptEula"`
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
	App       string       `json:"app"`
	Version   string       `json:"version"`
	StartedAt time.Time    `json:"startedAt"`
	Paths     appPaths     `json:"paths"`
	Profile   *profile     `json:"profile,omitempty"`
	Server    serverStatus `json:"server"`
}

type serverStatus struct {
	Installed   bool       `json:"installed"`
	Running     bool       `json:"running"`
	PID         int        `json:"pid,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	StoppedAt   *time.Time `json:"stoppedAt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	JoinAddress string     `json:"joinAddress,omitempty"`
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

func main() {
	home, err := minemuxHome()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		startedAt: time.Now().UTC(),
		home:      home,
		server:    &serverProcess{},
	}
	if err := a.ensureDirs(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("GET /api/device", a.handleDevice)
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
		ServerMain:   filepath.Join(a.home, "servers", "main"),
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
		ID:                 "main",
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
		App:       "minemux-daemon",
		Version:   appVersion,
		StartedAt: a.startedAt,
		Paths:     a.paths(),
		Profile:   p,
		Server:    a.server.status(a),
	}
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
	if _, err := os.Stat(a.serverJarPath()); err != nil {
		return errors.New("server.jar is missing; run setup first")
	}
	if ok, err := eulaAccepted(filepath.Join(a.paths().ServerMain, "eula.txt")); err != nil || !ok {
		if err != nil {
			return err
		}
		return errors.New("Minecraft EULA is not accepted")
	}

	args := []string{
		"-Dterminal.jline=false",
		"-Dterminal.ansi=false",
		"-Djna.nosys=true",
		fmt.Sprintf("-Xms%dM", minInt(p.MemoryMB, 1024)),
		fmt.Sprintf("-Xmx%dM", p.MemoryMB),
		"-jar",
		"server.jar",
		"nogui",
	}
	cmd := exec.CommandContext(ctx, "java", args...)
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
	s.appendLogLocked(fmt.Sprintf("Paper server started with pid %d", cmd.Process.Pid))

	go s.capture(stdout)
	go s.capture(stderr)
	go s.wait(cmd, done)

	return nil
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
	installed := false
	if _, err := os.Stat(a.serverJarPath()); err == nil {
		installed = true
	}
	status := serverStatus{
		Installed: installed,
		Running:   s.cmd != nil && s.cmd.Process != nil,
		StartedAt: s.startedAt,
		StoppedAt: s.stoppedAt,
		LastError: s.lastError,
	}
	if status.Running {
		status.PID = s.cmd.Process.Pid
	}
	port := 25565
	if p != nil && p.Ports.JavaTCP > 0 {
		port = p.Ports.JavaTCP
	}
	status.JoinAddress = localJoinAddress(port)
	return status
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

	resolvedVersion, jarURL, sha, err := resolvePaperDownload(p.MinecraftVersion, javaMajor)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	p.MinecraftVersion = resolvedVersion
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
	if err := downloadFileWithSHA256(jarURL, a.serverJarPath(), sha); err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
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
	writeJSON(w, http.StatusOK, map[string]any{"online": 0, "players": []string{}})
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
	id := time.Now().UTC().Format("20060102-150405") + "-" + safeName(reason)
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
		"goos":          runtime.GOOS,
		"goarch":        runtime.GOARCH,
		"cpus":          runtime.NumCPU(),
		"goVersion":     runtime.Version(),
		"javaVersion":   detectJavaMajor(),
		"lanIP":         localIP(),
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
	if _, err := exec.LookPath("pkg"); err != nil {
		return errors.New("Java is not installed and Termux pkg is unavailable; open Terminal and run: pkg install openjdk-25")
	}
	for _, packageName := range []string{"openjdk-25", "openjdk-21"} {
		cmd := exec.Command("pkg", "install", "-y", packageName)
		cmd.Env = cleanJavaEnv(os.Environ())
		out, err := cmd.CombinedOutput()
		if err == nil && detectJavaMajor() > 0 {
			return nil
		}
		log.Printf("install %s failed: %v %s", packageName, err, strings.TrimSpace(string(out)))
	}
	return errors.New("could not install OpenJDK; check network access or run in Terminal: pkg install openjdk-25")
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
		return fmt.Errorf("sha256 mismatch for %s", filepath.Base(targetPath))
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
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
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
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("GET %s failed: %s %s", endpoint, res.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(target)
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
