package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
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
	playItReleaseBaseURL = "https://github.com/playit-cloud/playit-agent/releases/latest/download"
	worldMapDefaultLimit = 10000
	worldMapMinLimit     = 1000
	worldMapMaxLimit     = 1000000
	worldMapDetailLimit  = 5000
)

var fallbackDNSServers = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}

var (
	playItClaimURLPattern     = regexp.MustCompile(`https?://(?:www\.)?playit\.gg/(?:claim/[A-Za-z0-9_-]+|mc)\b[^\s<>"']*`)
	playItTunnelPattern       = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*\.gl\.(?:joinmc\.link|at\.ply\.gg)(?::[0-9]{1,5})?\b`)
	minecraftReleaseIDPattern = regexp.MustCompile(`^(?:1\.[0-9]+(?:\.[0-9]+)?|[0-9]{2}\.[0-9]+(?:\.[0-9]+)?)$`)
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
	wake           *wakeOnConnectService
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
	WorldUploadID           string          `json:"worldUploadId"`
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
	runtime   string
}

type wakeOnConnectService struct {
	mu       sync.Mutex
	listener net.Listener
	port     int
	starting bool
}

type playItAgentProcess struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	cliCmd    *exec.Cmd
	done      chan error
	startedAt *time.Time
	serverID  string
	lastError string
	logs      []string
	starting  bool
}

type modSearchHit struct {
	Provider            string   `json:"provider,omitempty"`
	ProjectID           string   `json:"project_id"`
	ProjectIDAlt        string   `json:"projectId,omitempty"`
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
	WebsiteURL          string   `json:"websiteUrl,omitempty"`
	Author              string   `json:"author,omitempty"`
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
	Provider  string `json:"provider"`
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
}

type modVersionOption struct {
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"versionNumber,omitempty"`
	FileName      string   `json:"fileName,omitempty"`
	GameVersions  []string `json:"gameVersions,omitempty"`
	Loaders       []string `json:"loaders,omitempty"`
	ReleaseType   string   `json:"releaseType,omitempty"`
	Downloads     int      `json:"downloads,omitempty"`
	Date          string   `json:"date,omitempty"`
	Primary       bool     `json:"primary,omitempty"`
}

type curseForgeKeyRequest struct {
	APIKey string `json:"apiKey"`
}

type termuxCommandRequest struct {
	Command        string `json:"command"`
	Cwd            string `json:"cwd"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type termuxCommandResponse struct {
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"durationMs"`
}

type updatePlanResponse struct {
	ServerID     string            `json:"serverId"`
	Minecraft    updateRuntimePlan `json:"minecraft"`
	Mods         []updateModPlan   `json:"mods"`
	Blockers     []updateModPlan   `json:"blockers"`
	RequiresStop bool              `json:"requiresStop"`
}

type updateRuntimePlan struct {
	CurrentVersion          string `json:"currentVersion"`
	TargetVersion           string `json:"targetVersion"`
	Loader                  string `json:"loader"`
	Source                  string `json:"source,omitempty"`
	Warning                 string `json:"warning,omitempty"`
	UpdateAvailable         bool   `json:"updateAvailable"`
	RuntimeRefreshAvailable bool   `json:"runtimeRefreshAvailable"`
	Message                 string `json:"message,omitempty"`
}

type updateModPlan struct {
	ProjectID       string `json:"projectId,omitempty"`
	VersionID       string `json:"versionId,omitempty"`
	TargetVersionID string `json:"targetVersionId,omitempty"`
	CurrentName     string `json:"currentName,omitempty"`
	TargetName      string `json:"targetName,omitempty"`
	FileName        string `json:"fileName"`
	TargetFileName  string `json:"targetFileName,omitempty"`
	TargetFolder    string `json:"targetFolder"`
	Source          string `json:"source"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

type updateApplyRequest struct {
	UpdateMinecraft        bool              `json:"updateMinecraft"`
	TargetMinecraftVersion string            `json:"targetMinecraftVersion,omitempty"`
	UpdateMods             bool              `json:"updateMods"`
	ModChoices             map[string]string `json:"modChoices,omitempty"`
}

type updateApplyResponse struct {
	ServerID string              `json:"serverId"`
	BackupID string              `json:"backupId,omitempty"`
	Results  []updateApplyResult `json:"results"`
	Plan     updatePlanResponse  `json:"plan"`
}

type updateApplyResult struct {
	Name     string `json:"name"`
	FileName string `json:"fileName,omitempty"`
	Action   string `json:"action"`
	Message  string `json:"message,omitempty"`
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

type curseForgeSearchResponse struct {
	Data       []curseForgeMod `json:"data"`
	Pagination struct {
		TotalCount int `json:"totalCount"`
	} `json:"pagination"`
}

type curseForgeMod struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Summary       string `json:"summary"`
	DownloadCount int    `json:"downloadCount"`
	Logo          struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Links struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	Categories []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"categories"`
	Authors []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"authors"`
	LatestFilesIndexes []struct {
		GameVersion string `json:"gameVersion"`
		FileID      int    `json:"fileId"`
		Filename    string `json:"filename"`
		ReleaseType int    `json:"releaseType"`
		ModLoader   int    `json:"modLoader"`
	} `json:"latestFilesIndexes"`
}

type curseForgeFilesResponse struct {
	Data []curseForgeFile `json:"data"`
}

type curseForgeFileResponse struct {
	Data curseForgeFile `json:"data"`
}

type curseForgeDownloadURLResponse struct {
	Data string `json:"data"`
}

type curseForgeFile struct {
	ID            int      `json:"id"`
	ModID         int      `json:"modId"`
	DisplayName   string   `json:"displayName"`
	FileName      string   `json:"fileName"`
	ReleaseType   int      `json:"releaseType"`
	FileDate      string   `json:"fileDate"`
	DownloadURL   string   `json:"downloadUrl"`
	FileLength    int64    `json:"fileLength"`
	DownloadCount int      `json:"downloadCount"`
	GameVersions  []string `json:"gameVersions"`
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

type worldMapResponse struct {
	ServerID    string               `json:"serverId"`
	Dimension   string               `json:"dimension"`
	WorldName   string               `json:"worldName"`
	Available   []worldDimensionInfo `json:"availableDimensions"`
	Chunks      []worldMapChunk      `json:"chunks"`
	Bounds      worldMapBounds       `json:"bounds"`
	Limit       int                  `json:"limit"`
	TotalChunks int                  `json:"totalChunks"`
	Mode        string               `json:"mode,omitempty"`
	Truncated   bool                 `json:"truncated"`
	Warning     string               `json:"warning,omitempty"`
}

type worldDimensionInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	RegionFiles int    `json:"regionFiles"`
}

type worldMapChunk struct {
	X      int      `json:"x"`
	Z      int      `json:"z"`
	Color  string   `json:"color,omitempty"`
	Pixels []string `json:"pixels,omitempty"`
}

type worldMapBounds struct {
	MinX int `json:"minX"`
	MaxX int `json:"maxX"`
	MinZ int `json:"minZ"`
	MaxZ int `json:"maxZ"`
}

type worldPlayersResponse struct {
	Online      int                 `json:"online"`
	Players     []worldPlayerMarker `json:"players"`
	RefreshedAt time.Time           `json:"refreshedAt"`
}

type worldPlayerMarker struct {
	Name      string  `json:"name"`
	Dimension string  `json:"dimension,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	ChunkX    int     `json:"chunkX,omitempty"`
	ChunkZ    int     `json:"chunkZ,omitempty"`
	Available bool    `json:"available"`
	Error     string  `json:"error,omitempty"`
}

type worldTrimRequest struct {
	Dimension string          `json:"dimension"`
	Areas     []worldTrimArea `json:"areas"`
}

type worldTrimArea struct {
	MinChunkX int `json:"minChunkX"`
	MaxChunkX int `json:"maxChunkX"`
	MinChunkZ int `json:"minChunkZ"`
	MaxChunkZ int `json:"maxChunkZ"`
}

type worldTrimResponse struct {
	ServerID             string `json:"serverId"`
	Dimension            string `json:"dimension"`
	WorldName            string `json:"worldName"`
	BackupID             string `json:"backupId"`
	BeforeChunks         int    `json:"beforeChunks"`
	KeptChunks           int    `json:"keptChunks"`
	DeletedChunks        int    `json:"deletedChunks"`
	RegionFilesRemoved   int    `json:"regionFilesRemoved"`
	RegionFilesCompacted int    `json:"regionFilesCompacted"`
}

type worldTrimStats struct {
	BeforeChunks         int
	KeptChunks           int
	DeletedChunks        int
	RegionFilesRemoved   int
	RegionFilesCompacted int
}

type modWorldScanRequest struct {
	Namespaces []string `json:"namespaces"`
	Dimensions []string `json:"dimensions,omitempty"`
}

type modWorldScanResponse struct {
	ServerID           string                    `json:"serverId"`
	WorldName          string                    `json:"worldName"`
	Namespaces         []string                  `json:"namespaces"`
	Dimensions         []modWorldDimensionReport `json:"dimensions"`
	TotalChunksScanned int                       `json:"totalChunksScanned"`
	TotalChunksMatched int                       `json:"totalChunksMatched"`
	TotalBlocks        int                       `json:"totalBlocks"`
	TotalBlockEntities int                       `json:"totalBlockEntities"`
	TotalEntities      int                       `json:"totalEntities"`
	BackupID           string                    `json:"backupId,omitempty"`
	Cleaned            bool                      `json:"cleaned"`
}

type modWorldDimensionReport struct {
	Dimension          string `json:"dimension"`
	RegionFiles        int    `json:"regionFiles"`
	RegionFilesChanged int    `json:"regionFilesChanged,omitempty"`
	ChunksScanned      int    `json:"chunksScanned"`
	ChunksMatched      int    `json:"chunksMatched"`
	ChunksCleaned      int    `json:"chunksCleaned,omitempty"`
	Blocks             int    `json:"blocks"`
	BlockEntities      int    `json:"blockEntities"`
	Entities           int    `json:"entities"`
	Warning            string `json:"warning,omitempty"`
}

type modWorldChunkReport struct {
	Blocks        int
	BlockEntities int
	Entities      int
	Changed       bool
}

type nbtTreeTag struct {
	Type     byte
	Name     string
	ListType byte
	Value    any
}

type worldRegionChunkRef struct {
	RegionPath string
	RegionX    int
	RegionZ    int
	LocalX     int
	LocalZ     int
	ChunkX     int
	ChunkZ     int
}

type nbtCompound map[string]any

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

type fabricGameVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type quiltGameVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
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
		wake:           &wakeOnConnectService{},
		activeServerID: defaultServerID,
	}
	a.loadActiveServerID()
	if err := a.ensureDirs(); err != nil {
		log.Fatal(err)
	}
	a.startConfiguredServerIfNeeded()
	a.startBackupScheduler()
	a.startWakeOnConnectMonitor()

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
	mux.HandleFunc("POST /api/server/setup/world", a.handleServerSetupWorldUpload)
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
	mux.HandleFunc("GET /api/world/map", a.handleWorldMap)
	mux.HandleFunc("GET /api/world/players", a.handleWorldPlayers)
	mux.HandleFunc("POST /api/world/trim", a.handleWorldTrim)
	mux.HandleFunc("POST /api/world/mods/scan", a.handleWorldModsScan)
	mux.HandleFunc("POST /api/world/mods/cleanup", a.handleWorldModsCleanup)
	mux.HandleFunc("GET /api/updates", a.handleUpdatesPlan)
	mux.HandleFunc("POST /api/updates/apply", a.handleUpdatesApply)
	mux.HandleFunc("GET /api/mods", a.handleModsList)
	mux.HandleFunc("GET /api/mods/providers", a.handleModsProviders)
	mux.HandleFunc("GET /api/mods/search", a.handleModsSearch)
	mux.HandleFunc("GET /api/mods/versions", a.handleModVersions)
	mux.HandleFunc("POST /api/mods/install", a.handleModsInstall)
	mux.HandleFunc("POST /api/mods/uninstall", a.handleModsUninstall)
	mux.HandleFunc("POST /api/mods/upload", a.handleModsUpload)
	mux.HandleFunc("POST /api/mods/rollback", a.handleModsRollback)
	mux.HandleFunc("POST /api/mods/curseforge/key", a.handleCurseForgeKeySave)
	mux.HandleFunc("DELETE /api/mods/curseforge/key", a.handleCurseForgeKeyDelete)
	mux.HandleFunc("GET /api/backups", a.handleBackupsList)
	mux.HandleFunc("POST /api/backups/create", a.handleBackupsCreate)
	mux.HandleFunc("POST /api/backups/restore", a.handleBackupsRestore)
	mux.HandleFunc("GET /api/backups/download", a.handleBackupsDownload)
	mux.HandleFunc("POST /api/backups/upload", a.handleBackupsUpload)
	mux.HandleFunc("POST /api/backups/lock", a.handleBackupsLock)
	mux.HandleFunc("DELETE /api/backups/", a.handleBackupsDelete)
	mux.HandleFunc("GET /api/diagnostics", a.handleDiagnostics)
	mux.HandleFunc("POST /api/diagnostics/export", a.handleDiagnosticsExport)
	mux.HandleFunc("GET /api/termux/info", a.handleTermuxInfo)
	mux.HandleFunc("POST /api/termux/command", a.handleTermuxCommand)

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
			CreateRestorePointBeforeModChanges: false,
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
		if err := a.server.startWithOptions(context.Background(), a, p, true); err != nil {
			a.server.mu.Lock()
			a.server.lastError = "auto-start failed: " + err.Error()
			a.server.mu.Unlock()
		}
	}()
}

func (a *app) startWakeOnConnectMonitor() {
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			<-timer.C
			a.refreshWakeOnConnect()
			timer.Reset(5 * time.Second)
		}
	}()
}

func (a *app) refreshWakeOnConnect() {
	if a.wake == nil {
		return
	}
	if a.server.isActive() {
		a.wake.stop()
		return
	}
	p, err := a.loadProfile()
	if err != nil || p == nil || !a.serverInstalled(p) || p.Features.AutoStart {
		a.wake.stop()
		return
	}
	a.wake.start(a, p)
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
	if a != nil && a.wake != nil {
		a.wake.stop()
	}
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
	if err := a.ensureFabricRuntimeDependenciesBeforeStart(p); err != nil {
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
	s.logs = nil
	s.cmd = cmd
	s.stdin = stdin
	s.done = done
	s.startedAt = &now
	s.stoppedAt = nil
	s.lastError = ""
	s.ready = false
	s.stopping = false
	s.runtime = strings.ToLower(strings.TrimSpace(p.Loader))
	s.appendLogLocked(fmt.Sprintf("%s server started with pid %d", p.Loader, cmd.Process.Pid))

	go s.capture(stdout)
	go s.capture(stderr)
	go s.wait(cmd, done)
	s.mu.Unlock()

	if p.PlayIt.Enabled && !strings.EqualFold(p.Loader, "paper") {
		go a.startPlayItAgentForProfile(p)
	}

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

func (s *serverProcess) isActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil && s.cmd.Process != nil
}

func (w *wakeOnConnectService) start(a *app, p *profile) {
	if w == nil || a == nil || p == nil {
		return
	}
	port := p.Ports.JavaTCP
	if port <= 0 {
		port = 25565
	}
	w.mu.Lock()
	if w.listener != nil && w.port == port {
		w.mu.Unlock()
		return
	}
	w.closeLocked()
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		w.mu.Unlock()
		return
	}
	w.listener = listener
	w.port = port
	w.starting = false
	w.mu.Unlock()

	a.appendServerLog("Wake-on-connect listening on Java port " + strconv.Itoa(port))
	go w.acceptLoop(a, listener)
}

func (w *wakeOnConnectService) stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closeLocked()
	w.starting = false
}

func (w *wakeOnConnectService) closeLocked() {
	if w.listener != nil {
		_ = w.listener.Close()
		w.listener = nil
	}
	w.port = 0
}

func (w *wakeOnConnectService) acceptLoop(a *app, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		w.trigger(a)
	}
}

func (w *wakeOnConnectService) trigger(a *app) {
	if w == nil || a == nil {
		return
	}
	w.mu.Lock()
	if w.starting {
		w.mu.Unlock()
		return
	}
	w.starting = true
	w.closeLocked()
	w.mu.Unlock()

	a.appendServerLog("Wake-on-connect received a client ping; starting server.")
	go func() {
		p, err := a.loadProfile()
		if err != nil {
			a.appendServerLog("Wake-on-connect failed: " + err.Error())
			w.mu.Lock()
			w.starting = false
			w.mu.Unlock()
			return
		}
		if javaMajor := detectJavaMajor(); javaMajor > 0 && p.JavaVersion != javaMajor {
			p.JavaVersion = javaMajor
			_ = a.saveProfile(p)
		}
		if err := a.server.start(context.Background(), a, p); err != nil {
			a.appendServerLog("Wake-on-connect start failed: " + err.Error())
			w.mu.Lock()
			w.starting = false
			w.mu.Unlock()
			time.AfterFunc(3*time.Second, a.refreshWakeOnConnect)
			return
		}
		w.mu.Lock()
		w.starting = false
		w.mu.Unlock()
	}()
}

