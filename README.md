# ADB Phone Controller (Go + Fyne)

Cross-platform desktop GUI for controlling Android devices via ADB.
Rewritten from the original Python/Tkinter version.

## Features (current MVP)

- Device discovery and selection (USB + Wi-Fi)
- Device info (model, battery level/status)
- Screenshot (view + save PNG)
- Application list (user/system) + open app
- Basic controls: Home / Back / Recent / Power, text input, coordinate tap
- Dark theme

More features (file manager, mirroring, permissions, logcat, batch ops...) will be added step by step.

## Requirements

- Go 1.22+
- ADB (platform-tools) in PATH
- On Linux: additional system packages for Fyne (see [Fyne docs](https://docs.fyne.io/started/))

## Build & Run

```bash
go mod tidy
go run .
```

### Build Windows .exe

```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o adb-phone-controller.exe .
```

GitHub Actions automatically builds the Windows executable on every push to `main` and on releases.

## Download

Check the [Releases](https://github.com/kekih/adb-phone-controller-go/releases) or the Actions artifacts.

## License

MIT
