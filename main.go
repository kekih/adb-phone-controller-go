package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kekih/adb-phone-controller-go/adb"
)

type AppUI struct {
	app            fyne.App
	window         fyne.Window
	client         *adb.Client
	serial         string
	deviceLabel    *widget.Label
	batteryLabel   *widget.Label
	statusLabel    *widget.Label
	screenshotImg  *canvas.Image
	screenWrap     *fyne.Container
	appList        *widget.List
	allApps        []string
	includeSystem  bool
	selectedApp    string
	filePathEntry  *widget.Entry
	fileList       *widget.List
	fileEntries    []adb.FileEntry
	currentPath    string
	selectedFile   *adb.FileEntry
	apkPathEntry   *widget.Entry
	uninstallList  *widget.List
	uninstallPkgs  []string
	selectedUninst string
	optR, optD, optG bool

	// live mirror
	mirrorRunning  atomic.Bool
	mirrorStop     chan struct{}
	mirrorInterval time.Duration
	mirrorBtn      *widget.Button
	mirrorFPSLabel *widget.Label
	sourceW, sourceH int
	frameMu        sync.Mutex
	lastFrame      image.Image
	mirrorBusy     atomic.Bool
}

func main() {
	if !adb.AdbAvailable() {
		fmt.Println("ADB not found in PATH. Install Android platform-tools and add adb to PATH.")
		os.Exit(1)
	}

	a := app.NewWithID("com.kekih.adbphonecontroller")
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("ADB Phone Controller (Go + Fyne)")
	w.Resize(fyne.NewSize(1150, 780))
	w.SetMaster()

	ui := &AppUI{
		app:            a,
		window:         w,
		currentPath:    "/sdcard",
		mirrorInterval: 400 * time.Millisecond,
	}
	ui.build()
	w.SetOnClosed(func() {
		ui.stopMirror()
	})
	w.ShowAndRun()
}

func (ui *AppUI) setStatus(msg string) {
	ui.statusLabel.SetText(msg)
}