func (a *app) appendServerLog(line string) {
	a.server.mu.Lock()
	defer a.server.mu.Unlock()
	a.server.appendLogLocked(line)
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
	status.PlayIt = a.playItStatus(p, status.Running, s.logs)
	status.WorldSeed = worldSeedFromProfileAndLogs(p, lines)
	return status
}

func (a *app) playItStatus(p *profile, serverRunning bool, serverLines []string) playItStatus {
	status := playItStatus{}
	if p == nil || !p.PlayIt.Enabled {
		return status
	}
	lines := []string{}
	if serverRunning {
		lines = append(lines, serverLines...)
	}
	serverID := p.ID
	if serverID == "" {
		serverID = a.activeServerIDValue()
	}
	lines = append(lines, a.playit.logLinesForServer(serverID)...)
	status = playItStatusFromLogs(p, lines)
	if strings.EqualFold(p.Loader, "paper") {
		status.Mode = "plugin"
	} else {
		status.Mode = "standalone-agent"
	}
	agentRunning, agentStartedAt, errorText := a.playit.stateForServer(serverID, status.Error)
	status.Running = agentRunning
	status.StartedAt = agentStartedAt
	status.Error = errorText
	if agentRunning || agentStartedAt != nil {
		status.Mode = "standalone-agent"
	}
	status.AgentInstalled = a.playItStandaloneAgentInstalled()
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
	case strings.Contains(lower, "service manager not available") ||
		strings.Contains(lower, "systemctl"):
		return "The packaged PlayIt service wrapper cannot run in Termux. MineMux uses a standalone PlayIt daemon instead; restart PlayIt to switch over."
	case strings.Contains(lower, "unsupportedaddresstypeexception"):
		return "PlayIt hit an Android IPv6 socket issue. Set the PlayIt tunnel to IPv4 only, then restart the agent."
	case strings.Contains(lower, "address family not supported") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "failed to send initial ping") ||
		strings.Contains(lower, "failed to reload_control_addr"):
		return "PlayIt agent could not connect to tunnel control servers. Check mobile data/Wi-Fi, VPN, private DNS, and retry."
	case strings.Contains(lower, "failed to get control addresses") ||
		strings.Contains(lower, "failed when communicating with tunnel server") ||
		strings.Contains(lower, "failed to lookup address information") ||
		strings.Contains(lower, "dns error"):
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

func (s *serverProcess) clearTransientLogs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		return
	}
	s.logs = nil
	s.lastError = ""
	s.ready = false
	s.stopping = false
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

func (a *app) playItSocketPath() string {
	return filepath.Join(a.playItDir(), "playit.sock")
}

func (a *app) playItLogPath() string {
	return filepath.Join(a.playItDir(), "playitd.log")
}

func (a *app) playItManagedDaemonPath() string {
	return filepath.Join(a.playItDir(), "bin", "playitd")
}

func (a *app) playItManagedCLIPath() string {
	return filepath.Join(a.playItDir(), "bin", "playit-cli")
}

func (a *app) startPlayItAgentForProfile(p *profile) {
	if p == nil || !p.PlayIt.Enabled {
		return
	}
	serverID := p.ID
	if serverID == "" {
		serverID = a.activeServerIDValue()
	}
	if err := a.playit.start(context.Background(), a, serverID); err != nil {
		a.playit.setError(err.Error())
		a.server.mu.Lock()
		a.server.appendLogLocked("PlayIt agent failed: " + err.Error())
		a.server.mu.Unlock()
	}
}

func (p *playItAgentProcess) start(ctx context.Context, a *app, serverID string) error {
	p.mu.Lock()
	if (p.cmd != nil && p.cmd.Process != nil) || p.starting {
		if p.serverID == "" || p.serverID == serverID {
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		return errors.New("PlayIt agent is already running for another server")
	}
	p.starting = true
	p.serverID = serverID
	p.startedAt = nil
	p.lastError = ""
	p.logs = nil
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
	daemonPath := a.playItDaemonPath()
	if _, err := os.Stat(daemonPath); err != nil {
		return errors.New("PlayIt standalone daemon is not installed")
	}
	if err := os.MkdirAll(a.playItDir(), 0o700); err != nil {
		return err
	}
	_ = os.Remove(a.playItSocketPath())
	cmd := exec.CommandContext(ctx, daemonPath, "--secret-path", a.playItSecretPath(), "--socket-path", a.playItSocketPath(), "--log-path", a.playItLogPath())
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
	p.serverID = serverID
	p.starting = false
	p.appendLogLocked("PlayIt agent started with pid " + strconv.Itoa(cmd.Process.Pid))
	p.appendLogLocked("Using PlayIt daemon from " + playItBinarySource(daemonPath))
	p.mu.Unlock()
	startFailed = false

	go p.capture(stdout)
	go p.capture(stderr)
	go p.wait(cmd, done)
	go p.attachPlayItCLI(ctx, a)
	return nil
}

func (p *playItAgentProcess) attachPlayItCLI(ctx context.Context, a *app) {
	cliPath := a.playItCLIPath()
	if _, err := os.Stat(cliPath); err != nil {
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(a.playItSocketPath()); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	cmd := exec.CommandContext(ctx, cliPath, "--socket-path", a.playItSocketPath(), "-s")
	cmd.Dir = a.playItDir()
	cmd.Env = append(cleanJavaEnv(os.Environ()),
		"XDG_CONFIG_HOME="+filepath.Join(a.home, ".config"),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.appendLog("PlayIt CLI attach failed: " + err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.appendLog("PlayIt CLI attach failed: " + err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		p.appendLog("PlayIt CLI attach failed: " + err.Error())
		return
	}
	p.mu.Lock()
	p.cliCmd = cmd
	p.mu.Unlock()
	p.appendLog("PlayIt CLI attached to local daemon from " + playItBinarySource(cliPath))
	go p.capture(stdout)
	go p.capture(stderr)
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		p.appendLog("PlayIt CLI attach stopped: " + err.Error())
	}
	p.mu.Lock()
	if p.cliCmd == cmd {
		p.cliCmd = nil
	}
	p.mu.Unlock()
}

func (p *playItAgentProcess) stop(timeout time.Duration) error {
	p.mu.Lock()
	cmd := p.cmd
	cliCmd := p.cliCmd
	done := p.done
	p.mu.Unlock()
	if cliCmd != nil && cliCmd.Process != nil {
		_ = cliCmd.Process.Kill()
	}
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
	if p.cliCmd != nil && p.cliCmd.Process != nil {
		_ = p.cliCmd.Process.Kill()
		p.cliCmd = nil
	}
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

func (p *playItAgentProcess) logLinesForServer(serverID string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if serverID == "" || p.serverID != serverID {
		return nil
	}
	out := make([]string, len(p.logs))
	copy(out, p.logs)
	return out
}

func (p *playItAgentProcess) stateForServer(serverID, existingError string) (bool, *time.Time, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if serverID == "" || p.serverID != serverID {
		return false, nil, existingError
	}
	running := p.starting || (p.cmd != nil && p.cmd.Process != nil)
	startedAt := p.startedAt
	err := existingError
	if p.lastError != "" {
		err = p.lastError
	}
	return running, startedAt, err
}

func (p *playItAgentProcess) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return
	}
	p.cliCmd = nil
	p.done = nil
	p.startedAt = nil
	p.serverID = ""
	p.lastError = ""
	p.logs = nil
	p.starting = false
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
	if err := ensureTermuxDNSConfigured(); err != nil && logLine != nil {
		logLine("Could not update Termux DNS resolver before PlayIt install: " + err.Error())
	}
	if a.playItPackagedAgentInstalled() || (a.playItStandaloneAgentInstalled() && a.playItPackagedCLIPath() != "") {
		return nil
	}
	if shouldPreferPackagedPlayItAgent() {
		if err := a.installPackagedPlayItAgent(logLine); err == nil && a.playItStandaloneAgentInstalled() {
			return nil
		} else if err != nil && logLine != nil && !a.playItStandaloneAgentInstalled() {
			logLine("Termux PlayIt package install failed: " + err.Error())
		}
	}
	if a.playItStandaloneAgentInstalled() {
		return nil
	}
	if err := a.installManagedPlayItAgent(logLine); err == nil {
		return nil
	} else if logLine != nil {
		logLine("Standalone PlayIt download failed: " + err.Error())
	}
	if a.playItManagedAgentInstalled() {
		return nil
	}
	if err := a.installPackagedPlayItAgent(logLine); err != nil {
		if termuxPkgPath() == "" {
			return errors.New("could not install PlayIt standalone agent; Termux pkg is unavailable and the direct download failed")
		}
		if !a.playItStandaloneAgentInstalled() {
			return fmt.Errorf("could not install PlayIt standalone agent; direct download failed and Termux package install failed: %w", err)
		}
	}
	if !a.playItStandaloneAgentInstalled() {
		return errors.New("could not install PlayIt standalone agent; direct download failed and the Termux package did not provide playitd/playit-cli")
	}
	return nil
}

func (a *app) installPackagedPlayItAgent(logLine func(string)) error {
	pkgPath := termuxPkgPath()
	if pkgPath == "" {
		return errors.New("Termux pkg is unavailable")
	}
	var installErrors []string
	for _, args := range [][]string{
		{"install", "-y", "tur-repo"},
		{"install", "-y", playItPackageName},
	} {
		if logLine != nil {
			logLine("Running pkg " + strings.Join(args, " "))
		}
		cmd := exec.Command(pkgPath, args...)
		cmd.Env = cleanJavaEnv(os.Environ())
		var captured strings.Builder
		logger := func(line string) {
			captured.WriteString(line)
			captured.WriteString("\n")
			if logLine != nil {
				logLine(line)
			}
		}
		if err := runCommandStreaming(cmd, logger); err != nil {
			detail := strings.TrimSpace(captured.String())
			if detail != "" {
				installErrors = append(installErrors, "pkg "+strings.Join(args, " ")+": "+lastLines(detail, 8))
			} else {
				installErrors = append(installErrors, "pkg "+strings.Join(args, " ")+": "+err.Error())
			}
			if logLine != nil {
				logLine("pkg " + strings.Join(args, " ") + " failed: " + err.Error())
			}
		}
	}
	if a.playItPackagedAgentInstalled() {
		return nil
	}
	if len(installErrors) > 0 {
		return fmt.Errorf("Termux package did not provide a complete PlayIt daemon/CLI set. Last output: %s", strings.Join(installErrors, " | "))
	}
	return errors.New("Termux package did not provide a complete PlayIt daemon/CLI set")
}

func (a *app) installManagedPlayItAgent(logLine func(string)) error {
	suffix, err := playItStandaloneAssetSuffix()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.playItManagedDaemonPath()), 0o755); err != nil {
		return err
	}
	assets := []struct {
		name   string
		url    string
		target string
	}{
		{name: "playitd", url: playItReleaseBaseURL + "/playit-linux-" + suffix, target: a.playItManagedDaemonPath()},
		{name: "playit-cli", url: playItReleaseBaseURL + "/playit-cli-linux-" + suffix, target: a.playItManagedCLIPath()},
	}
	for _, asset := range assets {
		if pathIsExecutable(asset.target) {
			continue
		}
		if logLine != nil {
			logLine("Downloading standalone " + asset.name)
		}
		if err := downloadFile(asset.url, asset.target); err != nil {
			return err
		}
		if err := os.Chmod(asset.target, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func playItStandaloneAssetSuffix() (string, error) {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64", nil
	case "arm":
		return "armv7", nil
	case "amd64":
		return "amd64", nil
	case "386":
		return "i686", nil
	default:
		return "", fmt.Errorf("unsupported PlayIt standalone architecture: %s", runtime.GOARCH)
	}
}

func (a *app) playItStandaloneAgentInstalled() bool {
	return a.playItDaemonPath() != "" && a.playItCLIPath() != ""
}

func (a *app) playItPackagedAgentInstalled() bool {
	return a.playItPackagedDaemonPath() != "" && a.playItPackagedCLIPath() != ""
}

func (a *app) playItManagedAgentInstalled() bool {
	return pathIsExecutable(a.playItManagedDaemonPath()) && pathIsExecutable(a.playItManagedCLIPath())
}

func (a *app) playItPackagedDaemonPath() string {
	return firstExecutablePath("playitd", "/data/data/com.termux/files/usr/bin/playitd")
}

func (a *app) playItPackagedCLIPath() string {
	return firstExecutablePath("playit-cli", "/data/data/com.termux/files/usr/bin/playit-cli", "playit", "/data/data/com.termux/files/usr/bin/playit")
}

func (a *app) playItDaemonPath() string {
	if path := a.playItPackagedDaemonPath(); path != "" {
		return path
	}
	if pathIsExecutable(a.playItManagedDaemonPath()) {
		return a.playItManagedDaemonPath()
	}
	return ""
}

func (a *app) playItCLIPath() string {
	if path := a.playItPackagedCLIPath(); path != "" {
		return path
	}
	if pathIsExecutable(a.playItManagedCLIPath()) {
		return a.playItManagedCLIPath()
	}
	return ""
}

func shouldPreferPackagedPlayItAgent() bool {
	if runtime.GOOS == "android" {
		return true
	}
	prefix := strings.ToLower(filepath.ToSlash(os.Getenv("PREFIX")))
	return strings.Contains(prefix, "com.termux") || termuxPkgPath() != ""
}

func termuxPkgPath() string {
	return firstExecutablePath("pkg", "/data/data/com.termux/files/usr/bin/pkg")
}

func playItBinarySource(path string) string {
	normalized := filepath.ToSlash(path)
	switch {
	case strings.Contains(normalized, "/data/data/com.termux/files/usr/bin/"):
		return "Termux package"
	case strings.Contains(normalized, "/playit/bin/"):
		return "managed standalone binary"
	case path != "":
		return filepath.Base(path)
	default:
		return "unknown source"
	}
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

func firstExecutablePath(candidates ...string) string {
	for _, candidate := range candidates {
		if strings.Contains(candidate, string(os.PathSeparator)) || strings.HasPrefix(candidate, "/") {
			if pathIsExecutable(candidate) {
				return candidate
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil && pathIsExecutable(path) {
			return path
		}
	}
	return ""
}

func pathIsExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
		s.appendLogLocked(s.runtimeLabelLocked() + " server stopped with error: " + err.Error())
		return
	}
	s.appendLogLocked(s.runtimeLabelLocked() + " server stopped")
}

func (s *serverProcess) runtimeLabelLocked() string {
	runtimeName := strings.TrimSpace(s.runtime)
	if runtimeName == "" {
		return "Minecraft"
	}
	return runtimeName
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

func (s *serverProcess) logLinesSince(since time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, line := range s.logs {
		stamp, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, stamp)
		if err != nil || ts.Before(since) {
			continue
		}
		out = append(out, rest)
	}
	return out
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
	loader := strings.TrimSpace(r.URL.Query().Get("loader"))
	if loader == "" {
		loader = "paper"
	}
	versions, source, warning := minecraftVersionOptionsForLoader(loader, detectJavaMajor())
	writeJSON(w, http.StatusOK, map[string]any{
		"minecraftVersions": versions,
		"loaders":           loaderOptions(),
		"loader":            loader,
		"source":            source,
		"warning":           warning,
	})
}

func (a *app) handleMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	loader := strings.TrimSpace(r.URL.Query().Get("loader"))
	if loader == "" {
		loader = "paper"
	}
	javaMajor := detectJavaMajor()
	if value := strings.TrimSpace(r.URL.Query().Get("javaMajor")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			javaMajor = parsed
		}
	}
	versions, source, warning := minecraftVersionOptionsForLoader(loader, javaMajor)
	writeJSON(w, http.StatusOK, map[string]any{
		"versions":  versions,
		"javaMajor": javaMajor,
		"loader":    loader,
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
	a.playit.reset()
	a.server.clearTransientLogs()
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
		a.playit.reset()
		a.server.clearTransientLogs()
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
	if strings.TrimSpace(req.WorldUploadID) != "" {
		a.appendSetupLog("Installing uploaded world")
		if err := a.installSetupWorldUpload(requestedServerID, req.WorldUploadID); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
			return
		}
		p.Seed = ""
		delete(p.Properties, "level-seed")
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
	if strings.EqualFold(p.Loader, "fabric") && !setupProjectSelected(projects, "fabric-api") {
		projects = append([]string{"fabric-api"}, projects...)
		a.appendSetupLog("Fabric runtime selected; adding fabric-api.")
	}
	if req.PlayIt.Enabled && strings.EqualFold(p.Loader, "paper") {
		if err := a.installPlayItPlugin(p); err != nil {
			a.appendSetupLog("PlayIt plugin install failed: " + err.Error())
			a.appendSetupLog("Standalone PlayIt agent can be started from the dashboard if needed.")
		}
		a.appendSetupLog("If PlayIt shows IPv6/control-channel errors on Android, set the PlayIt agent/tunnel to IPv4 only in the PlayIt dashboard.")
		if req.Crossplay.Enabled {
			a.appendSetupLog("PlayIt plugin handles Java TCP. Bedrock UDP needs a separate PlayIt agent/tunnel path.")
		}
	} else if req.PlayIt.Enabled {
		a.appendSetupLog("PlayIt enabled for " + p.Loader + "; MineMux will use the standalone PlayIt agent for this runtime.")
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

func setupProjectSelected(projects []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, project := range projects {
		if strings.EqualFold(strings.TrimSpace(project), needle) {
			return true
		}
	}
	return false
}

func (a *app) ensureFabricRuntimeDependenciesBeforeStart(p *profile) error {
	if p == nil || !strings.EqualFold(p.Loader, "fabric") {
		return nil
	}
	serverID := p.ID
	if serverID == "" {
		serverID = a.activeServerIDValue()
	}
	if serverModJarContains(a.serverDir(serverID), "fabric-api", "fabric_api") {
		return nil
	}
	a.server.mu.Lock()
	a.server.appendLogLocked("Fabric server is missing Fabric API; installing fabric-api before start.")
	a.server.mu.Unlock()
	if _, err := a.installModrinthProjectFor(serverID, p, "fabric-api", "", map[string]bool{}); err != nil {
		return fmt.Errorf("Fabric API is required for MineMux Fabric servers, but MineMux could not install it automatically: %w", err)
	}
	a.server.mu.Lock()
	a.server.appendLogLocked("Installed fabric-api for Fabric server.")
	a.server.mu.Unlock()
	return nil
}

func serverModJarContains(serverDir string, tokens ...string) bool {
	entries, err := os.ReadDir(filepath.Join(serverDir, "mods"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jar") {
			continue
		}
		name := strings.ToLower(entry.Name())
		for _, token := range tokens {
			if token != "" && strings.Contains(name, strings.ToLower(token)) {
				return true
			}
		}
	}
	return false
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

func (a *app) handleServerSetupWorldUpload(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".zip") {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "world uploads must be .zip files"})
		return
	}
	dir := filepath.Join(a.paths().Runtime, "setup-worlds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	id := safeName(strings.TrimSuffix(name, filepath.Ext(name))) + "-" + strconv.FormatInt(time.Now().Unix(), 10)
	target := filepath.Join(dir, id+".zip")
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
	writeJSON(w, http.StatusOK, map[string]string{"worldUploadId": id, "fileName": name})
}

func (a *app) installSetupWorldUpload(serverID, uploadID string) error {
	uploadID = safeName(uploadID)
	if uploadID == "" {
		return errors.New("world upload id is required")
	}
	source := filepath.Join(a.paths().Runtime, "setup-worlds", uploadID+".zip")
	if _, err := os.Stat(source); err != nil {
		return errors.New("uploaded world is missing; upload it again")
	}
	serverDir := a.serverDir(serverID)
	worldName := "world"
	targetWorld := filepath.Join(serverDir, worldName)
	if err := os.RemoveAll(targetWorld); err != nil {
		return err
	}
	if err := unzipWorldArchive(source, serverDir, worldName); err != nil {
		return err
	}
	_ = os.Remove(source)
	a.appendSetupLog("Uploaded world installed as " + worldName)
	return nil
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
	a.playit.reset()
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

func (a *app) handleTermuxInfo(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	shell := termuxShell()
	writeJSON(w, http.StatusOK, map[string]any{
		"home":  home,
		"cwd":   home,
		"shell": shell,
		"path":  os.Getenv("PATH"),
	})
}

func (a *app) handleTermuxCommand(w http.ResponseWriter, r *http.Request) {
	var req termuxCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "command is required"})
		return
	}
	home, _ := os.UserHomeDir()
	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		cwd = home
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		cwd = home
	}
	timeout := time.Duration(clampInt(req.TimeoutSeconds, 5, 120)) * time.Second
	if req.TimeoutSeconds <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	started := time.Now()
	cmd := exec.CommandContext(ctx, termuxShell(), "-lc", command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			stderr.WriteString("\nCommand timed out after " + timeout.String())
			exitCode = 124
		}
	}
	writeJSON(w, http.StatusOK, termuxCommandResponse{
		Command:    command,
		Cwd:        cwd,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: time.Since(started).Milliseconds(),
	})
}

func termuxShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	return "sh"
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
	a.playit.reset()
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
	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "missing file upload"})
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	paths := []string{}
	treeUpload := strings.TrimSpace(r.URL.Query().Get("tree")) == "1"
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
			return
		}
		relativeName := filepath.Base(header.Filename)
		if treeUpload {
			if safeRelative, ok := safeUploadRelativePath(header.Filename); ok {
				relativeName = safeRelative
			}
		}
		target := filepath.Join(dir, relativeName)
		cleanDir := filepath.Clean(dir)
		cleanTarget := filepath.Clean(target)
		if cleanTarget != cleanDir && !strings.HasPrefix(cleanTarget, cleanDir+string(os.PathSeparator)) {
			file.Close()
			writeJSON(w, http.StatusBadRequest, apiError{Error: "upload path escapes server directory"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			file.Close()
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		out, err := os.Create(cleanTarget)
		if err != nil {
			file.Close()
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		_, copyErr := io.Copy(out, file)
		closeErr := out.Close()
		file.Close()
		if copyErr != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: copyErr.Error()})
			return
		}
		if closeErr != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: closeErr.Error()})
			return
		}
		root := a.serverDir(id)
		paths = append(paths, filepath.ToSlash(strings.TrimPrefix(cleanTarget, root+string(os.PathSeparator))))
	}
	status := "uploaded"
	if len(paths) > 1 {
		status = fmt.Sprintf("uploaded %d files", len(paths))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "paths": paths, "path": firstString(paths)})
}

func safeUploadRelativePath(name string) (string, bool) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "\x00") {
		return "", false
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || filepath.IsAbs(cleaned) {
		return "", false
	}
	return cleaned, true
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

func (a *app) handleUpdatesPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := a.updatePlanFor(serverIDFromRequest(a, r), "")
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (a *app) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	var req updateApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	if id == a.activeServerIDValue() && a.server.status(a).Running {
		writeJSON(w, http.StatusConflict, apiError{Error: "stop the server before applying updates"})
		return
	}
	targetVersion := strings.TrimSpace(req.TargetMinecraftVersion)
	if !req.UpdateMinecraft {
		targetVersion = p.MinecraftVersion
	}
	plan, err := a.updatePlanFor(id, targetVersion)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	if req.UpdateMods {
		if missing := missingUpdateChoices(plan, req.ModChoices); len(missing) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "some add-ons need a choice before updating",
				"mods":  missing,
				"plan":  plan,
			})
			return
		}
	}

	results := []updateApplyResult{}
	backupID := ""
	backupCreated := false
	ensureBackup := func() error {
		if backupCreated {
			return nil
		}
		backup, err := a.createBackupFor(id, "before-update")
		if err != nil {
			return err
		}
		backupCreated = true
		backupID = backup.ID
		return nil
	}

	if req.UpdateMinecraft {
		if plan.Minecraft.TargetVersion == "" {
			writeJSON(w, http.StatusBadGateway, apiError{Error: "could not resolve a target Minecraft version"})
			return
		}
		if err := ensureBackup(); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
			return
		}
		javaMajor := detectJavaMajor()
		if javaMajor <= 0 {
			javaMajor = p.JavaVersion
		}
		p.MinecraftVersion = plan.Minecraft.TargetVersion
		if javaMajor > 0 {
			p.JavaVersion = javaMajor
		}
		resolved, err := a.installServerRuntimeFor(id, p, javaMajor)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
			return
		}
		if resolved != "" {
			p.MinecraftVersion = resolved
		}
		if err := a.saveProfileFor(id, p); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		if err := writeServerProperties(a.serverDir(id), p); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		if err := a.updateLockRuntime(id, p); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		results = append(results, updateApplyResult{
			Name:    "Minecraft",
			Action:  "updated",
			Message: "Runtime refreshed for Minecraft " + p.MinecraftVersion,
		})
	}

	if req.UpdateMods {
		if refreshed, err := a.loadProfileFor(id); err == nil {
			p = refreshed
		}
		modPlan, err := a.updatePlanFor(id, p.MinecraftVersion)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
			return
		}
		for _, mod := range modPlan.Mods {
			choice := choiceForUpdateMod(req.ModChoices, mod)
			if choice == "" {
				if mod.UpdateAvailable {
					choice = "update"
				} else {
					choice = "ignore"
				}
			}
			switch choice {
			case "update":
				if !mod.UpdateAvailable || mod.TargetVersionID == "" || mod.ProjectID == "" {
					results = append(results, updateApplyResult{Name: mod.CurrentName, FileName: mod.FileName, Action: "skipped", Message: "No compatible update is available"})
					continue
				}
				if err := ensureBackup(); err != nil {
					writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
					return
				}
				if err := a.removeLockedInstall(id, mod, true); err != nil {
					writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
					return
				}
				installed, err := a.installModrinthProjectFor(id, p, mod.ProjectID, mod.TargetVersionID, map[string]bool{})
				if err != nil {
					writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
					return
				}
				message := "Installed latest compatible version"
				if len(installed) > 1 {
					message = fmt.Sprintf("Installed %d add-ons including dependencies", len(installed))
				}
				results = append(results, updateApplyResult{Name: mod.CurrentName, FileName: mod.FileName, Action: "updated", Message: message})
			case "disable":
				if err := ensureBackup(); err != nil {
					writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
					return
				}
				if err := a.disableLockedInstall(id, mod); err != nil {
					writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
					return
				}
				results = append(results, updateApplyResult{Name: mod.CurrentName, FileName: mod.FileName, Action: "disabled", Message: "Moved to disabled-addons"})
			case "manual":
				results = append(results, updateApplyResult{Name: mod.CurrentName, FileName: mod.FileName, Action: "manual", Message: "Left installed for manual replacement"})
			default:
				results = append(results, updateApplyResult{Name: mod.CurrentName, FileName: mod.FileName, Action: "ignored", Message: "Left unchanged"})
			}
		}
	}

	finalPlan, err := a.updatePlanFor(id, "")
	if err != nil {
		finalPlan = plan
	}
	writeJSON(w, http.StatusOK, updateApplyResponse{
		ServerID: id,
		BackupID: backupID,
		Results:  results,
		Plan:     finalPlan,
	})
}

func (a *app) updatePlanFor(id, targetMinecraftVersion string) (updatePlanResponse, error) {
	id = normalizeServerID(id)
	p, err := a.loadProfileFor(id)
	if err != nil {
		return updatePlanResponse{}, errors.New("profile not found; run setup first")
	}
	javaMajor := detectJavaMajor()
	if javaMajor <= 0 {
		javaMajor = p.JavaVersion
	}
	target, source, warning := recommendedMinecraftUpdateTarget(p.Loader, javaMajor)
	if strings.TrimSpace(targetMinecraftVersion) != "" {
		target = strings.TrimSpace(targetMinecraftVersion)
		source = "selected"
	}
	current := strings.TrimSpace(p.MinecraftVersion)
	if current == "" {
		current = "latest-compatible"
	}
	if target == "" {
		target = current
	}
	targetProfile := *p
	targetProfile.MinecraftVersion = target
	runtimeRefresh := strings.TrimSpace(p.Loader) != "" && target != ""
	plan := updatePlanResponse{
		ServerID: id,
		Minecraft: updateRuntimePlan{
			CurrentVersion:          current,
			TargetVersion:           target,
			Loader:                  p.Loader,
			Source:                  source,
			Warning:                 warning,
			UpdateAvailable:         !strings.EqualFold(current, target),
			RuntimeRefreshAvailable: runtimeRefresh,
			Message:                 "Runtime/build will be refreshed with the existing installer.",
		},
		Mods:         []updateModPlan{},
		Blockers:     []updateModPlan{},
		RequiresStop: id == a.activeServerIDValue() && a.server.status(a).Running,
	}

	lock, err := a.loadLockfileFor(id)
	if err != nil {
		lock = lockfile{MinecraftVersion: p.MinecraftVersion, Loader: p.Loader, Installed: []lockedInstall{}}
	}
	for _, installed := range lock.Installed {
		if strings.TrimSpace(installed.FileName) == "" {
			continue
		}
		mod := updateModPlan{
			ProjectID:    installed.ProjectID,
			VersionID:    installed.VersionID,
			CurrentName:  firstNonEmpty(installed.Name, installed.FileName),
			FileName:     installed.FileName,
			TargetFolder: installed.TargetFolder,
			Source:       installed.Source,
			Status:       "current",
		}
		if !strings.EqualFold(installed.Source, "modrinth") || installed.ProjectID == "" {
			mod.Status = "manual"
			mod.Reason = "This add-on was not installed from Modrinth and cannot be checked automatically."
			plan.Mods = append(plan.Mods, mod)
			plan.Blockers = append(plan.Blockers, mod)
			continue
		}
		version, err := resolveModrinthVersion(&targetProfile, installed.ProjectID, "")
		if err != nil {
			mod.Status = "unavailable"
			mod.Reason = err.Error()
			plan.Mods = append(plan.Mods, mod)
			plan.Blockers = append(plan.Blockers, mod)
			continue
		}
		file, err := primaryModrinthFile(version)
		if err != nil {
			mod.Status = "unavailable"
			mod.Reason = err.Error()
			plan.Mods = append(plan.Mods, mod)
			plan.Blockers = append(plan.Blockers, mod)
			continue
		}
		mod.TargetVersionID = version.ID
		mod.TargetName = version.Name
		mod.TargetFileName = filepath.Base(file.Filename)
		mod.UpdateAvailable = installed.VersionID == "" || !strings.EqualFold(installed.VersionID, version.ID) || !strings.EqualFold(installed.FileName, mod.TargetFileName)
		if mod.UpdateAvailable {
			mod.Status = "available"
		}
		plan.Mods = append(plan.Mods, mod)
	}
	return plan, nil
}

func recommendedMinecraftUpdateTarget(loader string, javaMajor int) (string, string, string) {
	options, source, warning := minecraftVersionOptionsForLoader(loader, javaMajor)
	for _, option := range options {
		if option.Recommended {
			return option.ID, source, warning
		}
	}
	if len(options) > 0 {
		return options[0].ID, source, warning
	}
	return latestFallbackMinecraftVersion(javaMajor), "fallback", warning
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func missingUpdateChoices(plan updatePlanResponse, choices map[string]string) []string {
	missing := []string{}
	for _, mod := range plan.Mods {
		if mod.Status != "unavailable" && mod.Status != "manual" {
			continue
		}
		if _, ok := lookupUpdateChoice(choices, mod); ok {
			continue
		}
		missing = append(missing, firstNonEmpty(mod.CurrentName, mod.FileName))
	}
	return missing
}

func choiceForUpdateMod(choices map[string]string, mod updateModPlan) string {
	choice, ok := lookupUpdateChoice(choices, mod)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "update", "disable", "manual", "upload", "upload-manually":
		if strings.EqualFold(choice, "upload") || strings.EqualFold(choice, "upload-manually") {
			return "manual"
		}
		return strings.ToLower(strings.TrimSpace(choice))
	default:
		return "ignore"
	}
}

func lookupUpdateChoice(choices map[string]string, mod updateModPlan) (string, bool) {
	if len(choices) == 0 {
		return "", false
	}
	keys := []string{mod.ProjectID, mod.FileName, mod.TargetFolder + "/" + mod.FileName}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if choice, ok := choices[key]; ok {
			return choice, true
		}
	}
	return "", false
}

func (a *app) updateLockRuntime(id string, p *profile) error {
	lock, _ := a.loadLockfileFor(id)
	lock.MinecraftVersion = p.MinecraftVersion
	lock.Loader = p.Loader
	return a.saveLockfileFor(id, lock)
}

func (a *app) removeLockedInstall(id string, mod updateModPlan, removeFile bool) error {
	lock, _ := a.loadLockfileFor(id)
	next := make([]lockedInstall, 0, len(lock.Installed))
	for _, installed := range lock.Installed {
		matched := installed.FileName == mod.FileName && installed.TargetFolder == mod.TargetFolder
		if mod.ProjectID != "" && installed.ProjectID == mod.ProjectID {
			matched = true
		}
		if !matched {
			next = append(next, installed)
			continue
		}
		if removeFile {
			_ = os.Remove(filepath.Join(a.serverDir(id), installed.TargetFolder, installed.FileName))
		}
	}
	lock.Installed = next
	return a.saveLockfileFor(id, lock)
}

