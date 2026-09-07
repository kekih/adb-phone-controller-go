# ADB Phone Controller (Go + Fyne)

Cross-platform desktop GUI for controlling Android devices via ADB.

**https://github.com/kekih/adb-phone-controller-go**

## Download Windows .exe

1. Open **[Actions](https://github.com/kekih/adb-phone-controller-go/actions)**
2. Latest successful **Build** → artifact **`adb-phone-controller-windows-amd64`**
3. Unzip → run `adb-phone-controller.exe`

Or tag a release: `git tag v0.2.0 && git push origin v0.2.0` — the workflow attaches the exe to Releases.

## Features

- Device selection (USB + Wi-Fi ADB)
- Device info (model, battery)
- **Live screen mirror** (continuous screenshots, adjustable rate ~2–5 FPS)
- **Click on screen image to tap** the device
- Single screenshot + save PNG
- App list + open
- Hardware keys, text input, coordinate tap
- File manager (browse / download / upload / delete / mkdir)
- APK install (options -r/-d/-g) + uninstall
- **ADB console windows are hidden on Windows** (no flashing CMD)
- Dark theme

## Requirements

- ADB (platform-tools) in PATH
- USB debugging enabled

## Build

```bash
go mod tidy
go run .
```

Windows:
```bash
go build -ldflags="-s -w -H windowsgui" -o adb-phone-controller.exe .
```

## Note on live mirror

Real-time view uses repeated `screencap` (no H.264 pipeline yet). Latency depends on device/USB (~200–500 ms). For true low-latency mirroring use [scrcpy](https://github.com/Genymobile/scrcpy) alongside this tool; a future version may integrate it.

## License

MIT