func (ui *AppUI) build() {
	ui.deviceLabel = widget.NewLabel("Device: —")
	ui.deviceLabel.TextStyle = fyne.TextStyle{Bold: true}
	ui.batteryLabel = widget.NewLabel("Battery: —")
	ui.statusLabel = widget.NewLabel("Ready")

	header := container.NewHBox(
		ui.deviceLabel,
		ui.batteryLabel,
		layout.NewSpacer(),
		widget.NewButton("Refresh info", ui.refreshHeader),
		widget.NewButton("Change device", ui.showDeviceDialog),
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("Screen", ui.buildScreenTab()),
		container.NewTabItem("Control", ui.buildControlTab()),
		container.NewTabItem("Apps", ui.buildAppsTab()),
		container.NewTabItem("Files", ui.buildFilesTab()),
		container.NewTabItem("APK", ui.buildAPKTab()),
		container.NewTabItem("Wi-Fi ADB", ui.buildWifiTab()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	statusBar := container.NewHBox(ui.statusLabel)
	ui.window.SetContent(container.NewBorder(header, statusBar, nil, nil, tabs))

	go func() {
		time.Sleep(300 * time.Millisecond)
		ui.showDeviceDialog()
	}()
}

func (ui *AppUI) showDeviceDialog() {
	devices, err := adb.ListDevices()
	if err != nil {
		dialog.ShowError(err, ui.window)
		return
	}
	if len(devices) == 0 {
		dialog.ShowInformation("No devices", "No devices found.\nConnect a phone via USB (USB debugging on) and try again.", ui.window)
		return
	}
	var options, serials []string
	for _, d := range devices {
		st := d.State
		if st == "device" {
			st = "ready"
		}
		options = append(options, fmt.Sprintf("%s  |  %s  |  %s", d.Serial, d.Model, st))
		serials = append(serials, d.Serial)
	}
	selected := 0
	list := widget.NewRadioGroup(options, func(s string) {
		for i, o := range options {
			if o == s {
				selected = i
				break
			}
		}
	})
	list.SetSelected(options[0])
	d := dialog.NewCustomConfirm("Select device", "Connect", "Cancel", list, func(ok bool) {
		if !ok {
			return
		}
		serial := serials[selected]
		for _, d := range devices {
			if d.Serial == serial && d.State != "device" {
				dialog.ShowInformation("Unavailable", "Device not ready: "+d.State, ui.window)
				return
			}
		}
		ui.setDevice(serial)
	}, ui.window)
	d.Resize(fyne.NewSize(520, 300))
	d.Show()
}

func (ui *AppUI) setDevice(serial string) {
	ui.stopMirror()
	ui.serial = serial
	ui.client = &adb.Client{Serial: serial}
	ui.deviceLabel.SetText("Device: " + serial)
	ui.refreshHeader()
	ui.refreshAppList()
	ui.refreshFileList()
	ui.refreshUninstallList()
	ui.setStatus("Connected: " + serial)
}

func (ui *AppUI) refreshHeader() {
	if ui.client == nil {
		return
	}
	go func() {
		model := ui.client.GetProp("ro.product.model")
		brand := ui.client.GetProp("ro.product.brand")
		name := strings.TrimSpace(brand + " " + model)
		if name == "" {
			name = ui.serial
		}
		level, status := ui.client.BatteryInfo()
		batt := "Battery: —"
		if level >= 0 {
			batt = fmt.Sprintf("Battery: %d%%", level)
			if status != "" {
				batt += " (" + status + ")"
			}
		}
		ui.deviceLabel.SetText("Device: " + name)
		ui.batteryLabel.SetText(batt)
	}()
}

// ---------- Screen (live mirror) ----------

func (ui *AppUI) buildScreenTab() fyne.CanvasObject {
	ui.screenshotImg = canvas.NewImageFromImage(nil)
	ui.screenshotImg.FillMode = canvas.ImageFillContain
	ui.screenshotImg.SetMinSize(fyne.NewSize(360, 640))

	// Clickable overlay for tap
	tappable := newTappableImage(ui.screenshotImg, ui.onScreenTap)

	ui.mirrorBtn = widget.NewButton("▶ Start live mirror", ui.toggleMirror)
	ui.mirrorFPSLabel = widget.NewLabel("Interval: 400ms")

	intervalSel := widget.NewSelect([]string{"200ms (~5 FPS)", "300ms", "400ms", "500ms", "1s"}, func(s string) {
		switch {
		case strings.HasPrefix(s, "200"):
			ui.mirrorInterval = 200 * time.Millisecond
		case strings.HasPrefix(s, "300"):
			ui.mirrorInterval = 300 * time.Millisecond
		case strings.HasPrefix(s, "400"):
			ui.mirrorInterval = 400 * time.Millisecond
		case strings.HasPrefix(s, "500"):
			ui.mirrorInterval = 500 * time.Millisecond
		default:
			ui.mirrorInterval = time.Second
		}
		ui.mirrorFPSLabel.SetText("Interval: " + s)
	})
	intervalSel.SetSelected("400ms")

	controls := container.NewHBox(
		ui.mirrorBtn,
		widget.NewButton("Single shot", ui.takeScreenshot),
		widget.NewButton("Save PNG...", ui.saveScreenshot),
		widget.NewLabel("Rate:"),
		intervalSel,
		ui.mirrorFPSLabel,
	)
	hint := widget.NewLabel("Click on the image to tap the device (while mirror or after a shot)")
	hint.TextStyle = fyne.TextStyle{Italic: true}

	ui.screenWrap = container.NewBorder(controls, hint, nil, nil, tappable)
	return ui.screenWrap
}

func (ui *AppUI) toggleMirror() {
	if ui.mirrorRunning.Load() {
		ui.stopMirror()
	} else {
		ui.startMirror()
	}
}

func (ui *AppUI) startMirror() {
	if ui.client == nil {
		dialog.ShowInformation("No device", "Select a device first", ui.window)
		return
	}
	if ui.mirrorRunning.Load() {
		return
	}
	ui.mirrorRunning.Store(true)
	ui.mirrorStop = make(chan struct{})
	ui.mirrorBtn.SetText("■ Stop live mirror")
	ui.setStatus("Live mirror started")

	go ui.mirrorLoop()
}

func (ui *AppUI) stopMirror() {
	if !ui.mirrorRunning.Load() {
		return
	}
	ui.mirrorRunning.Store(false)
	if ui.mirrorStop != nil {
		select {
		case <-ui.mirrorStop:
		default:
			close(ui.mirrorStop)
		}
	}
	if ui.mirrorBtn != nil {
		ui.mirrorBtn.SetText("▶ Start live mirror")
	}
	ui.setStatus("Live mirror stopped")
}

func (ui *AppUI) mirrorLoop() {
	frames := 0
	lastFPS := time.Now()
	for ui.mirrorRunning.Load() {
		start := time.Now()
		if ui.mirrorBusy.CompareAndSwap(false, true) {
			data, err := ui.client.ScreenshotPNG(8 * time.Second)
			ui.mirrorBusy.Store(false)
			if err == nil && len(data) > 0 {
				img, _, err := image.Decode(bytes.NewReader(data))
				if err == nil {
					b := img.Bounds()
					ui.frameMu.Lock()
					ui.lastFrame = img
					ui.sourceW, ui.sourceH = b.Dx(), b.Dy()
					ui.frameMu.Unlock()
					ui.screenshotImg.Image = img
					ui.screenshotImg.Refresh()
					frames++
				}
			}
		}
		if time.Since(lastFPS) >= time.Second {
			fps := frames
			frames = 0
			lastFPS = time.Now()
			ui.frameMu.Lock()
			w, h := ui.sourceW, ui.sourceH
			ui.frameMu.Unlock()
			if w > 0 {
				ui.setStatus(fmt.Sprintf("Live mirror  •  %dx%d  •  ~%d FPS", w, h, fps))
			}
		}
		elapsed := time.Since(start)
		sleep := ui.mirrorInterval - elapsed
		if sleep < 50*time.Millisecond {
			sleep = 50 * time.Millisecond
		}
		select {
		case <-ui.mirrorStop:
			return
		case <-time.After(sleep):
		}
	}
}

func (ui *AppUI) onScreenTap(x, y float32) {
	if ui.client == nil {
		return
	}
	ui.frameMu.Lock()
	sw, sh := ui.sourceW, ui.sourceH
	ui.frameMu.Unlock()
	if sw <= 0 || sh <= 0 {
		return
	}
	// Map click coordinates from widget size to device pixels
	sz := ui.screenshotImg.Size()
	if sz.Width <= 0 || sz.Height <= 0 {
		return
	}
	// ImageFillContain: letterboxed
	scale := float32(sw) / sz.Width
	if float32(sh)/sz.Height < scale {
		scale = float32(sh) / sz.Height
	}
	dw := float32(sw) / scale
	dh := float32(sh) / scale
	offX := (sz.Width - dw) / 2
	offY := (sz.Height - dh) / 2
	if x < offX || y < offY || x > offX+dw || y > offY+dh {
		return
	}
	dx := int((x - offX) * scale)
	dy := int((y - offY) * scale)
	if dx < 0 {
		dx = 0
	}
	if dy < 0 {
		dy = 0
	}
	if dx >= sw {
		dx = sw - 1
	}
	if dy >= sh {
		dy = sh - 1
	}
	go func() {
		if err := ui.client.Tap(dx, dy); err != nil {
			ui.setStatus(err.Error())
		} else {
			ui.setStatus(fmt.Sprintf("Tap %d,%d", dx, dy))
		}
	}()
}

func (ui *AppUI) takeScreenshot() {
	if ui.client == nil {
		dialog.ShowInformation("No device", "Select a device first", ui.window)
		return
	}
	ui.setStatus("Taking screenshot...")
	go func() {
		data, err := ui.client.ScreenshotPNG(20 * time.Second)
		if err != nil {
			ui.setStatus("Screenshot failed: " + err.Error())
			return
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			ui.setStatus("Decode failed: " + err.Error())
			return
		}
		b := img.Bounds()
		ui.frameMu.Lock()
		ui.lastFrame = img
		ui.sourceW, ui.sourceH = b.Dx(), b.Dy()
		ui.frameMu.Unlock()
		ui.screenshotImg.Image = img
		ui.screenshotImg.Refresh()
		ui.setStatus(fmt.Sprintf("Screenshot OK (%dx%d)", b.Dx(), b.Dy()))
	}()
}

func (ui *AppUI) saveScreenshot() {
	if ui.client == nil {
		return
	}
	dialog.ShowFileSave(func(uc fyne.URIWriteCloser, err error) {
		if err != nil || uc == nil {
			return
		}
		defer uc.Close()
		data, err := ui.client.ScreenshotPNG(20 * time.Second)
		if err != nil {
			ui.setStatus("Save failed: " + err.Error())
			return
		}
		if _, err = uc.Write(data); err != nil {
			ui.setStatus("Write failed: " + err.Error())
			return
		}
		ui.setStatus("Saved: " + uc.URI().Path())
	}, ui.window)
}

// tappableImage wraps canvas.Image and reports clicks.
type tappableImage struct {
	widget.BaseWidget
	img    *canvas.Image
	onTap  func(x, y float32)
}

func newTappableImage(img *canvas.Image, onTap func(x, y float32)) *tappableImage {
	t := &tappableImage{img: img, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableImage) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.img)
}

func (t *tappableImage) Tapped(e *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap(e.Position.X, e.Position.Y)
	}
}

