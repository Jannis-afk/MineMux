# MineMux MVP Plan

MineMux is a Termux fork that turns an Android phone into a local Minecraft server appliance.

## Current MVP Branch

This branch keeps the Termux package name and bootstrap prefix as `com.termux` for runtime compatibility. That means the APK cannot be installed beside normal Termux, but it lets the MVP use upstream bootstrap packages while validating the product.

The launcher opens the MineMux dashboard. The original terminal remains installed as a recovery screen.

## Runtime Shape

```text
Android app
  MineMuxActivity
  MineMuxRuntime
  MineMuxBootReceiver

Termux home
  ~/minemux/daemon/minemux-daemon
  ~/minemux/daemon/start-minemux.sh
  ~/minemux/webui/index.html
  ~/.termux/boot/00-minemux-daemon

Daemon
  127.0.0.1:8787
  Paper setup/start/stop/restart
  console/log polling
  Modrinth plugin install
  manual jar upload detection
  backups/restore
  diagnostics export
```

## Build Flow

Run this before building the APK:

```sh
./scripts/build-minemux-assets.sh
```

On Windows:

```powershell
.\scripts\build-minemux-assets.ps1
```

Then build the debug APK with Gradle:

```sh
./gradlew :app:assembleDebug
```

## Bootstrap Blocker

A final separately installable package such as `app.minemux` needs custom bootstrap packages built for:

```text
/data/data/app.minemux/files/usr
```

Do not change `TERMUX_PACKAGE_NAME` away from `com.termux` for the MVP branch until that bootstrap exists.

## Today Acceptance Criteria

- APK launches to MineMux dashboard.
- Start button installs runtime assets and starts the daemon.
- Dashboard loads from `http://127.0.0.1:8787`.
- Terminal button opens recovery terminal.
- Boot receiver schedules the MineMux daemon boot script.
- Paper server setup and start are validated on a phone.