func (a *app) disableLockedInstall(id string, mod updateModPlan) error {
	source := filepath.Join(a.serverDir(id), mod.TargetFolder, mod.FileName)
	if _, err := os.Stat(source); err == nil {
		disabledDir := filepath.Join(a.serverDir(id), "disabled-addons", mod.TargetFolder)
		if err := os.MkdirAll(disabledDir, 0o755); err != nil {
			return err
		}
		target := filepath.Join(disabledDir, mod.FileName)
		if _, err := os.Stat(target); err == nil {
			target = filepath.Join(disabledDir, strings.TrimSuffix(mod.FileName, filepath.Ext(mod.FileName))+"-"+time.Now().UTC().Format("20060102150405")+filepath.Ext(mod.FileName))
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return a.removeLockedInstall(id, mod, false)
}

func (a *app) handleWorldMap(w http.ResponseWriter, r *http.Request) {
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "server profile not found; run setup first"})
		return
	}
	dimension := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dimension")))
	if dimension == "" {
		dimension = "overworld"
	}
	if dimension != "overworld" && dimension != "nether" && dimension != "end" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "dimension must be overworld, nether, or end"})
		return
	}
	detailRequested := queryBool(r, "detail")
	limitMin := worldMapMinLimit
	limitMax := worldMapMaxLimit
	if detailRequested {
		limitMin = 1
		limitMax = worldMapDetailLimit
	}
	limit := clampInt(queryInt(r, "limit", worldMapDefaultLimit), limitMin, limitMax)
	worldName := worldFolderName(p)
	available := a.worldDimensions(id, worldName)
	regionDir := a.worldRegionDir(id, worldName, dimension)
	refs, err := collectWorldChunkRefs(regionDir)
	if err != nil {
		writeJSON(w, http.StatusOK, worldMapResponse{
			ServerID:  id,
			Dimension: dimension,
			WorldName: worldName,
			Available: available,
			Chunks:    []worldMapChunk{},
			Limit:     limit,
			Mode:      "overview",
			Warning:   err.Error(),
		})
		return
	}
	totalChunks := len(refs)
	fullBounds := worldBoundsForRefs(refs)
	viewportFiltered := false
	if hasWorldMapViewport(r) {
		minX := queryInt(r, "minX", 0)
		maxX := queryInt(r, "maxX", 0)
		minZ := queryInt(r, "minZ", 0)
		maxZ := queryInt(r, "maxZ", 0)
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		if minZ > maxZ {
			minZ, maxZ = maxZ, minZ
		}
		filtered := refs[:0]
		for _, ref := range refs {
			if ref.ChunkX >= minX && ref.ChunkX <= maxX && ref.ChunkZ >= minZ && ref.ChunkZ <= maxZ {
				filtered = append(filtered, ref)
			}
		}
		refs = filtered
		viewportFiltered = true
	}
	detail := detailRequested || limit <= worldMapDetailLimit
	truncated := len(refs) > limit
	if truncated {
		if !viewportFiltered {
			sortWorldChunkRefsBySpawn(refs)
		}
		refs = refs[:limit]
	}
	chunks := make([]worldMapChunk, 0, len(refs))
	for _, ref := range refs {
		chunk := worldMapChunk{X: ref.ChunkX, Z: ref.ChunkZ}
		if detail {
			chunk.Pixels = loadWorldChunkPixels(ref, dimension)
		} else {
			chunk.Color = overviewChunkColor(ref, dimension)
		}
		chunks = append(chunks, chunk)
	}
	warning := ""
	if truncated {
		if viewportFiltered {
			warning = "Showing " + strconv.Itoa(limit) + " chunks from the visible area. Zoom in closer to load finer detail."
		} else {
			warning = "Showing nearest " + strconv.Itoa(limit) + " of " + strconv.Itoa(totalChunks) + " generated chunks."
		}
	}
	mode := "overview"
	if detail {
		mode = "detail"
	}
	writeJSON(w, http.StatusOK, worldMapResponse{
		ServerID:    id,
		Dimension:   dimension,
		WorldName:   worldName,
		Available:   available,
		Chunks:      chunks,
		Bounds:      fullBounds,
		Limit:       limit,
		TotalChunks: totalChunks,
		Mode:        mode,
		Truncated:   truncated,
		Warning:     warning,
	})
}

func worldBoundsForRefs(refs []worldRegionChunkRef) worldMapBounds {
	bounds := worldMapBounds{}
	for index, ref := range refs {
		if index == 0 || ref.ChunkX < bounds.MinX {
			bounds.MinX = ref.ChunkX
		}
		if index == 0 || ref.ChunkX > bounds.MaxX {
			bounds.MaxX = ref.ChunkX
		}
		if index == 0 || ref.ChunkZ < bounds.MinZ {
			bounds.MinZ = ref.ChunkZ
		}
		if index == 0 || ref.ChunkZ > bounds.MaxZ {
			bounds.MaxZ = ref.ChunkZ
		}
	}
	return bounds
}

func queryBool(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func hasWorldMapViewport(r *http.Request) bool {
	query := r.URL.Query()
	for _, key := range []string{"minX", "maxX", "minZ", "maxZ"} {
		if strings.TrimSpace(query.Get(key)) == "" {
			return false
		}
	}
	return true
}

func (a *app) handleWorldPlayers(w http.ResponseWriter, r *http.Request) {
	status := a.server.status(a)
	if !status.Running || !status.Ready {
		writeJSON(w, http.StatusOK, worldPlayersResponse{
			Online:      0,
			Players:     []worldPlayerMarker{},
			RefreshedAt: time.Now().UTC(),
		})
		return
	}
	names := playersFromLogs(a.mergedServerLogLines(maxLogLines))
	markers := make([]worldPlayerMarker, 0, len(names))
	if len(names) == 0 {
		writeJSON(w, http.StatusOK, worldPlayersResponse{
			Online:      0,
			Players:     markers,
			RefreshedAt: time.Now().UTC(),
		})
		return
	}
	started := time.Now().UTC()
	for _, name := range names {
		if !validMinecraftPlayerName(name) {
			continue
		}
		_ = a.server.send("data get entity " + name + " Pos")
		_ = a.server.send("data get entity " + name + " Dimension")
	}
	deadline := time.Now().Add(1400 * time.Millisecond)
	parsed := map[string]*worldPlayerMarker{}
	for time.Now().Before(deadline) {
		lines := a.server.logLinesSince(started)
		for _, line := range lines {
			parseWorldPlayerDataLine(line, parsed)
		}
		complete := 0
		for _, name := range names {
			if marker := parsed[name]; marker != nil && marker.Available && marker.Dimension != "" {
				complete++
			}
		}
		if complete >= len(names) {
			break
		}
		time.Sleep(120 * time.Millisecond)
	}
	for _, name := range names {
		marker := worldPlayerMarker{Name: name, Available: false, Error: "Position unavailable"}
		if parsedMarker := parsed[name]; parsedMarker != nil {
			marker = *parsedMarker
			marker.Name = name
			if marker.Available {
				marker.ChunkX = floorDivFloat(marker.X, 16)
				marker.ChunkZ = floorDivFloat(marker.Z, 16)
				marker.Error = ""
			}
			if marker.Dimension == "" {
				marker.Dimension = "overworld"
			}
		}
		markers = append(markers, marker)
	}
	writeJSON(w, http.StatusOK, worldPlayersResponse{
		Online:      len(markers),
		Players:     markers,
		RefreshedAt: time.Now().UTC(),
	})
}

func validMinecraftPlayerName(name string) bool {
	if name == "" || len(name) > 16 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func parseWorldPlayerDataLine(line string, markers map[string]*worldPlayerMarker) {
	if !strings.Contains(line, " has the following entity data:") {
		return
	}
	prefix, data, ok := strings.Cut(line, " has the following entity data:")
	if !ok {
		return
	}
	fields := strings.Fields(strings.TrimSpace(prefix))
	if len(fields) == 0 {
		return
	}
	name := fields[len(fields)-1]
	if !validMinecraftPlayerName(name) {
		return
	}
	marker := markers[name]
	if marker == nil {
		marker = &worldPlayerMarker{Name: name}
		markers[name] = marker
	}
	data = strings.TrimSpace(data)
	if x, y, z, ok := parseMinecraftPosData(data); ok {
		marker.X = x
		marker.Y = y
		marker.Z = z
		marker.Available = true
		return
	}
	if dimension := normalizeMinecraftDimension(data); dimension != "" {
		marker.Dimension = dimension
	}
}

func parseMinecraftPosData(data string) (float64, float64, float64, bool) {
	re := regexp.MustCompile(`\[\s*([-+]?[0-9]+(?:\.[0-9]+)?)[dDfF]?\s*,\s*([-+]?[0-9]+(?:\.[0-9]+)?)[dDfF]?\s*,\s*([-+]?[0-9]+(?:\.[0-9]+)?)[dDfF]?\s*\]`)
	match := re.FindStringSubmatch(data)
	if len(match) != 4 {
		return 0, 0, 0, false
	}
	x, errX := strconv.ParseFloat(match[1], 64)
	y, errY := strconv.ParseFloat(match[2], 64)
	z, errZ := strconv.ParseFloat(match[3], 64)
	return x, y, z, errX == nil && errY == nil && errZ == nil
}

func normalizeMinecraftDimension(data string) string {
	lower := strings.ToLower(data)
	switch {
	case strings.Contains(lower, "minecraft:the_nether") || strings.Contains(lower, "the_nether"):
		return "nether"
	case strings.Contains(lower, "minecraft:the_end") || strings.Contains(lower, "the_end"):
		return "end"
	case strings.Contains(lower, "minecraft:overworld") || strings.Contains(lower, "overworld"):
		return "overworld"
	default:
		return ""
	}
}

func floorDivFloat(value float64, divisor int) int {
	return int(math.Floor(value / float64(divisor)))
}

func sortWorldChunkRefsBySpawn(refs []worldRegionChunkRef) {
	sort.Slice(refs, func(i, j int) bool {
		di := refs[i].ChunkX*refs[i].ChunkX + refs[i].ChunkZ*refs[i].ChunkZ
		dj := refs[j].ChunkX*refs[j].ChunkX + refs[j].ChunkZ*refs[j].ChunkZ
		if di != dj {
			return di < dj
		}
		if refs[i].ChunkZ != refs[j].ChunkZ {
			return refs[i].ChunkZ < refs[j].ChunkZ
		}
		return refs[i].ChunkX < refs[j].ChunkX
	})
}

func (a *app) handleWorldTrim(w http.ResponseWriter, r *http.Request) {
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "server profile not found; run setup first"})
		return
	}
	if id == a.activeServerIDValue() && a.server.status(a).Running {
		writeJSON(w, http.StatusConflict, apiError{Error: "stop the server before trimming the world"})
		return
	}
	var req worldTrimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	dimension := strings.ToLower(strings.TrimSpace(req.Dimension))
	if dimension == "" {
		dimension = "overworld"
	}
	if dimension != "overworld" && dimension != "nether" && dimension != "end" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "dimension must be overworld, nether, or end"})
		return
	}
	areas, err := normalizeWorldTrimAreas(req.Areas)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	worldName := worldFolderName(p)
	regionDir := a.worldRegionDir(id, worldName, dimension)
	refs, err := collectWorldChunkRefs(regionDir)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: err.Error()})
		return
	}
	stats := worldTrimPreview(refs, areas)
	if stats.KeptChunks == 0 {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "selected keep areas do not include any generated chunks"})
		return
	}
	backup, err := a.createBackupFor(id, "before-world-trim")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
		return
	}
	stats, err = trimWorldRegions(refs, areas)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, worldTrimResponse{
		ServerID:             id,
		Dimension:            dimension,
		WorldName:            worldName,
		BackupID:             backup.ID,
		BeforeChunks:         stats.BeforeChunks,
		KeptChunks:           stats.KeptChunks,
		DeletedChunks:        stats.DeletedChunks,
		RegionFilesRemoved:   stats.RegionFilesRemoved,
		RegionFilesCompacted: stats.RegionFilesCompacted,
	})
}

func (a *app) handleWorldModsScan(w http.ResponseWriter, r *http.Request) {
	a.handleWorldModsScanOrCleanup(w, r, false)
}

func (a *app) handleWorldModsCleanup(w http.ResponseWriter, r *http.Request) {
	a.handleWorldModsScanOrCleanup(w, r, true)
}

func (a *app) handleWorldModsScanOrCleanup(w http.ResponseWriter, r *http.Request, cleanup bool) {
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "server profile not found; run setup first"})
		return
	}
	if cleanup && id == a.activeServerIDValue() && a.server.status(a).Running {
		writeJSON(w, http.StatusConflict, apiError{Error: "stop the server before cleaning mod blocks from the world"})
		return
	}
	var req modWorldScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	namespaces, namespaceSet, err := normalizeModNamespaces(req.Namespaces)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	dimensions, err := normalizeWorldDimensions(req.Dimensions)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	worldName := worldFolderName(p)
	response := modWorldScanResponse{
		ServerID:   id,
		WorldName:  worldName,
		Namespaces: namespaces,
		Dimensions: []modWorldDimensionReport{},
		Cleaned:    cleanup,
	}
	if cleanup {
		preview, err := a.scanWorldModRefs(id, worldName, dimensions, namespaceSet, false)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		if preview.TotalBlocks+preview.TotalBlockEntities+preview.TotalEntities == 0 {
			writeJSON(w, http.StatusOK, preview)
			return
		}
		backup, err := a.createBackupFor(id, "before-mod-world-cleanup")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: "restore point failed: " + err.Error()})
			return
		}
		response.BackupID = backup.ID
	}
	scanned, err := a.scanWorldModRefs(id, worldName, dimensions, namespaceSet, cleanup)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	scanned.BackupID = response.BackupID
	scanned.Cleaned = cleanup
	writeJSON(w, http.StatusOK, scanned)
}

func (a *app) scanWorldModRefs(serverID, worldName string, dimensions []string, namespaces map[string]bool, cleanup bool) (modWorldScanResponse, error) {
	response := modWorldScanResponse{
		ServerID:   serverID,
		WorldName:  worldName,
		Namespaces: sortedNamespaceKeys(namespaces),
		Dimensions: []modWorldDimensionReport{},
		Cleaned:    cleanup,
	}
	for _, dimension := range dimensions {
		report, err := scanWorldModDimension(a.worldRegionDir(serverID, worldName, dimension), dimension, namespaces, cleanup)
		if err != nil && report.Warning == "" {
			report.Warning = err.Error()
		}
		response.Dimensions = append(response.Dimensions, report)
		response.TotalChunksScanned += report.ChunksScanned
		response.TotalChunksMatched += report.ChunksMatched
		response.TotalBlocks += report.Blocks
		response.TotalBlockEntities += report.BlockEntities
		response.TotalEntities += report.Entities
	}
	return response, nil
}

func scanWorldModDimension(regionDir, dimension string, namespaces map[string]bool, cleanup bool) (modWorldDimensionReport, error) {
	report := modWorldDimensionReport{Dimension: dimension}
	entries, err := os.ReadDir(regionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.Warning = "dimension has no generated region files"
			return report, nil
		}
		return report, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "r.") || !strings.HasSuffix(entry.Name(), ".mca") {
			continue
		}
		report.RegionFiles++
		path := filepath.Join(regionDir, entry.Name())
		regionReport, changed, err := scanRegionModRefs(path, namespaces, cleanup)
		if err != nil {
			if report.Warning == "" {
				report.Warning = err.Error()
			}
			continue
		}
		report.ChunksScanned += regionReport.ChunksScanned
		report.ChunksMatched += regionReport.ChunksMatched
		report.ChunksCleaned += regionReport.ChunksCleaned
		report.Blocks += regionReport.Blocks
		report.BlockEntities += regionReport.BlockEntities
		report.Entities += regionReport.Entities
		if changed {
			report.RegionFilesChanged++
		}
	}
	return report, nil
}

func scanRegionModRefs(path string, namespaces map[string]bool, cleanup bool) (modWorldDimensionReport, bool, error) {
	rawChunks, timestamps, err := readRegionRawChunks(path)
	if err != nil {
		return modWorldDimensionReport{}, false, err
	}
	report := modWorldDimensionReport{}
	changed := false
	for index, raw := range rawChunks {
		payload, err := decodeRegionRawChunk(raw)
		if err != nil {
			continue
		}
		root, err := readNBTTree(payload)
		if err != nil {
			continue
		}
		chunkReport := scanNBTModRefs(root, namespaces, cleanup)
		report.ChunksScanned++
		if chunkReport.Blocks+chunkReport.BlockEntities+chunkReport.Entities > 0 {
			report.ChunksMatched++
		}
		report.Blocks += chunkReport.Blocks
		report.BlockEntities += chunkReport.BlockEntities
		report.Entities += chunkReport.Entities
		if cleanup && chunkReport.Changed {
			nextPayload, err := writeNBTTree(root)
			if err != nil {
				return report, changed, err
			}
			nextRaw, err := encodeRegionRawChunk(nextPayload)
			if err != nil {
				return report, changed, err
			}
			rawChunks[index] = nextRaw
			report.ChunksCleaned++
			changed = true
		}
	}
	if cleanup && changed {
		if err := writeRegionRawChunks(path, rawChunks, timestamps); err != nil {
			return report, changed, err
		}
	}
	return report, changed, nil
}