func (t *tappableImage) TappedSecondary(*fyne.PointEvent) {}

func (t *tappableImage) Cursor() desktop.Cursor {
	return desktop.CrosshairCursor
}

func (t *tappableImage) MinSize() fyne.Size {
	return t.img.MinSize()
}

// ---------- Control ----------

func (ui *AppUI) buildControlTab() fyne.CanvasObject {
	keys := container.NewGridWithColumns(3,
		widget.NewButton("Home", func() { ui.pressKey(3) }),
		widget.NewButton("Back", func() { ui.pressKey(4) }),
		widget.NewButton("Recent", func() { ui.pressKey(187) }),
		widget.NewButton("Power", func() { ui.pressKey(26) }),
		widget.NewButton("Vol +", func() { ui.pressKey(24) }),
		widget.NewButton("Vol −", func() { ui.pressKey(25) }),
	)
	textEntry := widget.NewEntry()
	textEntry.SetPlaceHolder("Text to send...")
	sendBtn := widget.NewButton("Send text", func() {
		if ui.client == nil || textEntry.Text == "" {
			return
		}
		t := textEntry.Text
		go func() {
			if err := ui.client.InputText(t); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Text sent")
			}
		}()
	})
	tapX, tapY := widget.NewEntry(), widget.NewEntry()
	tapX.SetText("500")
	tapY.SetText("1000")
	tapBtn := widget.NewButton("Tap", func() {
		if ui.client == nil {
			return
		}
		x, e1 := strconv.Atoi(strings.TrimSpace(tapX.Text))
		y, e2 := strconv.Atoi(strings.TrimSpace(tapY.Text))
		if e1 != nil || e2 != nil {
			ui.setStatus("Invalid coordinates")
			return
		}
		go func() {
			if err := ui.client.Tap(x, y); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus(fmt.Sprintf("Tapped %d,%d", x, y))
			}
		}()
	})
	return container.NewPadded(container.NewVBox(
		widget.NewLabel("Device buttons"), keys,
		widget.NewSeparator(),
		widget.NewLabel("Text input"),
		container.NewBorder(nil, nil, nil, sendBtn, textEntry),
		widget.NewSeparator(),
		widget.NewLabel("Tap by coordinates"),
		container.NewHBox(widget.NewLabel("X"), tapX, widget.NewLabel("Y"), tapY, tapBtn),
	))
}

