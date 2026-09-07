# ADB Phone Controller (Go + Fyne)

Cross-platform desktop GUI for controlling Android devices via ADB.
Rewritten from the original Python/Tkinter version.

**Repository:** https://github.com/kekih/adb-phone-controller-go

## Download (Windows .exe)

1. Open **[Actions](https://github.com/kekih/adb-phone-controller-go/actions)**
2. Open the latest successful **Build** run
3. Download the artifact **`adb-phone-controller-windows-amd64`**
4. Unzip and run `adb-phone-controller.exe`

Or create a release yourself:
- Push a tag: `git tag v0.1.0 && git push origin v0.1.0`
- The workflow will attach the `.exe` to the GitHub Release automatically.

## Features (v0.1.0)

- Device discovery and selection (USB + Wi-Fi)
- Device info (model, battery)
- Screenshot (view + save PNG)
- Application list + open app
- Basic controls: Home / Back / Recent / Power / Volume, text input, coordinate tap
- **File manager**: browse, download, upload, delete, create folder
- **APK Manager**: install APK (with -r/-d/-g), uninstall packages
- Wi-Fi ADB (tcpip + connect)
- Dark theme

More features (monitoring, logcat, permissions, real-time mirroring...) coming step by step.

## Requirements

- ADB (platform-tools) in PATH
- USB debugging enabled on the phone

## Build from source

```bash
git clone https://github.com/kekih/adb-phone-controller-go.git
cd adb-phone-controller-go
go mod tidy
go run .
```

### Windows .exe (on Windows with MinGW)

```bash
go build -ldflags="-s -w -H windowsgui" -o adb-phone-controller.exe .
```

## License

MIT