func normalizeModNamespaces(input []string) ([]string, map[string]bool, error) {
	set := map[string]bool{}
	for _, raw := range input {
		for _, part := range strings.Split(raw, ",") {
			value := strings.ToLower(strings.TrimSpace(part))
			value = strings.TrimSuffix(value, ":")
			if strings.Contains(value, ":") {
				value = strings.SplitN(value, ":", 2)[0]
			}
			if value == "" {
				continue
			}
			if value == "minecraft" {
				return nil, nil, errors.New("minecraft namespace cannot be cleaned")
			}
			if !regexp.MustCompile(`^[a-z0-9_.-]+$`).MatchString(value) {
				return nil, nil, fmt.Errorf("invalid mod namespace: %s", value)
			}
			set[value] = true
		}
	}
	if len(set) == 0 {
		return nil, nil, errors.New("at least one mod namespace is required")
	}
	return sortedNamespaceKeys(set), set, nil
}

func sortedNamespaceKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeWorldDimensions(input []string) ([]string, error) {
	if len(input) == 0 {
		return []string{"overworld", "nether", "end"}, nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, value := range input {
		dimension := strings.ToLower(strings.TrimSpace(value))
		if dimension == "" {
			continue
		}
		if dimension != "overworld" && dimension != "nether" && dimension != "end" {
			return nil, errors.New("dimension must be overworld, nether, or end")
		}
		if !seen[dimension] {
			out = append(out, dimension)
			seen[dimension] = true
		}
	}
	if len(out) == 0 {
		return []string{"overworld", "nether", "end"}, nil
	}
	return out, nil
}

func normalizeWorldTrimAreas(input []worldTrimArea) ([]worldTrimArea, error) {
	if len(input) == 0 {
		return nil, errors.New("at least one keep area is required")
	}
	if len(input) > 64 {
		return nil, errors.New("too many keep areas; use 64 or fewer")
	}
	areas := make([]worldTrimArea, 0, len(input))
	for _, area := range input {
		if area.MinChunkX > area.MaxChunkX {
			area.MinChunkX, area.MaxChunkX = area.MaxChunkX, area.MinChunkX
		}
		if area.MinChunkZ > area.MaxChunkZ {
			area.MinChunkZ, area.MaxChunkZ = area.MaxChunkZ, area.MinChunkZ
		}
		if area.MaxChunkX-area.MinChunkX > 4096 || area.MaxChunkZ-area.MinChunkZ > 4096 {
			return nil, errors.New("keep area is too large")
		}
		areas = append(areas, area)
	}
	return areas, nil
}

func worldTrimPreview(refs []worldRegionChunkRef, areas []worldTrimArea) worldTrimStats {
	stats := worldTrimStats{BeforeChunks: len(refs)}
	for _, ref := range refs {
		if chunkInTrimAreas(ref.ChunkX, ref.ChunkZ, areas) {
			stats.KeptChunks++
		}
	}
	stats.DeletedChunks = stats.BeforeChunks - stats.KeptChunks
	return stats
}

func trimWorldRegions(refs []worldRegionChunkRef, areas []worldTrimArea) (worldTrimStats, error) {
	stats := worldTrimPreview(refs, areas)
	if stats.KeptChunks == 0 {
		return stats, errors.New("selected keep areas do not include any generated chunks")
	}
	grouped := map[string][]worldRegionChunkRef{}
	for _, ref := range refs {
		grouped[ref.RegionPath] = append(grouped[ref.RegionPath], ref)
	}
	for path, regionRefs := range grouped {
		keep := map[int]bool{}
		for _, ref := range regionRefs {
			if !chunkInTrimAreas(ref.ChunkX, ref.ChunkZ, areas) {
				continue
			}
			keep[ref.LocalX+ref.LocalZ*32] = true
		}
		switch {
		case len(keep) == len(regionRefs):
			continue
		case len(keep) == 0:
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return stats, err
			}
			stats.RegionFilesRemoved++
		default:
			if err := compactRegionFile(path, keep); err != nil {
				return stats, err
			}
			stats.RegionFilesCompacted++
		}
	}
	return stats, nil
}

func chunkInTrimAreas(chunkX, chunkZ int, areas []worldTrimArea) bool {
	for _, area := range areas {
		if chunkX >= area.MinChunkX && chunkX <= area.MaxChunkX && chunkZ >= area.MinChunkZ && chunkZ <= area.MaxChunkZ {
			return true
		}
	}
	return false
}

func compactRegionFile(path string, keep map[int]bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 8192 {
		return fmt.Errorf("region file %s is too small", filepath.Base(path))
	}
	next := make([]byte, 8192)
	for index := 0; index < 1024; index++ {
		if !keep[index] {
			continue
		}
		location := binary.BigEndian.Uint32(data[index*4 : index*4+4])
		offset := int(location >> 8)
		sectors := int(location & 0xff)
		if offset < 2 || sectors <= 0 {
			continue
		}
		start := offset * 4096
		end := start + sectors*4096
		if start < 0 || start >= len(data) || end > len(data) {
			return fmt.Errorf("region file %s has an invalid chunk offset", filepath.Base(path))
		}
		raw := data[start:end]
		needed := len(raw)
		if len(raw) >= 5 {
			length := int(binary.BigEndian.Uint32(raw[:4]))
			if length > 0 && length+4 <= len(raw) {
				needed = alignToSector(length + 4)
			}
		}
		if needed <= 0 {
			continue
		}
		newSectors := needed / 4096
		if newSectors > 255 {
			return fmt.Errorf("chunk in %s is too large to compact safely", filepath.Base(path))
		}
		payload := make([]byte, needed)
		copy(payload, raw[:minInt(needed, len(raw))])
		newOffset := len(next) / 4096
		binary.BigEndian.PutUint32(next[index*4:index*4+4], uint32(newOffset<<8|newSectors))
		copy(next[4096+index*4:4096+index*4+4], data[4096+index*4:4096+index*4+4])
		next = append(next, payload...)
	}
	if len(next) == 8192 {
		return fmt.Errorf("region file %s would be empty after compacting", filepath.Base(path))
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".trim-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(next); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	backupPath := path + ".before-trim-" + time.Now().UTC().Format("20060102150405")
	if err := os.Rename(path, backupPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func alignToSector(size int) int {
	if size <= 0 {
		return 0
	}
	if size%4096 == 0 {
		return size
	}
	return (size/4096 + 1) * 4096
}

func worldFolderName(p *profile) string {
	if p != nil && p.Properties != nil {
		if value := strings.TrimSpace(p.Properties["level-name"]); value != "" {
			clean := filepath.Base(filepath.Clean(value))
			if clean != "." && clean != string(os.PathSeparator) && clean != "" {
				return clean
			}
		}
	}
	return "world"
}

func (a *app) worldDimensions(serverID, worldName string) []worldDimensionInfo {
	dimensions := []worldDimensionInfo{
		{ID: "overworld", Name: "Overworld"},
		{ID: "nether", Name: "Nether"},
		{ID: "end", Name: "End"},
	}
	for i := range dimensions {
		dir := a.worldRegionDir(serverID, worldName, dimensions[i].ID)
		count := countRegionFiles(dir)
		dimensions[i].Available = count > 0
		dimensions[i].RegionFiles = count
	}
	return dimensions
}

func (a *app) worldRegionDir(serverID, worldName, dimension string) string {
	worldRoot := filepath.Join(a.serverDir(serverID), worldName)
	candidates := []string{}
	switch dimension {
	case "nether":
		candidates = append(candidates,
			filepath.Join(worldRoot, "DIM-1", "region"),
			filepath.Join(worldRoot, "dimensions", "minecraft", "the_nether", "region"),
		)
	case "end":
		candidates = append(candidates,
			filepath.Join(worldRoot, "DIM1", "region"),
			filepath.Join(worldRoot, "dimensions", "minecraft", "the_end", "region"),
		)
	default:
		candidates = append(candidates,
			filepath.Join(worldRoot, "region"),
			filepath.Join(worldRoot, "dimensions", "minecraft", "overworld", "region"),
		)
	}
	for _, dir := range candidates {
		if countRegionFiles(dir) > 0 {
			return dir
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(worldRoot, "region")
}

func countRegionFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mca") {
			count++
		}
	}
	return count
}

func collectWorldChunkRefs(regionDir string) ([]worldRegionChunkRef, error) {
	entries, err := os.ReadDir(regionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no generated region files found for this dimension")
		}
		return nil, err
	}
	refs := []worldRegionChunkRef{}
	header := make([]byte, 4096)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "r.") || !strings.HasSuffix(entry.Name(), ".mca") {
			continue
		}
		rx, rz, ok := parseRegionFileName(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(regionDir, entry.Name())
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := io.ReadFull(file, header); err != nil {
			_ = file.Close()
			continue
		}
		_ = file.Close()
		for localZ := 0; localZ < 32; localZ++ {
			for localX := 0; localX < 32; localX++ {
				index := localX + localZ*32
				offset := binary.BigEndian.Uint32(header[index*4 : index*4+4])
				if offset>>8 == 0 {
					continue
				}
				refs = append(refs, worldRegionChunkRef{
					RegionPath: path,
					RegionX:    rx,
					RegionZ:    rz,
					LocalX:     localX,
					LocalZ:     localZ,
					ChunkX:     rx*32 + localX,
					ChunkZ:     rz*32 + localZ,
				})
			}
		}
	}
	if len(refs) == 0 {
		return nil, errors.New("no generated chunks found for this dimension")
	}
	return refs, nil
}