func (ui *AppUI) pressKey(code int) {
	if ui.client == nil {
		return
	}
	go func() {
		if err := ui.client.PressKey(code); err != nil {
			ui.setStatus(err.Error())
		} else {
			ui.setStatus("Key sent")
		}
	}()
}

// ---------- Apps ----------

func (ui *AppUI) buildAppsTab() fyne.CanvasObject {
	includeCheck := widget.NewCheck("Show system apps", func(v bool) {
		ui.includeSystem = v
		ui.refreshAppList()
	})
	ui.appList = widget.NewList(
		func() int { return len(ui.allApps) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(ui.allApps[i]) },
	)
	ui.appList.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(ui.allApps) {
			ui.selectedApp = ui.allApps[id]
		}
	}
	top := container.NewHBox(includeCheck,
		widget.NewButton("Refresh", ui.refreshAppList),
		widget.NewButton("Open selected", ui.openSelectedApp),
	)
	return container.NewBorder(top, nil, nil, nil, ui.appList)
}

func (ui *AppUI) refreshAppList() {
	if ui.client == nil {
		return
	}
	ui.setStatus("Loading packages...")
	go func() {
		pkgs, err := ui.client.ListPackages(ui.includeSystem)
		if err != nil {
			ui.setStatus(err.Error())
			return
		}
		ui.allApps = pkgs
		ui.selectedApp = ""
		ui.appList.Refresh()
		ui.setStatus(fmt.Sprintf("Loaded %d packages", len(pkgs)))
	}()
}

