$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Assets = Join-Path $Root "app/src/main/assets/minemux"
$WebAssets = Join-Path $Assets "webui"

New-Item -ItemType Directory -Force $Assets | Out-Null
New-Item -ItemType Directory -Force $WebAssets | Out-Null

$GoCommand = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $GoCommand) {
    $DefaultGo = "C:\Program Files\Go\bin\go.exe"
    if (Test-Path $DefaultGo) {
        $GoCommand = $DefaultGo
    }
}

if ($GoCommand) {
    Push-Location (Join-Path $Root "daemon")
    try {
        $env:GOOS = "android"
        $env:GOARCH = "arm64"
        & $GoCommand build -o (Join-Path $Assets "minemux-daemon") ./cmd/minemux-daemon
    } finally {
        Pop-Location
        Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
        Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
    }
} else {
    Write-Warning "go is not installed; keeping existing $Assets/minemux-daemon"
}

Copy-Item (Join-Path $Root "webui/index.html") (Join-Path $WebAssets "index.html") -Force
Write-Output $Assets