func parseRegionFileName(name string) (int, int, bool) {
	name = strings.TrimSuffix(strings.TrimPrefix(name, "r."), ".mca")
	parts := strings.Split(name, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, errX := strconv.Atoi(parts[0])
	z, errZ := strconv.Atoi(parts[1])
	return x, z, errX == nil && errZ == nil
}

func loadWorldChunkPixels(ref worldRegionChunkRef, dimension string) []string {
	fallback := fallbackChunkPixels(dimension)
	payload, err := readRegionChunkPayload(ref)
	if err != nil {
		return fallback
	}
	root, err := readNBTCompound(payload)
	if err != nil {
		return fallback
	}
	pixels, err := chunkSurfacePixels(root, dimension)
	if err != nil {
		return fallback
	}
	return pixels
}

func readRegionChunkPayload(ref worldRegionChunkRef) ([]byte, error) {
	file, err := os.Open(ref.RegionPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 4096)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, err
	}
	index := ref.LocalX + ref.LocalZ*32
	location := binary.BigEndian.Uint32(header[index*4 : index*4+4])
	sector := int64(location >> 8)
	if sector == 0 {
		return nil, errors.New("chunk is not generated")
	}
	if _, err := file.Seek(sector*4096, io.SeekStart); err != nil {
		return nil, err
	}
	var lengthBytes [4]byte
	if _, err := io.ReadFull(file, lengthBytes[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint32(lengthBytes[:]))
	if length <= 1 || length > 16*1024*1024 {
		return nil, errors.New("invalid chunk length")
	}
	compression := make([]byte, 1)
	if _, err := io.ReadFull(file, compression); err != nil {
		return nil, err
	}
	compressed := make([]byte, length-1)
	if _, err := io.ReadFull(file, compressed); err != nil {
		return nil, err
	}
	var reader io.ReadCloser
	switch compression[0] {
	case 1:
		reader, err = gzip.NewReader(bytes.NewReader(compressed))
	case 2:
		reader, err = zlib.NewReader(bytes.NewReader(compressed))
	case 3:
		return compressed, nil
	default:
		return nil, errors.New("unsupported chunk compression")
	}
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func readRegionRawChunks(path string) (map[int][]byte, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if len(data) < 8192 {
		return nil, nil, fmt.Errorf("region file %s is too small", filepath.Base(path))
	}
	timestamps := append([]byte(nil), data[4096:8192]...)
	chunks := map[int][]byte{}
	for index := 0; index < 1024; index++ {
		location := binary.BigEndian.Uint32(data[index*4 : index*4+4])
		offset := int(location >> 8)
		sectors := int(location & 0xff)
		if offset == 0 || sectors == 0 {
			continue
		}
		start := offset * 4096
		end := start + sectors*4096
		if start < 0 || start >= len(data) || end > len(data) {
			continue
		}
		raw := data[start:end]
		if len(raw) >= 4 {
			length := int(binary.BigEndian.Uint32(raw[:4]))
			if length > 0 && length+4 <= len(raw) {
				raw = raw[:alignToSector(length+4)]
			}
		}
		chunks[index] = append([]byte(nil), raw...)
	}
	return chunks, timestamps, nil
}

func writeRegionRawChunks(path string, chunks map[int][]byte, timestamps []byte) error {
	next := make([]byte, 8192)
	if len(timestamps) >= 4096 {
		copy(next[4096:8192], timestamps[:4096])
	}
	indexes := make([]int, 0, len(chunks))
	for index := range chunks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		raw := chunks[index]
		if len(raw) == 0 {
			continue
		}
		needed := alignToSector(len(raw))
		payload := make([]byte, needed)
		copy(payload, raw)
		sectors := len(payload) / 4096
		if sectors > 255 {
			return fmt.Errorf("chunk in %s is too large to write safely", filepath.Base(path))
		}
		offset := len(next) / 4096
		binary.BigEndian.PutUint32(next[index*4:index*4+4], uint32(offset<<8|sectors))
		next = append(next, payload...)
	}
	if len(next) == 8192 {
		return fmt.Errorf("region file %s would be empty after cleanup", filepath.Base(path))
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".rewrite-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(next); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	backupPath := path + ".before-mod-cleanup-" + time.Now().UTC().Format("20060102150405")
	if err := os.Rename(path, backupPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func decodeRegionRawChunk(raw []byte) ([]byte, error) {
	if len(raw) < 5 {
		return nil, errors.New("raw chunk is too small")
	}
	length := int(binary.BigEndian.Uint32(raw[:4]))
	if length <= 1 || length+4 > len(raw) {
		return nil, errors.New("invalid raw chunk length")
	}
	compression := raw[4]
	compressed := raw[5 : 4+length]
	var reader io.ReadCloser
	var err error
	switch compression {
	case 1:
		reader, err = gzip.NewReader(bytes.NewReader(compressed))
	case 2:
		reader, err = zlib.NewReader(bytes.NewReader(compressed))
	case 3:
		return append([]byte(nil), compressed...), nil
	default:
		return nil, errors.New("unsupported chunk compression")
	}
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func encodeRegionRawChunk(payload []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	length := compressed.Len() + 1
	total := alignToSector(length + 4)
	raw := make([]byte, total)
	binary.BigEndian.PutUint32(raw[:4], uint32(length))
	raw[4] = 2
	copy(raw[5:], compressed.Bytes())
	return raw, nil
}

func scanNBTModRefs(root *nbtTreeTag, namespaces map[string]bool, cleanup bool) modWorldChunkReport {
	report := modWorldChunkReport{}
	data := nbtChunkData(root)
	if data == nil {
		return report
	}
	report.Blocks += scanSectionModBlocks(data, namespaces, cleanup, &report)
	report.BlockEntities += scanAndMaybeRemoveNamedList(data, namespaces, cleanup, &report, "block_entities", "TileEntities")
	report.Entities += scanAndMaybeRemoveNamedList(data, namespaces, cleanup, &report, "entities", "Entities")
	return report
}

func nbtChunkData(root *nbtTreeTag) *nbtTreeTag {
	if root == nil || root.Type != 10 {
		return nil
	}
	if level := nbtCompoundChild(root, "Level"); level != nil && level.Type == 10 {
		return level
	}
	if data := nbtCompoundChild(root, "Data"); data != nil && data.Type == 10 {
		return data
	}
	return root
}

func scanSectionModBlocks(data *nbtTreeTag, namespaces map[string]bool, cleanup bool, report *modWorldChunkReport) int {
	sections := nbtCompoundChild(data, "sections", "Sections")
	if sections == nil || sections.Type != 9 || sections.ListType != 10 {
		return 0
	}
	items, _ := sections.Value.([]nbtTreeTag)
	total := 0
	for sectionIndex := range items {
		section := &items[sectionIndex]
		blockStates := nbtCompoundChild(section, "block_states")
		if blockStates == nil {
			blockStates = section
		}
		palette := nbtCompoundChild(blockStates, "palette", "Palette")
		if palette == nil || palette.Type != 9 || palette.ListType != 10 {
			continue
		}
		paletteItems, _ := palette.Value.([]nbtTreeTag)
		matchingIndexes := map[int]bool{}
		for paletteIndex := range paletteItems {
			nameTag := nbtCompoundChild(&paletteItems[paletteIndex], "Name")
			name := nbtString(nameTag)
			if namespaceMatches(name, namespaces) {
				matchingIndexes[paletteIndex] = true
				if cleanup {
					nameTag.Value = "minecraft:air"
					report.Changed = true
				}
			}
		}
		if len(matchingIndexes) == 0 {
			continue
		}
		palette.Value = paletteItems
		dataTag := nbtCompoundChild(blockStates, "data", "BlockStates")
		blockData, _ := nbtLongArray(dataTag)
		total += countMatchingPaletteBlocks(blockData, len(paletteItems), matchingIndexes)
	}
	sections.Value = items
	return total
}

func scanAndMaybeRemoveNamedList(data *nbtTreeTag, namespaces map[string]bool, cleanup bool, report *modWorldChunkReport, names ...string) int {
	list := nbtCompoundChild(data, names...)
	if list == nil || list.Type != 9 || list.ListType != 10 {
		return 0
	}
	items, _ := list.Value.([]nbtTreeTag)
	matches := 0
	next := make([]nbtTreeTag, 0, len(items))
	for index := range items {
		id := nbtString(nbtCompoundChild(&items[index], "id", "Id"))
		if namespaceMatches(id, namespaces) {
			matches++
			continue
		}
		next = append(next, items[index])
	}
	if cleanup && matches > 0 {
		list.Value = next
		report.Changed = true
	}
	return matches
}

func countMatchingPaletteBlocks(data []int64, paletteSize int, matching map[int]bool) int {
	if paletteSize <= 0 || len(matching) == 0 {
		return 0
	}
	if len(data) == 0 {
		if matching[0] {
			return 4096
		}
		return 0
	}
	bits := 1
	for (1 << bits) < paletteSize {
		bits++
	}
	if bits < 4 {
		bits = 4
	}
	mask := uint64((1 << bits) - 1)
	count := 0
	for blockIndex := 0; blockIndex < 4096; blockIndex++ {
		bitIndex := blockIndex * bits
		longIndex := bitIndex / 64
		startBit := bitIndex % 64
		if longIndex < 0 || longIndex >= len(data) {
			continue
		}
		value := (uint64(data[longIndex]) >> startBit) & mask
		bitsInFirst := 64 - startBit
		if bitsInFirst < bits && longIndex+1 < len(data) {
			value |= (uint64(data[longIndex+1]) << bitsInFirst) & mask
		}
		if matching[int(value)] {
			count++
		}
	}
	return count
}

func namespaceMatches(id string, namespaces map[string]bool) bool {
	namespace, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(id)), ":")
	return ok && namespaces[namespace]
}

func nbtCompoundChild(tag *nbtTreeTag, names ...string) *nbtTreeTag {
	if tag == nil || tag.Type != 10 {
		return nil
	}
	children, _ := tag.Value.([]nbtTreeTag)
	for i := range children {
		for _, name := range names {
			if children[i].Name == name {
				return &children[i]
			}
		}
	}
	return nil
}

func nbtString(tag *nbtTreeTag) string {
	if tag == nil || tag.Type != 8 {
		return ""
	}
	value, _ := tag.Value.(string)
	return value
}

func nbtLongArray(tag *nbtTreeTag) ([]int64, bool) {
	if tag == nil || tag.Type != 12 {
		return nil, false
	}
	value, ok := tag.Value.([]int64)
	return value, ok
}

type nbtReader struct {
	reader *bytes.Reader
}

func readNBTCompound(data []byte) (nbtCompound, error) {
	n := &nbtReader{reader: bytes.NewReader(data)}
	tag, err := n.readByte()
	if err != nil {
		return nil, err
	}
	if tag != 10 {
		return nil, errors.New("root NBT tag is not a compound")
	}
	if _, err := n.readString(); err != nil {
		return nil, err
	}
	value, err := n.readPayload(tag)
	if err != nil {
		return nil, err
	}
	compound, ok := value.(nbtCompound)
	if !ok {
		return nil, errors.New("root NBT payload is not a compound")
	}
	return compound, nil
}

func readNBTTree(data []byte) (*nbtTreeTag, error) {
	n := &nbtReader{reader: bytes.NewReader(data)}
	tagType, err := n.readByte()
	if err != nil {
		return nil, err
	}
	if tagType != 10 {
		return nil, errors.New("root NBT tag is not a compound")
	}
	name, err := n.readString()
	if err != nil {
		return nil, err
	}
	tag, err := n.readTreePayload(tagType)
	if err != nil {
		return nil, err
	}
	tag.Name = name
	return &tag, nil
}

func (n *nbtReader) readTreePayload(tag byte) (nbtTreeTag, error) {
	out := nbtTreeTag{Type: tag}
	switch tag {
	case 1:
		value, err := n.readByte()
		out.Value = value
		return out, err
	case 2:
		value, err := n.readUint16()
		out.Value = int16(value)
		return out, err
	case 3:
		value, err := n.readUint32()
		out.Value = int32(value)
		return out, err
	case 4:
		value, err := n.readUint64()
		out.Value = int64(value)
		return out, err
	case 5:
		value, err := n.readUint32()
		out.Value = value
		return out, err
	case 6:
		value, err := n.readUint64()
		out.Value = value
		return out, err
	case 7:
		length, err := n.readInt32()
		if err != nil {
			return out, err
		}
		if length < 0 {
			return out, errors.New("negative byte array length")
		}
		value := make([]byte, int(length))
		_, err = io.ReadFull(n.reader, value)
		out.Value = value
		return out, err
	case 8:
		value, err := n.readString()
		out.Value = value
		return out, err
	case 9:
		childTag, err := n.readByte()
		if err != nil {
			return out, err
		}
		length, err := n.readInt32()
		if err != nil {
			return out, err
		}
		if length < 0 {
			return out, errors.New("negative list length")
		}
		items := make([]nbtTreeTag, int(length))
		for i := range items {
			item, err := n.readTreePayload(childTag)
			if err != nil {
				return out, err
			}
			items[i] = item
		}
		out.ListType = childTag
		out.Value = items
		return out, nil
	case 10:
		children := []nbtTreeTag{}
		for {
			childTag, err := n.readByte()
			if err != nil {
				return out, err
			}
			if childTag == 0 {
				break
			}
			name, err := n.readString()
			if err != nil {
				return out, err
			}
			child, err := n.readTreePayload(childTag)
			if err != nil {
				return out, err
			}
			child.Name = name
			children = append(children, child)
		}
		out.Value = children
		return out, nil
	case 11:
		length, err := n.readInt32()
		if err != nil {
			return out, err
		}
		if length < 0 {
			return out, errors.New("negative int array length")
		}
		value := make([]int32, int(length))
		for i := range value {
			next, err := n.readUint32()
			if err != nil {
				return out, err
			}
			value[i] = int32(next)
		}
		out.Value = value
		return out, nil
	case 12:
		length, err := n.readInt32()
		if err != nil {
			return out, err
		}
		if length < 0 {
			return out, errors.New("negative long array length")
		}
		value := make([]int64, int(length))
		for i := range value {
			next, err := n.readUint64()
			if err != nil {
				return out, err
			}
			value[i] = int64(next)
		}
		out.Value = value
		return out, nil
	default:
		return out, fmt.Errorf("unsupported NBT tag %d", tag)
	}
}

func writeNBTTree(root *nbtTreeTag) ([]byte, error) {
	if root == nil || root.Type != 10 {
		return nil, errors.New("root NBT tag is not a compound")
	}
	var buf bytes.Buffer
	buf.WriteByte(root.Type)
	writeNBTString(&buf, root.Name)
	if err := writeNBTPayload(&buf, root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeNBTPayload(buf *bytes.Buffer, tag *nbtTreeTag) error {
	switch tag.Type {
	case 1:
		buf.WriteByte(byteFromAny(tag.Value))
	case 2:
		writeUint16(buf, uint16(int16FromAny(tag.Value)))
	case 3:
		writeUint32(buf, uint32(int32FromAny(tag.Value)))
	case 4:
		writeUint64(buf, uint64(int64FromAny(tag.Value)))
	case 5:
		writeUint32(buf, uint32FromAny(tag.Value))
	case 6:
		writeUint64(buf, uint64FromAny(tag.Value))
	case 7:
		value, _ := tag.Value.([]byte)
		writeUint32(buf, uint32(len(value)))
		buf.Write(value)
	case 8:
		writeNBTString(buf, stringFromAny(tag.Value))
	case 9:
		items, _ := tag.Value.([]nbtTreeTag)
		buf.WriteByte(tag.ListType)
		writeUint32(buf, uint32(len(items)))
		for i := range items {
			item := items[i]
			item.Type = tag.ListType
			if err := writeNBTPayload(buf, &item); err != nil {
				return err
			}
		}
	case 10:
		children, _ := tag.Value.([]nbtTreeTag)
		for i := range children {
			buf.WriteByte(children[i].Type)
			writeNBTString(buf, children[i].Name)
			if err := writeNBTPayload(buf, &children[i]); err != nil {
				return err
			}
		}
		buf.WriteByte(0)
	case 11:
		value, _ := tag.Value.([]int32)
		writeUint32(buf, uint32(len(value)))
		for _, next := range value {
			writeUint32(buf, uint32(next))
		}
	case 12:
		value, _ := tag.Value.([]int64)
		writeUint32(buf, uint32(len(value)))
		for _, next := range value {
			writeUint64(buf, uint64(next))
		}
	default:
		return fmt.Errorf("unsupported NBT tag %d", tag.Type)
	}
	return nil
}

func writeNBTString(buf *bytes.Buffer, value string) {
	if len(value) > 65535 {
		value = value[:65535]
	}
	writeUint16(buf, uint16(len(value)))
	buf.WriteString(value)
}

func writeUint16(buf *bytes.Buffer, value uint16) {
	var tmp [2]byte
	binary.BigEndian.PutUint16(tmp[:], value)
	buf.Write(tmp[:])
}

func writeUint32(buf *bytes.Buffer, value uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], value)
	buf.Write(tmp[:])
}

func writeUint64(buf *bytes.Buffer, value uint64) {
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], value)
	buf.Write(tmp[:])
}

func byteFromAny(value any) byte {
	switch v := value.(type) {
	case byte:
		return v
	case int8:
		return byte(v)
	case int:
		return byte(v)
	default:
		return 0
	}
}

func int16FromAny(value any) int16 {
	switch v := value.(type) {
	case int16:
		return v
	case uint16:
		return int16(v)
	case int:
		return int16(v)
	default:
		return 0
	}
}

func int32FromAny(value any) int32 {
	switch v := value.(type) {
	case int32:
		return v
	case uint32:
		return int32(v)
	case int:
		return int32(v)
	default:
		return 0
	}
}

func int64FromAny(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case uint64:
		return int64(v)
	case int:
		return int64(v)
	default:
		return 0
	}
}

func uint32FromAny(value any) uint32 {
	switch v := value.(type) {
	case uint32:
		return v
	case int32:
		return uint32(v)
	case int:
		return uint32(v)
	default:
		return 0
	}
}

func uint64FromAny(value any) uint64 {
	switch v := value.(type) {
	case uint64:
		return v
	case int64:
		return uint64(v)
	case int:
		return uint64(v)
	default:
		return 0
	}
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func (n *nbtReader) readPayload(tag byte) (any, error) {
	switch tag {
	case 1:
		return n.readByte()
	case 2:
		value, err := n.readUint16()
		return int16(value), err
	case 3:
		value, err := n.readUint32()
		return int32(value), err
	case 4:
		value, err := n.readUint64()
		return int64(value), err
	case 5:
		value, err := n.readUint32()
		return value, err
	case 6:
		value, err := n.readUint64()
		return value, err
	case 7:
		length, err := n.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, errors.New("negative byte array length")
		}
		out := make([]byte, int(length))
		_, err = io.ReadFull(n.reader, out)
		return out, err
	case 8:
		return n.readString()
	case 9:
		childTag, err := n.readByte()
		if err != nil {
			return nil, err
		}
		length, err := n.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, errors.New("negative list length")
		}
		out := make([]any, int(length))
		for i := range out {
			value, err := n.readPayload(childTag)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case 10:
		out := nbtCompound{}
		for {
			childTag, err := n.readByte()
			if err != nil {
				return nil, err
			}
			if childTag == 0 {
				break
			}
			name, err := n.readString()
			if err != nil {
				return nil, err
			}
			value, err := n.readPayload(childTag)
			if err != nil {
				return nil, err
			}
			out[name] = value
		}
		return out, nil
	case 11:
		length, err := n.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, errors.New("negative int array length")
		}
		out := make([]int32, int(length))
		for i := range out {
			value, err := n.readInt32()
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case 12:
		length, err := n.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, errors.New("negative long array length")
		}
		out := make([]int64, int(length))
		for i := range out {
			value, err := n.readInt64()
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	default:
		return nil, errors.New("unsupported NBT tag")
	}
}

func (n *nbtReader) readByte() (byte, error) {
	value, err := n.reader.ReadByte()
	return value, err
}

func (n *nbtReader) readUint16() (uint16, error) {
	var bytes [2]byte
	_, err := io.ReadFull(n.reader, bytes[:])
	return binary.BigEndian.Uint16(bytes[:]), err
}

func (n *nbtReader) readUint32() (uint32, error) {
	var bytes [4]byte
	_, err := io.ReadFull(n.reader, bytes[:])
	return binary.BigEndian.Uint32(bytes[:]), err
}

func (n *nbtReader) readUint64() (uint64, error) {
	var bytes [8]byte
	_, err := io.ReadFull(n.reader, bytes[:])
	return binary.BigEndian.Uint64(bytes[:]), err
}

func (n *nbtReader) readInt32() (int32, error) {
	value, err := n.readUint32()
	return int32(value), err
}

func (n *nbtReader) readInt64() (int64, error) {
	value, err := n.readUint64()
	return int64(value), err
}

func (n *nbtReader) readString() (string, error) {
	length, err := n.readUint16()
	if err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	bytes := make([]byte, int(length))
	_, err = io.ReadFull(n.reader, bytes)
	return string(bytes), err
}

type worldChunkSection struct {
	Y             int
	Palette       []string
	Data          []int64
	Bits          int
	Mask          uint64
	ValuesPerLong int
	Compact       bool
}

func chunkSurfacePixels(root nbtCompound, dimension string) ([]string, error) {
	data := root
	if level, ok := root["Level"].(nbtCompound); ok {
		data = level
	}
	rawSections, ok := data["sections"].([]any)
	if !ok {
		rawSections, ok = data["Sections"].([]any)
	}
	if !ok || len(rawSections) == 0 {
		return nil, errors.New("chunk has no sections")
	}
	sections := []worldChunkSection{}
	for _, raw := range rawSections {
		compound, ok := raw.(nbtCompound)
		if !ok {
			continue
		}
		section, ok := parseWorldChunkSection(compound)
		if ok {
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		return nil, errors.New("chunk has no block states")
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Y > sections[j].Y })
	pixels := make([]string, 16*16)
	for z := 0; z < 16; z++ {
		for x := 0; x < 16; x++ {
			color := fallbackDimensionColor(dimension)
			found := false
			for _, section := range sections {
				for y := 15; y >= 0; y-- {
					block := section.blockAt(x, y, z)
					if skipSurfaceBlock(block) {
						continue
					}
					color = colorForBlock(block, dimension)
					found = true
					break
				}
				if found {
					break
				}
			}
			pixels[z*16+x] = color
		}
	}
	return pixels, nil
}

func parseWorldChunkSection(section nbtCompound) (worldChunkSection, bool) {
	y := intFromAny(section["Y"], 0)
	blockStates, _ := section["block_states"].(nbtCompound)
	if blockStates == nil {
		blockStates = section
	}
	rawPalette, ok := blockStates["palette"].([]any)
	if !ok {
		rawPalette, ok = blockStates["Palette"].([]any)
	}
	if !ok || len(rawPalette) == 0 {
		return worldChunkSection{}, false
	}
	palette := make([]string, 0, len(rawPalette))
	for _, raw := range rawPalette {
		compound, ok := raw.(nbtCompound)
		if !ok {
			continue
		}
		name, _ := compound["Name"].(string)
		if name == "" {
			name = "minecraft:air"
		}
		palette = append(palette, name)
	}
	if len(palette) == 0 {
		return worldChunkSection{}, false
	}
	data, _ := blockStates["data"].([]int64)
	if data == nil {
		data, _ = blockStates["BlockStates"].([]int64)
	}
	bitCount := 1
	for (1 << bitCount) < len(palette) {
		bitCount++
	}
	if bitCount < 4 {
		bitCount = 4
	}
	valuesPerLong := 64 / bitCount
	if valuesPerLong <= 0 {
		valuesPerLong = 1
	}
	compactLength := (4096*bitCount + 63) / 64
	paddedLength := (4096 + valuesPerLong - 1) / valuesPerLong
	compact := len(data) > 0 && len(data) <= compactLength && compactLength < paddedLength
	return worldChunkSection{Y: y, Palette: palette, Data: data, Bits: bitCount, Mask: (uint64(1) << bitCount) - 1, ValuesPerLong: valuesPerLong, Compact: compact}, true
}