func (ui *AppUI) openSelectedApp() {
	if ui.client == nil || ui.selectedApp == "" {
		ui.setStatus("Select an app first")
		return
	}
	pkg := ui.selectedApp
	ui.setStatus("Opening " + pkg + "...")
	go func() {
		if err := ui.client.OpenApp(pkg); err != nil {
			ui.setStatus(err.Error())
		} else {
			ui.setStatus("Opened " + pkg)
		}
	}()
}

// ---------- Files ----------

func (ui *AppUI) buildFilesTab() fyne.CanvasObject {
	ui.filePathEntry = widget.NewEntry()
	ui.filePathEntry.SetText("/sdcard")
	ui.filePathEntry.OnSubmitted = func(s string) { ui.navigateFiles(s) }

	ui.fileList = widget.NewList(
		func() int { return len(ui.fileEntries) },
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewLabel("name"), layout.NewSpacer(), widget.NewLabel("size"))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			e := ui.fileEntries[i]
			box := o.(*fyne.Container)
			name := e.Name
			if e.IsDir {
				name = "[DIR] " + name
			}
			box.Objects[0].(*widget.Label).SetText(name)
			sizeLbl := box.Objects[2].(*widget.Label)
			if e.IsDir {
				sizeLbl.SetText("")
			} else {
				sizeLbl.SetText(adb.HumanSize(e.Size))
			}
		},
	)
	ui.fileList.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(ui.fileEntries) {
			ui.selectedFile = &ui.fileEntries[id]
		}
	}

	nav := container.NewBorder(nil, nil,
		container.NewHBox(
			widget.NewButton("Up", ui.fileGoUp),
			widget.NewButton("/sdcard", func() { ui.navigateFiles("/sdcard") }),
			widget.NewButton("Open dir", ui.fileOpenDir),
		),
		widget.NewButton("Go", func() { ui.navigateFiles(ui.filePathEntry.Text) }),
		ui.filePathEntry,
	)
	actions := container.NewHBox(
		widget.NewButton("Download", ui.fileDownload),
		widget.NewButton("Upload...", ui.fileUpload),
		widget.NewButton("Delete", ui.fileDelete),
		widget.NewButton("New folder", ui.fileMkdir),
		widget.NewButton("Refresh", ui.refreshFileList),
	)
	return container.NewBorder(container.NewVBox(nav, actions), nil, nil, nil, ui.fileList)
}

func (ui *AppUI) navigateFiles(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/sdcard"
	}
	ui.currentPath = path
	ui.filePathEntry.SetText(path)
	ui.refreshFileList()
}

func (ui *AppUI) refreshFileList() {
	if ui.client == nil {
		return
	}
	ui.setStatus("Reading " + ui.currentPath + "...")
	go func() {
		items, err := ui.client.ListFiles(ui.currentPath)
		if err != nil {
			ui.setStatus(err.Error())
			return
		}
		ui.fileEntries = items
		ui.selectedFile = nil
		ui.fileList.Refresh()
		ui.setStatus(fmt.Sprintf("%d items in %s", len(items), ui.currentPath))
	}()
}

