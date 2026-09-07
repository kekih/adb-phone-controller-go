package adb

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Device represents a connected Android device.
type Device struct {
	Serial string
	State  string
	Model  string
}

// Client is bound to a specific device serial.
type Client struct {
	Serial string
}

// AdbAvailable checks if adb is in PATH.
func AdbAvailable() bool {
	_, err := exec.LookPath("adb")
	return err == nil
}

// ListDevices returns connected devices (serial, state, model).
func ListDevices() ([]Device, error) {
	cmd := exec.Command("adb", "devices", "-l")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("adb devices failed: %w (%s)", err, string(out))
	}

	var devices []Device
	lines := strings.Split(string(out), "\n")
	re := regexp.MustCompile(`^(\S+)\s+(device|offline|unauthorized|recovery|sideload)\b(.*)$`)
	modelRe := regexp.MustCompile(`model:(\S+)`)

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		serial, state, rest := m[1], m[2], m[3]
		model := ""
		if mm := modelRe.FindStringSubmatch(rest); mm != nil {
			model = strings.ReplaceAll(mm[1], "_", " ")
		}
		devices = append(devices, Device{Serial: serial, State: state, Model: model})
	}
	return devices, nil
}

func (c *Client) base() []string {
	return []string{"adb", "-s", c.Serial}
}

// Raw runs an arbitrary adb command for this device.
func (c *Client) Raw(args []string, timeout time.Duration) (stdout, stderr string, code int) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cmd := exec.Command(c.base()[0], append(c.base()[1:], args...)...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), exitErr.ExitCode()
			}
			return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), -1
		}
		return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), 0
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", "command timed out", -1
	}
}

// Shell runs adb shell ...
func (c *Client) Shell(args ...string) (string, string, int) {
	return c.Raw(append([]string{"shell"}, args...), 15*time.Second)
}

// GetProp returns a system property.
func (c *Client) GetProp(name string) string {
	out, _, code := c.Shell("getprop", name)
	if code != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}

// BatteryInfo returns level (0-100) and status text.
func (c *Client) BatteryInfo() (level int, status string) {
	out, _, code := c.Shell("dumpsys", "battery")
	if code != 0 {
		return -1, ""
	}
	level = -1
	status = ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "level:") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "level:"))); err == nil {
				level = v
			}
		} else if strings.HasPrefix(line, "status:") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "status:"))); err == nil {
				switch v {
				case 2:
					status = "Charging"
				case 3:
					status = "Discharging"
				case 4:
					status = "Not charging"
				case 5:
					status = "Full"
				default:
					status = strconv.Itoa(v)
				}
			}
		}
	}
	return level, status
}

// ScreenshotPNG returns raw PNG bytes from the device.
func (c *Client) ScreenshotPNG(timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cmd := exec.Command("adb", "-s", c.Serial, "exec-out", "screencap", "-p")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("screencap failed: %w (%s)", err, errBuf.String())
		}
		data := outBuf.Bytes()
		if len(data) == 0 {
			return nil, fmt.Errorf("empty screenshot")
		}
		return data, nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("screencap timed out")
	}
}

// ListPackages returns package names. includeSystem=false → only third-party (-3).
func (c *Client) ListPackages(includeSystem bool) ([]string, error) {
	args := []string{"pm", "list", "packages", "-3"}
	if includeSystem {
		args = []string{"pm", "list", "packages"}
	}
	out, errStr, code := c.Shell(args...)
	if code != 0 {
		return nil, fmt.Errorf("pm list packages failed: %s", errStr)
	}
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package:") {
			pkgs = append(pkgs, strings.TrimPrefix(line, "package:"))
		}
	}
	return pkgs, nil
}

// OpenApp launches the app via monkey.
func (c *Client) OpenApp(packageName string) error {
	_, errStr, code := c.Shell("monkey", "-p", packageName, "-c", "android.intent.category.LAUNCHER", "1")
	if code != 0 {
		return fmt.Errorf("open app failed: %s", errStr)
	}
	return nil
}

// PressKey sends a keyevent.
func (c *Client) PressKey(keycode int) error {
	_, errStr, code := c.Shell("input", "keyevent", strconv.Itoa(keycode))
	if code != 0 {
		return fmt.Errorf("keyevent failed: %s", errStr)
	}
	return nil
}

// Tap sends a tap at (x, y).
func (c *Client) Tap(x, y int) error {
	_, errStr, code := c.Shell("input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	if code != 0 {
		return fmt.Errorf("tap failed: %s", errStr)
	}
	return nil
}

// InputText sends text (basic escaping).
func (c *Client) InputText(text string) error {
	// Basic sanitization similar to original, but improved a bit.
	safe := strings.ReplaceAll(text, "\\", "")
	safe = strings.ReplaceAll(safe, "'", "")
	safe = strings.ReplaceAll(safe, "\"", "")
	safe = strings.ReplaceAll(safe, "$", "")
	safe = strings.ReplaceAll(safe, "`", "")
	safe = strings.ReplaceAll(safe, " ", "%s")
	_, errStr, code := c.Shell("input", "text", safe)
	if code != 0 {
		return fmt.Errorf("input text failed: %s", errStr)
	}
	return nil
}

// ConnectWifi connects to device over TCP/IP.
func ConnectWifi(ipPort string) (string, error) {
	cmd := exec.Command("adb", "connect", ipPort)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		return msg, err
	}
	return msg, nil
}

// EnableTCPIP enables TCP/IP mode on the given serial.
func EnableTCPIP(serial string, port int) (string, error) {
	cmd := exec.Command("adb", "-s", serial, "tcpip", strconv.Itoa(port))
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		return msg, err
	}
	return msg, nil
}