func (s worldChunkSection) blockAt(x, y, z int) string {
	if len(s.Palette) == 0 {
		return "minecraft:air"
	}
	if len(s.Data) == 0 {
		return s.Palette[0]
	}
	blockIndex := y*256 + z*16 + x
	if !s.Compact {
		valuesPerLong := s.ValuesPerLong
		if valuesPerLong <= 0 {
			valuesPerLong = 64 / s.Bits
		}
		if valuesPerLong <= 0 {
			valuesPerLong = 1
		}
		longIndex := blockIndex / valuesPerLong
		startBit := (blockIndex - longIndex*valuesPerLong) * s.Bits
		if longIndex < 0 || longIndex >= len(s.Data) {
			return s.Palette[0]
		}
		index := int((uint64(s.Data[longIndex]) >> startBit) & s.Mask)
		if index < 0 || index >= len(s.Palette) {
			return s.Palette[0]
		}
		return s.Palette[index]
	}
	bitIndex := blockIndex * s.Bits
	longIndex := bitIndex / 64
	startBit := bitIndex % 64
	if longIndex < 0 || longIndex >= len(s.Data) {
		return s.Palette[0]
	}
	value := (uint64(s.Data[longIndex]) >> startBit) & s.Mask
	bitsInFirst := 64 - startBit
	if bitsInFirst < s.Bits && longIndex+1 < len(s.Data) {
		value |= (uint64(s.Data[longIndex+1]) << bitsInFirst) & s.Mask
	}
	index := int(value)
	if index < 0 || index >= len(s.Palette) {
		return s.Palette[0]
	}
	return s.Palette[index]
}

func fallbackChunkPixels(dimension string) []string {
	color := fallbackDimensionColor(dimension)
	pixels := make([]string, 16*16)
	for i := range pixels {
		pixels[i] = color
	}
	return pixels
}

func fallbackDimensionColor(dimension string) string {
	switch dimension {
	case "nether":
		return "#5c1e24"
	case "end":
		return "#d9d2a3"
	default:
		return "#4f8a3d"
	}
}

func overviewChunkColor(ref worldRegionChunkRef, dimension string) string {
	hash := uint32(ref.ChunkX)*374761393 + uint32(ref.ChunkZ)*668265263 + 0x9e3779b9
	hash ^= hash >> 13
	hash *= 1274126177
	hash ^= hash >> 16
	shade := int(hash % 42)
	var r, g, b int
	switch dimension {
	case "nether":
		r = 86 + shade
		g = 24 + shade/4
		b = 31 + shade/3
	case "end":
		r = 174 + shade
		g = 166 + shade
		b = 117 + shade/2
	default:
		if hash%11 == 0 {
			r = 54 + shade/3
			g = 90 + shade/2
			b = 142 + shade
		} else if hash%7 == 0 {
			r = 126 + shade
			g = 108 + shade/2
			b = 70 + shade/3
		} else {
			r = 56 + shade/3
			g = 106 + shade
			b = 62 + shade/2
		}
	}
	return fmt.Sprintf("#%02x%02x%02x", clampInt(r, 0, 255), clampInt(g, 0, 255), clampInt(b, 0, 255))
}

func skipSurfaceBlock(block string) bool {
	block = strings.ToLower(block)
	if block == "" || strings.Contains(block, "air") {
		return true
	}
	for _, token := range []string{"torch", "flower", "grass", "fern", "sapling", "vine", "button", "pressure_plate", "carpet", "rail", "sign", "banner"} {
		if strings.Contains(block, token) && block != "minecraft:grass_block" {
			return true
		}
	}
	return false
}

func colorForBlock(block, dimension string) string {
	block = strings.ToLower(block)
	switch {
	case strings.Contains(block, "water"), strings.Contains(block, "ice"):
		return "#376dba"
	case strings.Contains(block, "lava"):
		return "#e26522"
	case strings.Contains(block, "snow"):
		return "#edf2f4"
	case strings.Contains(block, "sand"), strings.Contains(block, "sandstone"):
		return "#d7c47a"
	case strings.Contains(block, "gravel"):
		return "#8a867f"
	case strings.Contains(block, "clay"), strings.Contains(block, "terracotta"):
		return "#9b7465"
	case strings.Contains(block, "stone"), strings.Contains(block, "deepslate"), strings.Contains(block, "ore"):
		return "#777a7f"
	case strings.Contains(block, "dirt"), strings.Contains(block, "mud"), strings.Contains(block, "farmland"):
		return "#7b5934"
	case strings.Contains(block, "podzol"):
		return "#5f4024"
	case strings.Contains(block, "log"), strings.Contains(block, "wood"), strings.Contains(block, "planks"):
		return "#8a5b32"
	case strings.Contains(block, "leaves"), strings.Contains(block, "moss"), strings.Contains(block, "azalea"):
		return "#2f7d37"
	case strings.Contains(block, "grass_block"):
		return "#4f9a45"
	case strings.Contains(block, "netherrack"), strings.Contains(block, "nether_wart"):
		return "#6d2529"
	case strings.Contains(block, "crimson"):
		return "#7d1f45"
	case strings.Contains(block, "warped"):
		return "#1f756f"
	case strings.Contains(block, "basalt"), strings.Contains(block, "blackstone"):
		return "#343238"
	case strings.Contains(block, "soul_sand"), strings.Contains(block, "soul_soil"):
		return "#5e4b3f"
	case strings.Contains(block, "end_stone"):
		return "#d9d2a3"
	case strings.Contains(block, "purpur"):
		return "#b88bc0"
	default:
		return fallbackDimensionColor(dimension)
	}
}

func intFromAny(value any, fallback int) int {
	switch v := value.(type) {
	case byte:
		return int(int8(v))
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	default:
		return fallback
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil {
		return fallback
	}
	return value
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

func (a *app) handleModsProviders(w http.ResponseWriter, r *http.Request) {
	key := a.curseForgeAPIKey()
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": []map[string]any{
			{
				"id":          "modrinth",
				"name":        "Modrinth",
				"enabled":     true,
				"configured":  true,
				"description": "No API key required. Best supported provider in MineMux.",
			},
			{
				"id":          "curseforge",
				"name":        "CurseForge",
				"enabled":     key != "",
				"configured":  key != "",
				"description": "Requires a CurseForge Core API key for search and installs.",
			},
		},
	})
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
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		provider = "all"
	}
	res, err := a.searchMods(provider, query, p)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *app) handleModVersions(w http.ResponseWriter, r *http.Request) {
	id := serverIDFromRequest(a, r)
	p, err := a.loadProfileFor(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "profile not found; run setup first"})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		provider = "modrinth"
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "projectId is required"})
		return
	}
	var versions []modVersionOption
	switch provider {
	case "curseforge":
		versions, err = a.curseForgeVersions(projectID, p)
	default:
		versions, err = modrinthVersions(projectID, p)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
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
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "modrinth"
	}
	var installed []lockedInstall
	if provider == "curseforge" {
		installed, err = a.installCurseForgeProjectFor(id, p, req.ProjectID, req.VersionID)
	} else {
		installed, err = a.installModrinthProjectFor(id, p, req.ProjectID, req.VersionID, map[string]bool{})
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": installed})
}

func (a *app) handleCurseForgeKeySave(w http.ResponseWriter, r *http.Request) {
	var req curseForgeKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "apiKey is required"})
		return
	}
	if err := os.MkdirAll(a.paths().Runtime, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	if err := os.WriteFile(a.curseForgeKeyPath(), []byte(key+"\n"), 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": true})
}

func (a *app) handleCurseForgeKeyDelete(w http.ResponseWriter, r *http.Request) {
	_ = os.Remove(a.curseForgeKeyPath())
	writeJSON(w, http.StatusOK, map[string]bool{"configured": false})
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
	return a.installServerRuntimeFor(a.activeServerIDValue(), p, javaMajor)
}

func (a *app) installServerRuntimeFor(serverID string, p *profile, javaMajor int) (string, error) {
	serverID = normalizeServerID(serverID)
	serverDir := a.serverDir(serverID)
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		return "", err
	}
	serverJarPath := filepath.Join(serverDir, "server.jar")
	loader := strings.ToLower(strings.TrimSpace(p.Loader))
	switch loader {
	case "vanilla":
		a.appendSetupLog("Resolving Vanilla server download")
		version, jarURL, sha1sum, err := resolveVanillaDownload(p.MinecraftVersion, javaMajor)
		if err != nil {
			return "", err
		}
		a.appendSetupLog("Downloading Vanilla server.jar")
		return version, downloadFileWithSHA1(jarURL, serverJarPath, sha1sum)
	case "quilt":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestCompatibleMinecraftVersion(p.Loader, javaMajor)
		}
		a.appendSetupLog("Downloading Quilt installer")
		installer, err := latestMavenInstaller("https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer/maven-metadata.xml", "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer")
		if err != nil {
			return "", err
		}
		installerPath := filepath.Join(serverDir, "quilt-installer.jar")
		if err := downloadFile(installer, installerPath); err != nil {
			return "", err
		}
		a.appendSetupLog("Running Quilt server installer")
		if err := a.runInstallerInDir(serverDir, "java", "-jar", installerPath, "install", "server", version, "--download-server", "--install-dir="+serverDir); err != nil {
			return "", err
		}
		return version, nil
	case "fabric":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestCompatibleMinecraftVersion(p.Loader, javaMajor)
		}
		a.appendSetupLog("Downloading Fabric installer")
		installer, err := latestMavenInstaller("https://maven.fabricmc.net/net/fabricmc/fabric-installer/maven-metadata.xml", "https://maven.fabricmc.net/net/fabricmc/fabric-installer")
		if err != nil {
			return "", err
		}
		installerPath := filepath.Join(serverDir, "fabric-installer.jar")
		if err := downloadFile(installer, installerPath); err != nil {
			return "", err
		}
		a.appendSetupLog("Running Fabric server installer")
		if err := a.runInstallerInDir(serverDir, "java", "-jar", installerPath, "server", "-mcversion", version, "-downloadMinecraft", "-dir", serverDir); err != nil {
			return "", err
		}
		return version, nil
	case "forge":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestCompatibleMinecraftVersion(p.Loader, javaMajor)
		}
		a.appendSetupLog("Resolving Forge installer")
		installer, resolved, err := resolveForgeInstaller(version)
		if err != nil {
			return "", err
		}
		return resolved, a.installForgeFamilyFor(serverID, "Forge", installer)
	case "neoforge":
		version := p.MinecraftVersion
		if version == "" || version == "latest-compatible" {
			version = latestCompatibleMinecraftVersion(p.Loader, javaMajor)
		}
		a.appendSetupLog("Resolving NeoForge installer")
		installer, resolved, err := resolveNeoForgeInstaller(version)
		if err != nil {
			return "", err
		}
		return resolved, a.installForgeFamilyFor(serverID, "NeoForge", installer)
	default:
		a.appendSetupLog("Resolving Paper version and download")
		version, jarURL, sha, err := resolvePaperDownload(p.MinecraftVersion, javaMajor)
		if err != nil {
			return "", err
		}
		a.appendSetupLog("Downloading Paper server.jar")
		return version, downloadFileWithSHA256(jarURL, serverJarPath, sha)
	}
}

func (a *app) installForgeFamily(name, installerURL string) error {
	return a.installForgeFamilyFor(a.activeServerIDValue(), name, installerURL)
}

func (a *app) installForgeFamilyFor(serverID, name, installerURL string) error {
	serverDir := a.serverDir(serverID)
	installerPath := filepath.Join(serverDir, strings.ToLower(name)+"-installer.jar")
	a.appendSetupLog("Downloading " + name + " installer")
	if err := downloadFile(installerURL, installerPath); err != nil {
		return err
	}
	a.appendSetupLog("Running " + name + " server installer")
	if err := a.runInstallerInDir(serverDir, "java", "-jar", installerPath, "--installServer"); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(serverDir, "user_jvm_args.txt"), []byte("# MineMux writes memory limits before start\n"), 0o644)
	return nil
}

func (a *app) appendSetupLog(line string) {
	a.server.mu.Lock()
	a.server.appendLogLocked(line)
	a.server.mu.Unlock()
}

func (a *app) runInstaller(command string, args ...string) error {
	return a.runInstallerInDir(a.paths().ServerMain, command, args...)
}

func (a *app) runInstallerInDir(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = cleanJavaEnv(os.Environ())
	return runCommandStreaming(cmd, a.appendSetupLog)
}

func minecraftVersionOptions(javaMajor int) ([]minecraftVersionOption, string, string) {
	return minecraftVersionOptionsForLoader("vanilla", javaMajor)
}

func minecraftVersionOptionsForLoader(loader string, javaMajor int) ([]minecraftVersionOption, string, string) {
	loader = strings.ToLower(strings.TrimSpace(loader))
	if loader == "" {
		loader = "paper"
	}
	var (
		versions []minecraftVersionOption
		source   string
		err      error
	)
	switch loader {
	case "vanilla":
		versions, err = fetchMojangReleaseVersions(javaMajor)
		source = "mojang"
	case "fabric":
		versions, err = fetchFabricMinecraftVersions(javaMajor)
		source = "fabric"
	case "quilt":
		versions, err = fetchQuiltMinecraftVersions(javaMajor)
		source = "quilt"
	case "forge":
		versions, err = fetchForgeMinecraftVersions(javaMajor)
		source = "forge"
	case "neoforge":
		versions, err = fetchNeoForgeMinecraftVersions(javaMajor)
		source = "neoforge"
	default:
		versions, err = fetchPaperMinecraftVersions(javaMajor)
		source = "papermc"
	}
	if err == nil && len(versions) > 0 {
		return versions, source, ""
	}
	warning := ""
	if err != nil {
		warning = err.Error()
	} else {
		warning = "version catalog was empty"
	}
	return fallbackMinecraftVersions(javaMajor), "fallback", warning
}

func fetchMojangReleaseVersions(javaMajor int) ([]minecraftVersionOption, error) {
	var manifest mojangVersionManifest
	if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(manifest.Versions))
	for _, entry := range manifest.Versions {
		if entry.ID == "" || entry.Type != "release" {
			continue
		}
		ids = append(ids, entry.ID)
	}
	return minecraftOptionsFromIDs(ids, "release", javaMajor), nil
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
		if !isStandardMinecraftRelease(id) {
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

func fetchFabricMinecraftVersions(javaMajor int) ([]minecraftVersionOption, error) {
	var response []fabricGameVersion
	if err := getJSON("https://meta.fabricmc.net/v2/versions/game", &response); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(response))
	for _, entry := range response {
		if !entry.Stable {
			continue
		}
		ids = append(ids, entry.Version)
	}
	return minecraftOptionsFromIDs(ids, "stable", javaMajor), nil
}

func fetchQuiltMinecraftVersions(javaMajor int) ([]minecraftVersionOption, error) {
	var response []quiltGameVersion
	if err := getJSON("https://meta.quiltmc.org/v3/versions/game", &response); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(response))
	for _, entry := range response {
		if !entry.Stable {
			continue
		}
		ids = append(ids, entry.Version)
	}
	return minecraftOptionsFromIDs(ids, "stable", javaMajor), nil
}