func (ui *AppUI) fileGoUp() {
	if ui.currentPath == "/" {
		return
	}
	parent := filepath.Dir(strings.TrimRight(ui.currentPath, "/"))
	if parent == "." || parent == "" {
		parent = "/"
	}
	ui.navigateFiles(parent)
}

func (ui *AppUI) fileOpenDir() {
	if ui.selectedFile != nil && ui.selectedFile.IsDir {
		ui.navigateFiles(ui.selectedFile.Path)
	}
}

func (ui *AppUI) fileDownload() {
	if ui.client == nil || ui.selectedFile == nil || ui.selectedFile.IsDir {
		ui.setStatus("Select a file to download")
		return
	}
	remote := ui.selectedFile.Path
	dialog.ShowFileSave(func(uc fyne.URIWriteCloser, err error) {
		if err != nil || uc == nil {
			return
		}
		local := uc.URI().Path()
		uc.Close()
		ui.setStatus("Downloading...")
		go func() {
			if err := ui.client.PullFile(remote, local); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Downloaded: " + local)
			}
		}()
	}, ui.window)
}

func (ui *AppUI) fileUpload() {
	if ui.client == nil {
		return
	}
	fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
		if err != nil || uc == nil {
			return
		}
		local := uc.URI().Path()
		uc.Close()
		remote := strings.TrimRight(ui.currentPath, "/") + "/" + filepath.Base(local)
		ui.setStatus("Uploading...")
		go func() {
			if err := ui.client.PushFile(local, remote); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Uploaded: " + remote)
				ui.refreshFileList()
			}
		}()
	}, ui.window)
	fd.Show()
}

func (ui *AppUI) fileDelete() {
	if ui.client == nil || ui.selectedFile == nil {
		ui.setStatus("Select item to delete")
		return
	}
	path := ui.selectedFile.Path
	dialog.ShowConfirm("Delete", "Delete "+path+"?", func(ok bool) {
		if !ok {
			return
		}
		go func() {
			if err := ui.client.DeletePath(path); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Deleted")
				ui.refreshFileList()
			}
		}()
	}, ui.window)
}

func (ui *AppUI) fileMkdir() {
	if ui.client == nil {
		return
	}
	entry := widget.NewEntry()
	dialog.ShowForm("New folder", "Create", "Cancel", []*widget.FormItem{
		widget.NewFormItem("Name", entry),
	}, func(ok bool) {
		if !ok || strings.TrimSpace(entry.Text) == "" {
			return
		}
		newPath := strings.TrimRight(ui.currentPath, "/") + "/" + strings.TrimSpace(entry.Text)
		go func() {
			if err := ui.client.Mkdir(newPath); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Folder created")
				ui.refreshFileList()
			}
		}()
	}, ui.window)
}

// ---------- APK ----------

func (ui *AppUI) buildAPKTab() fyne.CanvasObject {
	ui.apkPathEntry = widget.NewEntry()
	ui.apkPathEntry.SetPlaceHolder("Path to .apk file")
	browseBtn := widget.NewButton("Browse...", func() {
		fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
			if err != nil || uc == nil {
				return
			}
			ui.apkPathEntry.SetText(uc.URI().Path())
			uc.Close()
		}, ui.window)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".apk"}))
		fd.Show()
	})
	optR := widget.NewCheck("-r reinstall (keep data)", func(v bool) { ui.optR = v })
	optD := widget.NewCheck("-d allow downgrade", func(v bool) { ui.optD = v })
	optG := widget.NewCheck("-g grant all permissions", func(v bool) { ui.optG = v })
	installBtn := widget.NewButton("Install APK", ui.installAPK)

	installBox := container.NewVBox(
		widget.NewLabel("Install APK"),
		container.NewBorder(nil, nil, nil, browseBtn, ui.apkPathEntry),
		optR, optD, optG,
		installBtn,
	)

	ui.uninstallList = widget.NewList(
		func() int { return len(ui.uninstallPkgs) },
		func() fyne.CanvasObject { return widget.NewLabel("pkg") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(ui.uninstallPkgs[i]) },
	)
	ui.uninstallList.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(ui.uninstallPkgs) {
			ui.selectedUninst = ui.uninstallPkgs[id]
		}
	}
	uninstTop := container.NewHBox(
		widget.NewButton("Refresh packages", ui.refreshUninstallList),
		widget.NewButton("Uninstall", func() { ui.doUninstall(false) }),
		widget.NewButton("Uninstall (keep data)", func() { ui.doUninstall(true) }),
	)
	uninstallBox := container.NewBorder(
		container.NewVBox(widget.NewLabel("Uninstall packages"), uninstTop),
		nil, nil, nil, ui.uninstallList,
	)

	split := container.NewHSplit(container.NewPadded(installBox), uninstallBox)
	split.SetOffset(0.4)
	return split
}

