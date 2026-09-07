package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kekih/adb-phone-controller-go/adb"
)

type AppUI struct {
	app        fyne.App
	window     fyne.Window
	client     *adb.Client
	serial     string
	deviceLabel *widget.Label
	batteryLabel *widget.Label
	statusLabel *widget.Label
	screenshotImg *canvas.Image
	appList    *widget.List
	allApps    []string
	includeSystem bool
}

func main() {
	if !adb.AdbAvailable() {
		fmt.Println("ADB not found in PATH. Install Android platform-tools and add adb to PATH.")
		os.Exit(1)
	}

	a := app.NewWithID("com.kekih.adbphonecontroller")
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("ADB Phone Controller (Go + Fyne)")
	w.Resize(fyne.NewSize(1100, 750))
	w.SetMaster()

	ui := &AppUI{
		app:    a,
		window: w,
	}
	ui.build()
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

	// Tabs
	tabs := container.NewAppTabs(
		container.NewTabItem("Screen", ui.buildScreenTab()),
		container.NewTabItem("Control", ui.buildControlTab()),
		container.NewTabItem("Apps", ui.buildAppsTab()),
		container.NewTabItem("Wi-Fi ADB", ui.buildWifiTab()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	statusBar := container.NewHBox(ui.statusLabel)
	content := container.NewBorder(header, statusBar, nil, nil, tabs)
	ui.window.SetContent(content)

	// Show device dialog on start
	ui.window.Canvas().AddShortcut(&fyne.Shortcut{KeyName: ""}, nil) // noop
	go func() {
		time.Sleep(200 * time.Millisecond)
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
		dialog.ShowInformation("No devices", "No devices found.\nConnect a phone via USB (with USB debugging enabled) and try again.", ui.window)
		return
	}

	var options []string
	var serials []string
	for _, d := range devices {
		state := d.State
		if state == "device" {
			state = "ready"
		}
		label := fmt.Sprintf("%s  |  %s  |  %s", d.Serial, d.Model, state)
		options = append(options, label)
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
		// Only allow "device" state
		for _, d := range devices {
			if d.Serial == serial && d.State != "device" {
				dialog.ShowInformation("Unavailable", "This device is not ready: "+d.State, ui.window)
				return
			}
		}
		ui.setDevice(serial)
	}, ui.window)
	d.Resize(fyne.NewSize(520, 300))
	d.Show()
}

func (ui *AppUI) setDevice(serial string) {
	ui.serial = serial
	ui.client = &adb.Client{Serial: serial}
	ui.deviceLabel.SetText("Device: " + serial)
	ui.refreshHeader()
	ui.refreshAppList()
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

// ---------- Screen tab ----------

func (ui *AppUI) buildScreenTab() fyne.CanvasObject {
	ui.screenshotImg = canvas.NewImageFromImage(nil)
	ui.screenshotImg.FillMode = canvas.ImageFillContain
	ui.screenshotImg.SetMinSize(fyne.NewSize(400, 600))

	btnShot := widget.NewButton("Take screenshot", ui.takeScreenshot)
	btnSave := widget.NewButton("Save PNG...", ui.saveScreenshot)

	controls := container.NewHBox(btnShot, btnSave)
	return container.NewBorder(controls, nil, nil, nil, ui.screenshotImg)
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
		ui.screenshotImg.Image = img
		ui.screenshotImg.Refresh()
		ui.setStatus(fmt.Sprintf("Screenshot OK (%dx%d)", img.Bounds().Dx(), img.Bounds().Dy()))
	}()
}

func (ui *AppUI) saveScreenshot() {
	if ui.screenshotImg.Image == nil {
		dialog.ShowInformation("No image", "Take a screenshot first", ui.window)
		return
	}
	dialog.ShowFileSave(func(uc fyne.URIWriteCloser, err error) {
		if err != nil || uc == nil {
			return
		}
		defer uc.Close()
		// Re-take to get fresh PNG bytes (simpler than re-encoding)
		data, err := ui.client.ScreenshotPNG(20 * time.Second)
		if err != nil {
			ui.setStatus("Save failed: " + err.Error())
			return
		}
		_, err = uc.Write(data)
		if err != nil {
			ui.setStatus("Write failed: " + err.Error())
			return
		}
		ui.setStatus("Saved: " + uc.URI().Path())
	}, ui.window)
}

// ---------- Control tab ----------

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
	textEntry.SetPlaceHolder("Text to send to device...")
	sendTextBtn := widget.NewButton("Send text", func() {
		if ui.client == nil {
			return
		}
		t := textEntry.Text
		if t == "" {
			return
		}
		go func() {
			err := ui.client.InputText(t)
			if err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus("Text sent")
			}
		}()
	})

	tapX := widget.NewEntry()
	tapX.SetText("500")
	tapY := widget.NewEntry()
	tapY.SetText("1000")
	tapBtn := widget.NewButton("Tap", func() {
		if ui.client == nil {
			return
		}
		x, err1 := strconv.Atoi(strings.TrimSpace(tapX.Text))
		y, err2 := strconv.Atoi(strings.TrimSpace(tapY.Text))
		if err1 != nil || err2 != nil {
			ui.setStatus("Invalid coordinates")
			return
		}
		go func() {
			err := ui.client.Tap(x, y)
			if err != nil {
				ui.setStatus(err.Error())
			} else {
				ui.setStatus(fmt.Sprintf("Tapped %d,%d", x, y))
			}
		}()
	})

	form := container.NewVBox(
		widget.NewLabel("Device buttons"),
		keys,
		widget.NewSeparator(),
		widget.NewLabel("Text input"),
		container.NewBorder(nil, nil, nil, sendTextBtn, textEntry),
		widget.NewSeparator(),
		widget.NewLabel("Tap by coordinates"),
		container.NewHBox(widget.NewLabel("X"), tapX, widget.NewLabel("Y"), tapY, tapBtn),
	)
	return container.NewPadded(form)
}