func fetchForgeMinecraftVersions(javaMajor int) ([]minecraftVersionOption, error) {
	metadata, err := getText("https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	matches := regexp.MustCompile(`<version>([^<]+)</version>`).FindAllStringSubmatch(metadata, -1)
	ids := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for i := len(matches) - 1; i >= 0; i-- {
		artifactVersion := strings.TrimSpace(matches[i][1])
		minecraftVersion := strings.SplitN(artifactVersion, "-", 2)[0]
		if minecraftVersion == "" || seen[minecraftVersion] {
			continue
		}
		seen[minecraftVersion] = true
		ids = append(ids, minecraftVersion)
	}
	return minecraftOptionsFromIDs(ids, "forge", javaMajor), nil
}

func fetchNeoForgeMinecraftVersions(javaMajor int) ([]minecraftVersionOption, error) {
	metadata, err := getText("https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	releases, err := fetchMojangReleaseVersions(javaMajor)
	if err != nil {
		return nil, err
	}
	options := make([]minecraftVersionOption, 0, len(releases))
	for _, option := range releases {
		prefix := neoForgeVersionPrefix(option.ID)
		if prefix == "" || !mavenMetadataHasVersionPrefix(metadata, prefix) {
			continue
		}
		option.Support = "neoforge"
		option.Recommended = len(options) == 0
		options = append(options, option)
	}
	return options, nil
}

func minecraftOptionsFromIDs(ids []string, support string, javaMajor int) []minecraftVersionOption {
	options := make([]minecraftVersionOption, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !isStandardMinecraftRelease(id) {
			continue
		}
		minimum := javaMinimumForMinecraft(id)
		if javaMajor > 0 && minimum > javaMajor {
			continue
		}
		options = append(options, minecraftVersionOption{
			ID:          id,
			JavaMinimum: minimum,
			Support:     support,
			Recommended: len(options) == 0,
		})
		seen[id] = true
	}
	return options
}

func fallbackMinecraftVersions(javaMajor int) []minecraftVersionOption {
	all := []minecraftVersionOption{
		{ID: "26.2", JavaMinimum: 21, Support: "fallback"},
		{ID: "26.1.2", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.21.8", JavaMinimum: 21, Support: "fallback"},
		{ID: "1.21.7", JavaMinimum: 21, Support: "fallback"},
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

func isStandardMinecraftRelease(version string) bool {
	// Mojang's manifest can include special/event entries marked as "release".
	// MineMux server setup targets Java Edition release IDs, including calendar-style 26.x IDs.
	return minecraftReleaseIDPattern.MatchString(strings.TrimSpace(version))
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

func latestCompatibleMinecraftVersion(loader string, javaMajor int) string {
	target, _, _ := recommendedMinecraftUpdateTarget(loader, javaMajor)
	if target != "" {
		return target
	}
	return latestFallbackMinecraftVersion(javaMajor)
}

func resolveVanillaDownload(requestedVersion string, javaMajor int) (string, string, string, error) {
	var manifest mojangVersionManifest
	if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		return "", "", "", err
	}
	version := requestedVersion
	if version == "" || version == "latest-compatible" {
		version = latestCompatibleMinecraftVersion("vanilla", javaMajor)
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

func mavenMetadataHasVersionPrefix(metadata, prefix string) bool {
	matches := regexp.MustCompile(`<version>([^<]+)</version>`).FindAllStringSubmatch(metadata, -1)
	for _, match := range matches {
		if strings.HasPrefix(strings.TrimSpace(match[1]), prefix) {
			return true
		}
	}
	return false
}

func neoForgeVersionPrefix(mcVersion string) string {
	parts := strings.Split(mcVersion, ".")
	if len(parts) < 2 {
		return ""
	}
	if parts[0] == "1" {
		if len(parts) == 2 {
			return parts[1] + ".0."
		}
		return parts[1] + "." + parts[2] + "."
	}
	if len(parts) >= 3 {
		return parts[0] + "." + parts[1] + "." + parts[2] + "."
	}
	return parts[0] + "." + parts[1] + "."
}

func (a *app) searchMods(provider, query string, p *profile) (*modSearchResponse, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "all"
	}
	out := &modSearchResponse{Limit: 30}
	var firstErr error
	if provider == "all" || provider == "modrinth" {
		res, err := searchModrinth(query, p)
		if err != nil {
			firstErr = err
		} else {
			out.Hits = append(out.Hits, res.Hits...)
		}
	}
	if provider == "all" || provider == "curseforge" {
		res, err := a.searchCurseForge(query, p)
		if err != nil {
			if provider == "curseforge" {
				return nil, err
			}
			if firstErr == nil {
				firstErr = err
			}
		} else {
			out.Hits = append(out.Hits, res.Hits...)
		}
	}
	if provider != "all" && provider != "modrinth" && provider != "curseforge" {
		return nil, fmt.Errorf("unsupported mod provider: %s", provider)
	}
	if len(out.Hits) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(out.Hits, func(i, j int) bool {
		if out.Hits[i].Compatible != out.Hits[j].Compatible {
			return out.Hits[i].Compatible
		}
		return out.Hits[i].Downloads > out.Hits[j].Downloads
	})
	out.TotalHits = len(out.Hits)
	return out, nil
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
			hit.Provider = "modrinth"
			hit.ProjectIDAlt = hit.ProjectID
			hit.WebsiteURL = "https://modrinth.com/mod/" + hit.Slug
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

func modrinthVersions(projectID string, p *profile) ([]modVersionOption, error) {
	endpoint, _ := url.Parse("https://api.modrinth.com/v2/project/" + url.PathEscape(projectID) + "/version")
	q := endpoint.Query()
	if p != nil && strings.TrimSpace(p.Loader) != "" {
		q.Set("loaders", jsonArrayString([]string{p.Loader}))
	}
	if p != nil && strings.TrimSpace(p.MinecraftVersion) != "" && p.MinecraftVersion != "latest-compatible" {
		q.Set("game_versions", jsonArrayString([]string{p.MinecraftVersion}))
	}
	endpoint.RawQuery = q.Encode()
	var versions []modrinthVersion
	if err := getJSON(endpoint.String(), &versions); err != nil {
		return nil, err
	}
	out := make([]modVersionOption, 0, len(versions))
	for i, version := range versions {
		fileName := ""
		downloads := 0
		if file, err := primaryModrinthFile(&version); err == nil {
			fileName = file.Filename
			downloads = int(file.Size)
		}
		out = append(out, modVersionOption{
			Provider:      "modrinth",
			ID:            version.ID,
			Name:          version.Name,
			VersionNumber: version.VersionNumber,
			FileName:      fileName,
			GameVersions:  version.GameVersions,
			Loaders:       version.Loaders,
			ReleaseType:   version.VersionType,
			Downloads:     downloads,
			Primary:       i == 0,
		})
	}
	return out, nil
}

func (a *app) curseForgeKeyPath() string {
	return filepath.Join(a.paths().Runtime, "curseforge-api-key")
}

func (a *app) curseForgeAPIKey() string {
	for _, key := range []string{"CURSEFORGE_API_KEY", "CF_API_KEY"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	data, err := os.ReadFile(a.curseForgeKeyPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *app) getCurseForgeJSON(endpoint string, target any) error {
	key := a.curseForgeAPIKey()
	if key == "" {
		return errors.New("CurseForge API key is not configured")
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", key)
	res, err := networkClient.Do(req)
	if err != nil {
		return networkError(endpoint, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("CurseForge request failed: %s %s", res.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func (a *app) searchCurseForge(query string, p *profile) (*modSearchResponse, error) {
	endpoint, _ := url.Parse("https://api.curseforge.com/v1/mods/search")
	q := endpoint.Query()
	q.Set("gameId", "432")
	q.Set("classId", "6")
	q.Set("searchFilter", query)
	q.Set("pageSize", "24")
	q.Set("sortField", "2")
	q.Set("sortOrder", "desc")
	if p != nil && p.MinecraftVersion != "" && p.MinecraftVersion != "latest-compatible" {
		q.Set("gameVersion", p.MinecraftVersion)
	}
	loader := ""
	if p != nil {
		loader = p.Loader
	}
	if loaderType := curseForgeModLoaderType(loader); loaderType > 0 {
		q.Set("modLoaderType", strconv.Itoa(loaderType))
	}
	endpoint.RawQuery = q.Encode()
	var response curseForgeSearchResponse
	if err := a.getCurseForgeJSON(endpoint.String(), &response); err != nil {
		return nil, err
	}
	out := &modSearchResponse{Limit: 24, TotalHits: response.Pagination.TotalCount}
	for _, mod := range response.Data {
		hit := curseForgeModToSearchHit(mod, p)
		if hit.ServerSide == "unsupported" {
			continue
		}
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

func curseForgeModToSearchHit(mod curseForgeMod, p *profile) modSearchHit {
	categories := []string{}
	for _, category := range mod.Categories {
		label := strings.TrimSpace(category.Name)
		if label == "" {
			label = strings.TrimSpace(category.Slug)
		}
		if label != "" && !stringSliceContains(categories, label) {
			categories = append(categories, label)
		}
	}
	versions := []string{}
	loaders := []string{}
	latestFile := ""
	for _, file := range mod.LatestFilesIndexes {
		if file.GameVersion != "" && !stringSliceContains(versions, file.GameVersion) {
			versions = append(versions, file.GameVersion)
		}
		loader := curseForgeLoaderName(file.ModLoader)
		if loader != "" && !stringSliceContains(loaders, loader) {
			loaders = append(loaders, loader)
		}
		if latestFile == "" && file.Filename != "" {
			latestFile = file.Filename
		}
	}
	author := ""
	if len(mod.Authors) > 0 {
		author = mod.Authors[0].Name
	}
	icon := mod.Logo.ThumbnailURL
	if icon == "" {
		icon = mod.Logo.URL
	}
	id := strconv.Itoa(mod.ID)
	hit := modSearchHit{
		Provider:         "curseforge",
		ProjectID:        id,
		ProjectIDAlt:     id,
		Slug:             mod.Slug,
		Title:            mod.Name,
		Description:      mod.Summary,
		ProjectType:      "mod",
		Categories:       categories,
		Versions:         versions,
		Downloads:        mod.DownloadCount,
		ClientSide:       "unknown",
		ServerSide:       "unknown",
		IconURL:          icon,
		LatestVersion:    latestFile,
		WebsiteURL:       mod.Links.WebsiteURL,
		Author:           author,
		SupportedLoaders: loaders,
	}
	versionOK := p == nil || p.MinecraftVersion == "" || p.MinecraftVersion == "latest-compatible" || stringSliceContains(versions, p.MinecraftVersion)
	loaderOK := p == nil || p.Loader == "" || curseForgeLoaderMatches(p.Loader, loaders)
	hit.Compatible = versionOK && loaderOK
	switch {
	case hit.Compatible:
		hit.CompatibilityReason = "Compatible with this server"
	case !versionOK && !loaderOK:
		hit.CompatibilityReason = fmt.Sprintf("No file for Minecraft %s and %s", p.MinecraftVersion, displayLoader(p.Loader))
	case !versionOK:
		hit.CompatibilityReason = "No file for Minecraft " + p.MinecraftVersion
	case !loaderOK:
		if len(loaders) == 0 {
			hit.CompatibilityReason = "No loader metadata from CurseForge"
		} else {
			hit.CompatibilityReason = fmt.Sprintf("Requires %s, not %s", strings.Join(loaders, ", "), displayLoader(p.Loader))
		}
	}
	return hit
}

func (a *app) curseForgeVersions(projectID string, p *profile) ([]modVersionOption, error) {
	modID, err := strconv.Atoi(strings.TrimSpace(projectID))
	if err != nil {
		return nil, errors.New("CurseForge projectId must be numeric")
	}
	files, err := a.curseForgeFiles(modID, p)
	if err != nil {
		return nil, err
	}
	out := make([]modVersionOption, 0, len(files))
	for i, file := range files {
		out = append(out, modVersionOption{
			Provider:      "curseforge",
			ID:            strconv.Itoa(file.ID),
			Name:          file.DisplayName,
			VersionNumber: file.DisplayName,
			FileName:      file.FileName,
			GameVersions:  file.GameVersions,
			ReleaseType:   curseForgeReleaseType(file.ReleaseType),
			Downloads:     file.DownloadCount,
			Date:          file.FileDate,
			Primary:       i == 0,
		})
	}
	return out, nil
}

func (a *app) curseForgeFiles(modID int, p *profile) ([]curseForgeFile, error) {
	endpoint, _ := url.Parse(fmt.Sprintf("https://api.curseforge.com/v1/mods/%d/files", modID))
	q := endpoint.Query()
	q.Set("pageSize", "50")
	if p != nil && p.MinecraftVersion != "" && p.MinecraftVersion != "latest-compatible" {
		q.Set("gameVersion", p.MinecraftVersion)
	}
	loader := ""
	if p != nil {
		loader = p.Loader
	}
	if loaderType := curseForgeModLoaderType(loader); loaderType > 0 {
		q.Set("modLoaderType", strconv.Itoa(loaderType))
	}
	endpoint.RawQuery = q.Encode()
	var response curseForgeFilesResponse
	if err := a.getCurseForgeJSON(endpoint.String(), &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (a *app) curseForgeFile(modID, fileID int) (curseForgeFile, error) {
	var response curseForgeFileResponse
	err := a.getCurseForgeJSON(fmt.Sprintf("https://api.curseforge.com/v1/mods/%d/files/%d", modID, fileID), &response)
	return response.Data, err
}

func (a *app) curseForgeDownloadURL(modID, fileID int) (string, error) {
	var response curseForgeDownloadURLResponse
	err := a.getCurseForgeJSON(fmt.Sprintf("https://api.curseforge.com/v1/mods/%d/files/%d/download-url", modID, fileID), &response)
	return response.Data, err
}

func (a *app) installCurseForgeProjectFor(serverID string, p *profile, projectID, versionID string) ([]lockedInstall, error) {
	modID, err := strconv.Atoi(strings.TrimSpace(projectID))
	if err != nil {
		return nil, errors.New("CurseForge projectId must be numeric")
	}
	var file curseForgeFile
	if strings.TrimSpace(versionID) != "" {
		fileID, err := strconv.Atoi(strings.TrimSpace(versionID))
		if err != nil {
			return nil, errors.New("CurseForge versionId must be numeric")
		}
		file, err = a.curseForgeFile(modID, fileID)
		if err != nil {
			return nil, err
		}
	} else {
		files, err := a.curseForgeFiles(modID, p)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, errors.New("no compatible CurseForge file found")
		}
		file = files[0]
		for _, candidate := range files {
			if candidate.ReleaseType == 1 {
				file = candidate
				break
			}
		}
	}
	downloadURL := strings.TrimSpace(file.DownloadURL)
	if downloadURL == "" {
		downloadURL, err = a.curseForgeDownloadURL(modID, file.ID)
		if err != nil {
			return nil, err
		}
	}
	if downloadURL == "" {
		return nil, errors.New("CurseForge did not provide a download URL for this file")
	}
	targetFolder := targetFolderForLoader(p.Loader)
	targetDir := filepath.Join(a.serverDir(serverID), targetFolder)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	targetPath := filepath.Join(targetDir, filepath.Base(file.FileName))
	if err := downloadFile(downloadURL, targetPath); err != nil {
		return nil, err
	}
	item := lockedInstall{
		Source:       "curseforge",
		ProjectID:    strconv.Itoa(modID),
		VersionID:    strconv.Itoa(file.ID),
		Name:         file.DisplayName,
		FileName:     filepath.Base(file.FileName),
		TargetFolder: targetFolder,
		InstalledAt:  time.Now().UTC(),
		PhoneSafety:  phoneSafety(file.DisplayName, file.FileLength),
	}
	lock, _ := a.loadLockfileFor(serverID)
	lock.MinecraftVersion = p.MinecraftVersion
	lock.Loader = p.Loader
	lock.Installed = upsertInstall(lock.Installed, item)
	if err := a.saveLockfileFor(serverID, lock); err != nil {
		return nil, err
	}
	return []lockedInstall{item}, nil
}

func curseForgeModLoaderType(loader string) int {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "forge":
		return 1
	case "fabric":
		return 4
	case "quilt":
		return 5
	case "neoforge":
		return 6
	default:
		return 0
	}
}

func curseForgeLoaderName(value int) string {
	switch value {
	case 1:
		return "Forge"
	case 4:
		return "Fabric"
	case 5:
		return "Quilt"
	case 6:
		return "NeoForge"
	default:
		return ""
	}
}

func curseForgeLoaderMatches(loader string, names []string) bool {
	if len(names) == 0 {
		return true
	}
	want := strings.ToLower(displayLoader(loader))
	for _, name := range names {
		if strings.EqualFold(strings.TrimSpace(name), want) {
			return true
		}
	}
	return false
}

func curseForgeReleaseType(value int) string {
	switch value {
	case 1:
		return "release"
	case 2:
		return "beta"
	case 3:
		return "alpha"
	default:
		return "unknown"
	}
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

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
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

func unzipWorldArchive(sourceZip, serverDir, worldName string) error {
	reader, err := zip.OpenReader(sourceZip)
	if err != nil {
		return err
	}
	defer reader.Close()
	stripPrefix := ""
	rootWorld := false
	topLevelWithLevel := map[string]bool{}
	for _, file := range reader.File {
		name := filepath.ToSlash(strings.TrimPrefix(file.Name, "/"))
		if name == "" || strings.Contains(name, "../") {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) == 1 && strings.EqualFold(parts[0], "level.dat") {
			rootWorld = true
			break
		}
		if len(parts) >= 2 && strings.EqualFold(parts[1], "level.dat") {
			topLevelWithLevel[parts[0]] = true
		}
		if strings.HasPrefix(name, "region/") || strings.HasPrefix(name, "DIM-1/") || strings.HasPrefix(name, "DIM1/") {
			rootWorld = true
			break
		}
	}
	targetDir := serverDir
	if rootWorld {
		targetDir = filepath.Join(serverDir, worldName)
	} else if len(topLevelWithLevel) == 1 {
		for top := range topLevelWithLevel {
			stripPrefix = strings.TrimSuffix(top, "/") + "/"
		}
		targetDir = filepath.Join(serverDir, worldName)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	for _, file := range reader.File {
		name := filepath.ToSlash(strings.TrimPrefix(file.Name, "/"))
		if stripPrefix != "" {
			if !strings.HasPrefix(name, stripPrefix) {
				continue
			}
			name = strings.TrimPrefix(name, stripPrefix)
		}
		if name == "" {
			continue
		}
		target := filepath.Join(targetDir, filepath.FromSlash(name))
		cleanTarget := filepath.Clean(target)
		cleanRoot := filepath.Clean(targetDir)
		if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in world upload: %s", file.Name)
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