func (ui *AppUI) installAPK() {
	if ui.client == nil {
		return
	}
	path := strings.TrimSpace(ui.apkPathEntry.Text)
	if path == "" {
		ui.setStatus("Select an APK file")
		return
	}
	var opts []string
	if ui.optR {
		opts = append(opts, "-r")
	}
	if ui.optD {
		opts = append(opts, "-d")
	}
	if ui.optG {
		opts = append(opts, "-g")
	}
	ui.setStatus("Installing...")
	go func() {
		if err := ui.client.InstallAPK(path, opts); err != nil {
			ui.setStatus(err.Error())
		} else {
			ui.setStatus("Install OK: " + filepath.Base(path))
			ui.refreshUninstallList()
		}
	}()
}

func (ui *AppUI) refreshUninstallList() {
	if ui.client == nil {
		return
	}
	go func() {
		pkgs, err := ui.client.ListPackages(false)
		if err != nil {
			ui.setStatus(err.Error())
			return
		}
		ui.uninstallPkgs = pkgs
		ui.selectedUninst = ""
		ui.uninstallList.Refresh()
	}()
}

func (ui *AppUI) doUninstall(keepData bool) {
	if ui.client == nil || ui.selectedUninst == "" {
		ui.setStatus("Select a package")
		return
	}
	pkg := ui.selectedUninst
	dialog.ShowConfirm("Uninstall", "Uninstall "+pkg+"?", func(ok bool) {
		if !ok {
			return
		}
		go func() {
			if err := ui.client.UninstallPackage(pkg, keepData); err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Uninstalled " + pkg)
				ui.refreshUninstallList()
			}
		}()
	}, ui.window)
}

// ---------- Wi-Fi ----------

func (ui *AppUI) buildWifiTab() fyne.CanvasObject {
	ipEntry := widget.NewEntry()
	ipEntry.SetPlaceHolder("192.168.1.100")
	portEntry := widget.NewEntry()
	portEntry.SetText("5555")
	enableBtn := widget.NewButton("Enable TCP/IP (current USB)", func() {
		if ui.client == nil {
			ui.setStatus("Select USB device first")
			return
		}
		port, _ := strconv.Atoi(portEntry.Text)
		if port == 0 {
			port = 5555
		}
		go func() {
			msg, err := adb.EnableTCPIP(ui.serial, port)
			if err != nil {
				ui.setStatus(msg + " / " + err.Error())
			} else {
				ui.setStatus(msg)
			}
		}()
	})
	connectBtn := widget.NewButton("Connect", func() {
		ip := strings.TrimSpace(ipEntry.Text)
		port := strings.TrimSpace(portEntry.Text)
		if port == "" {
			port = "5555"
		}
		if ip == "" {
			ui.setStatus("Enter IP")
			return
		}
		go func() {
			msg, err := adb.ConnectWifi(ip + ":" + port)
			if err != nil {
				ui.setStatus(msg + " / " + err.Error())
			} else {
				ui.setStatus(msg)
			}
		}()
	})
	return container.NewPadded(container.NewVBox(
		widget.NewLabel("Wi-Fi ADB"),
		widget.NewForm(
			widget.NewFormItem("IP", ipEntry),
			widget.NewFormItem("Port", portEntry),
		),
		enableBtn, connectBtn,
		widget.NewLabel("After enabling TCP/IP, disconnect USB and connect by IP."),
	))
}