func (ui *AppUI) pressKey(code int) {
	if ui.client == nil {
		return
	}
	go func() {
		err := ui.client.PressKey(code)
		if err != nil {
			ui.setStatus(err.Error())
		} else {
			ui.setStatus("Key sent")
		}
	}()
}

// ---------- Apps tab ----------

func (ui *AppUI) buildAppsTab() fyne.CanvasObject {
	includeCheck := widget.NewCheck("Show system apps", func(v bool) {
		ui.includeSystem = v
		ui.refreshAppList()
	})
	refreshBtn := widget.NewButton("Refresh list", ui.refreshAppList)
	openBtn := widget.NewButton("Open selected", ui.openSelectedApp)

	ui.appList = widget.NewList(
		func() int { return len(ui.allApps) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(ui.allApps[i])
		},
	)
	ui.appList.OnSelected = func(id widget.ListItemID) {
		// keep selection for open
	}

	top := container.NewHBox(includeCheck, refreshBtn, openBtn)
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
		ui.appList.Refresh()
		ui.setStatus(fmt.Sprintf("Loaded %d packages", len(pkgs)))
	}()
}

func (ui *AppUI) openSelectedApp() {
	if ui.client == nil {
		return
	}
	id := ui.appList // no direct selected getter in older Fyne; use a simple approach
	// For simplicity we open the first selected via a stored index
	// Fyne List doesn't expose selected easily in all versions; use OnSelected to store.
	// Quick fix: open by asking user or keep last selected.
	// We'll store last selected.
}

var lastSelectedApp string

func init() {
	// placeholder – will set properly below
}

// Fix openSelectedApp properly in the build

func (ui *AppUI) openSelectedAppFixed() {
	if ui.client == nil || lastSelectedApp == "" {
		ui.setStatus("Select an app first")
		return
	}
	ui.setStatus("Opening " + lastSelectedApp + "...")
	go func() {
		err := ui.client.OpenApp(lastSelectedApp)
		if err != nil {
			ui.setStatus(err.Error())
		} else {
			ui.setStatus("Opened " + lastSelectedApp)
		}
	}()
}

// ---------- Wi-Fi tab ----------

func (ui *AppUI) buildWifiTab() fyne.CanvasObject {
	ipEntry := widget.NewEntry()
	ipEntry.SetPlaceHolder("192.168.1.100")
	portEntry := widget.NewEntry()
	portEntry.SetText("5555")

	enableBtn := widget.NewButton("Enable TCP/IP (current USB device)", func() {
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
		target := ip + ":" + port
		go func() {
			msg, err := adb.ConnectWifi(target)
			if err != nil {
				ui.setStatus(msg + " / " + err.Error())
			} else {
				ui.setStatus(msg)
				// refresh device list after connect
			}
		}()
	})

	form := container.NewVBox(
		widget.NewLabel("Wi-Fi ADB"),
		widget.NewForm(
			widget.NewFormItem("IP", ipEntry),
			widget.NewFormItem("Port", portEntry),
		),
		enableBtn,
		connectBtn,
		widget.NewLabel("After enabling TCP/IP on USB, disconnect USB and connect via IP."),
	)
	return container.NewPadded(form)
}
