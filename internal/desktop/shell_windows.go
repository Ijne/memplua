//go:build windows && desktop

package desktop

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	appruntime "crawler/internal/runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:assets
var assets embed.FS

// Shell exposes only desktop capabilities. All knowledge and source operations
// (including tray actions) go through the authenticated REST boundary.
type Shell struct {
	app              *application.App
	runtime          *appruntime.Runtime
	options          appruntime.Options
	ctx              context.Context
	mu               sync.Mutex
	windowMu         sync.Mutex
	windows          map[string]*application.WebviewWindow
	state            uiState
	statePath        string
	saveTimer        *time.Timer
	closing          bool
	monitorSignature string
	tray             *application.SystemTray
	trayLanguage     string
}

// BootstrapData is the in-memory desktop connection/preferences payload. Token
// is a per-process local credential, not a user-managed API key.
type BootstrapData struct {
	APIAddress  string `json:"apiAddress"`
	Token       string `json:"token"`
	Locale      string `json:"locale"`
	SystemTheme string `json:"systemTheme"`
	Autostart   bool   `json:"autostart"`
}

const (
	widgetClosedWidth = 371
	widgetMaxWidth    = 483
	widgetHeight      = 48
)

// Run starts the single-instance Windows shell and shared runtime, waits for the
// local API, and owns graceful shutdown.
func Run(options appruntime.Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shell := &Shell{options: options, ctx: ctx, windows: map[string]*application.WebviewWindow{}, statePath: filepath.Join(options.Config.DataDir, "ui-state.json")}
	shell.state = loadState(shell.statePath)
	frontend, err := fs.Sub(assets, "assets")
	if err != nil {
		return err
	}
	// Wails acquires its single-instance lock in New, before the runtime opens
	// the API listener or database. Repeated launch only activates the widget.
	shell.app = application.New(application.Options{
		Name: "memplua", Description: "A calm workspace for captured knowledge", Icon: brandIcon(256),
		Services:       []application.Service{application.NewService(shell)},
		Assets:         application.AssetOptions{Handler: application.AssetFileServerFS(frontend), DisableLogging: true},
		LogLevel:       slog.LevelWarn,
		Windows:        application.WindowsOptions{DisableQuitOnLastWindowClosed: true, WebviewUserDataPath: filepath.Join(options.Config.DataDir, "webview"), AdditionalBrowserArgs: debugBrowserArgs()},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: desktopInstanceID(), OnSecondInstanceLaunch: func(application.SecondInstanceData) { _ = shell.OpenWindow("widget", "") }},
	})
	options.Desktop = true
	shell.runtime, err = appruntime.New(options)
	if err != nil {
		message := "memplua could not start. Check the configuration and the application log."
		if errors.Is(err, syscall.Errno(10048)) {
			message = "memplua cannot use its local API port because another application is already using it. Close the other memplua or headless instance, or choose a different API port in the configuration."
		}
		if options.Config.UI.Language == "ru" {
			message = "Не удалось запустить memplua. Проверьте конфигурацию и журнал приложения."
			if errors.Is(err, syscall.Errno(10048)) {
				message = "Локальный порт memplua занят другим приложением. Закройте другой экземпляр memplua или headless-сервер либо укажите другой порт API в конфигурации."
			}
		}
		showStartupError(message)
		return err
	}
	defer shell.runtime.Close()
	done := make(chan struct{})
	var runtimeErr error
	go func() { runtimeErr = shell.runtime.Run(ctx); close(done); shell.app.Quit() }()
	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	err = waitForAPI(readyCtx, shell.runtime.APIAddress, done)
	readyCancel()
	if err != nil {
		cancel()
		<-done
		showStartupError("memplua could not start its local service. Check the application log.")
		return errors.Join(err, runtimeErr)
	}
	// Only the widget is needed at startup. The remaining windows are created
	// on demand, avoiding several simultaneous WebView/API bootstraps.
	shell.createWindow("widget")
	shell.createTray()
	shell.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		shell.restoreWindows()
		_ = shell.OpenWindow("widget", "")
		if enabled, readErr := shell.app.Autostart.IsEnabled(); readErr == nil && enabled != options.Config.UI.StartWithWindows {
			if err := shell.SetAutostart(options.Config.UI.StartWithWindows); err != nil {
				shell.runtime.Logger.Warn("apply configured autostart", "error", err)
			}
		}
	})
	shell.app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		shell.app.Event.Emit("desktop:theme", shell.systemTheme())
	})
	shell.app.OnShutdown(func() {
		shell.mu.Lock()
		shell.closing = true
		if shell.saveTimer != nil {
			shell.saveTimer.Stop()
		}
		state := shell.state
		shell.mu.Unlock()
		if err := saveState(shell.statePath, state); err != nil {
			shell.runtime.Logger.Warn("save desktop state", "error", err)
		}
		cancel()
		<-done
		_ = shell.runtime.Close()
	})
	go shell.monitorDisplays(ctx)
	err = shell.app.Run()
	cancel()
	<-done
	return errors.Join(err, runtimeErr)
}

// Bootstrap returns local API connection data and initial desktop preferences.
func (s *Shell) Bootstrap() BootstrapData {
	enabled, err := s.app.Autostart.IsEnabled()
	if err != nil {
		s.runtime.Logger.Warn("read autostart", "error", err)
	}
	return BootstrapData{APIAddress: s.runtime.APIAddress, Token: s.runtime.APIToken, Locale: s.runtime.Settings.Current().UI.Language, SystemTheme: s.systemTheme(), Autostart: enabled}
}

func (s *Shell) systemTheme() string {
	if s.app.Env.IsDarkMode() {
		return "dark"
	}
	return "light"
}

// OpenWindow lazily creates and focuses a known logical window. Source windows
// require the conspect whose focus text they display.
func (s *Shell) OpenWindow(name, conspectID string) error {
	s.mu.Lock()
	window := s.windows[name]
	s.mu.Unlock()
	if window == nil {
		if !knownWindow(name) {
			return errors.New("unknown desktop window")
		}
		s.createWindow(name)
		s.mu.Lock()
		window = s.windows[name]
		s.mu.Unlock()
		if window == nil {
			return errors.New("create desktop window")
		}
		s.restoreWindow(name, window)
	}
	if name == "source" {
		if conspectID == "" || len(conspectID) > 100 {
			return errors.New("a conspect is required")
		}
		window.SetURL("/?window=source&conspect=" + url.QueryEscape(conspectID))
	}
	window.UnMinimise()
	window.Show()
	window.Focus()
	return nil
}

// HideWindow hides a previously created logical window without quitting.
func (s *Shell) HideWindow(name string) error {
	s.mu.Lock()
	window := s.windows[name]
	s.mu.Unlock()
	if window == nil {
		return errors.New("unknown desktop window")
	}
	window.Hide()
	return nil
}

// Quit requests complete application shutdown.
func (s *Shell) Quit() { s.app.Quit() }

// ResizeWidget validates and applies one of the compact widget widths.
func (s *Shell) ResizeWidget(width, height int) error {
	if width < widgetClosedWidth || width > widgetMaxWidth || height != widgetHeight {
		return errors.New("invalid widget size")
	}
	s.windows["widget"].SetSize(width, height)
	return nil
}

// SetAlwaysOnTop updates the native widget window immediately.
func (s *Shell) SetAlwaysOnTop(enabled bool) { s.windows["widget"].SetAlwaysOnTop(enabled) }

// SetAutostart updates the current user's Windows startup registration.
func (s *Shell) SetAutostart(enabled bool) error {
	if enabled {
		return s.app.Autostart.EnableWithOptions(application.AutostartOptions{Arguments: []string{"--config", s.options.ConfigPath, "--data-dir", s.options.Config.DataDir, "--listen", s.options.Config.API.Listen}})
	}
	return s.app.Autostart.Disable()
}

// PickFile opens a settings-attached native single-file picker.
func (s *Shell) PickFile(title string) (string, error) {
	return s.app.Dialog.OpenFile().SetTitle(title).CanChooseFiles(true).CanChooseDirectories(false).AttachToWindow(s.windows["settings"]).PromptForSingleSelection()
}

// PickFolder opens a settings-attached native directory picker.
func (s *Shell) PickFolder(title string) (string, error) {
	return s.app.Dialog.OpenFile().SetTitle(title).CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(true).AttachToWindow(s.windows["settings"]).PromptForSingleSelection()
}

func (s *Shell) createWindow(name string) {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	s.mu.Lock()
	if s.windows[name] != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	options := application.WebviewWindowOptions{Name: name, Title: "memplua", URL: "/?window=" + name, Hidden: true, Width: 800, Height: 640, Zoom: 1, ZoomControlEnabled: false,
		BackgroundColour: application.NewRGB(247, 248, 252), Windows: application.WindowsWindow{Theme: application.SystemDefault}}
	switch name {
	case "widget":
		options.Width, options.Height, options.Frameless, options.DisableResize = widgetClosedWidth, widgetHeight, true, true
		options.AlwaysOnTop = s.options.Config.UI.AlwaysOnTop
		options.Windows.HiddenOnTaskbar = true
		options.Windows.DisableFramelessWindowDecorations = true
		options.Windows.DisableIcon = true
		options.Windows.DisableMenu = true
		options.MinimiseButtonState = application.ButtonHidden
		options.MaximiseButtonState = application.ButtonHidden
		options.CloseButtonState = application.ButtonHidden
		options.BackgroundType = application.BackgroundTypeTransparent
		options.BackgroundColour = application.NewRGBA(0, 0, 0, 0)
	case "review":
		options.Title, options.Width, options.Height, options.MinWidth, options.MinHeight = "memplua · Review", 1180, 760, 960, 640
	case "settings":
		options.Title, options.Width, options.Height, options.MinWidth, options.MinHeight = "memplua · Settings", 760, 700, 640, 580
	case "source":
		options.Title, options.MinWidth, options.MinHeight = "memplua · Source text", 600, 420
	case "graph":
		options.Title, options.Width, options.Height, options.MinWidth, options.MinHeight = "memplua · Knowledge graph", 1100, 720, 760, 520
	}
	window := s.app.Window.NewWithOptions(options)
	s.mu.Lock()
	s.windows[name] = window
	s.mu.Unlock()
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) { event.Cancel(); window.Hide() })
	window.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) { s.captureWindow(name) })
	window.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { s.captureWindow(name) })
	window.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		if name == "widget" {
			window.EmitEvent("desktop:blur", nil)
		}
	})
}

func (s *Shell) captureWindow(name string) {
	s.mu.Lock()
	window := s.windows[name]
	s.mu.Unlock()
	if window == nil {
		return
	}
	if window.IsMinimised() || window.IsMaximised() {
		return
	}
	position := window.Bounds()
	bounds := windowBounds{X: position.X, Y: position.Y, Width: position.Width, Height: position.Height}
	if name == "widget" {
		bounds.Width, bounds.Height = widgetClosedWidth, widgetHeight
	}
	if screen, err := window.GetScreen(); err == nil && screen != nil {
		bounds.Monitor = screen.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return
	}
	s.state.Windows[name] = bounds
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closing {
			if err := saveState(s.statePath, s.state); err != nil {
				s.runtime.Logger.Warn("save desktop state", "error", err)
			}
		}
	})
}

func (s *Shell) restoreWindows() {
	s.mu.Lock()
	windows := make(map[string]*application.WebviewWindow, len(s.windows))
	for name, window := range s.windows {
		windows[name] = window
	}
	s.mu.Unlock()
	for name, window := range windows {
		s.restoreWindow(name, window)
	}
}

func (s *Shell) restoreWindow(name string, window *application.WebviewWindow) {
	primary := s.app.Screen.GetPrimary()
	if primary == nil {
		return
	}
	s.mu.Lock()
	saved, exists := s.state.Windows[name]
	s.mu.Unlock()
	screen := primary
	if exists {
		if found := s.app.Screen.GetByID(saved.Monitor); found != nil {
			screen = found
		}
	}
	work := windowBounds{X: screen.WorkArea.X, Y: screen.WorkArea.Y, Width: screen.WorkArea.Width, Height: screen.WorkArea.Height}
	if !exists {
		current := window.Bounds()
		saved = windowBounds{Width: current.Width, Height: current.Height}
		if saved.Width <= 0 || saved.Height <= 0 {
			return
		}
		saved.X, saved.Y = work.X+(work.Width-saved.Width)/2, work.Y+(work.Height-saved.Height)/2
		if name == "widget" {
			saved.X, saved.Y, saved.Width, saved.Height = work.X+(work.Width-widgetClosedWidth)/2, work.Y+16, widgetClosedWidth, widgetHeight
		}
	}
	if name == "widget" {
		saved.Width, saved.Height = widgetClosedWidth, widgetHeight
	}
	saved = clampBounds(saved, work)
	window.SetBounds(application.Rect{X: saved.X, Y: saved.Y, Width: saved.Width, Height: saved.Height})
}

func knownWindow(name string) bool {
	switch name {
	case "widget", "review", "settings", "source", "graph":
		return true
	default:
		return false
	}
}

func waitForAPI(ctx context.Context, address string, runtimeDone <-chan struct{}) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/healthz", nil)
		if err != nil {
			return err
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for local API: %w", ctx.Err())
		case <-runtimeDone:
			return errors.New("local service stopped during startup")
		case <-ticker.C:
		}
	}
}

func (s *Shell) monitorDisplays(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			language := s.runtime.Settings.Current().UI.Language
			if language != s.trayLanguage {
				s.refreshTrayMenu()
			}
			var signature strings.Builder
			for _, screen := range s.app.Screen.GetAll() {
				fmt.Fprintf(&signature, "%s:%v:%f;", screen.ID, screen.WorkArea, screen.ScaleFactor)
			}
			current := signature.String()
			if s.monitorSignature != "" && current != s.monitorSignature {
				s.restoreWindows()
			}
			s.monitorSignature = current
		}
	}
}

func (s *Shell) createTray() {
	tray := s.app.SystemTray.New()
	tray.SetIcon(trayIcon())
	tray.SetLabel("memplua")
	tray.OnClick(func() { _ = s.OpenWindow("widget", "") })
	s.tray = tray
	s.refreshTrayMenu()
}

func (s *Shell) refreshTrayMenu() {
	menu := s.app.Menu.New()
	s.trayLanguage = s.runtime.Settings.Current().UI.Language
	ru := s.trayLanguage == "ru"
	label := func(en, russian string) string {
		if ru {
			return russian
		}
		return en
	}
	for _, entry := range []struct{ name, label string }{
		{"widget", label("Show Widget", "Показать виджет")}, {"review", label("Review", "Проверка")},
		{"settings", label("Settings", "Настройки")}, {"graph", label("Knowledge Graph", "Граф знаний")},
	} {
		menu.Add(entry.label).OnClick(func(*application.Context) { _ = s.OpenWindow(entry.name, "") })
	}
	menu.AddSeparator()
	for _, source := range []string{"microphone", "loopback"} {
		sourceLabel := label("Microphone", "Микрофон")
		if source == "loopback" {
			sourceLabel = label("System audio", "Звук системы")
		}
		submenu := menu.AddSubmenu(sourceLabel)
		for _, action := range []struct{ name, label string }{{"start", label("Start", "Начать")}, {"pause", label("Pause", "Пауза")}, {"resume", label("Resume", "Продолжить")}, {"stop", label("Stop", "Остановить")}} {
			submenu.Add(action.label).OnClick(func(*application.Context) { go s.sourceAction(source, action.name) })
		}
	}
	menu.AddSeparator()
	menu.Add(label("Quit", "Завершить работу")).OnClick(func(*application.Context) { s.app.Quit() })
	s.tray.SetMenu(menu)
}

func (s *Shell) sourceAction(source, action string) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	path := "/api/v1/sources/" + url.PathEscape(source) + "/start"
	{
		response, err := s.request(ctx, http.MethodGet, "/api/v1/ui/state")
		if err != nil {
			s.actionFailed(err)
			return
		}
		var status struct {
			Sources []struct {
				ID        string   `json:"id"`
				SessionID string   `json:"session_id"`
				Actions   []string `json:"actions"`
			} `json:"sources"`
		}
		err = json.NewDecoder(response.Body).Decode(&status)
		response.Body.Close()
		if err != nil {
			s.actionFailed(err)
			return
		}
		sessionID := ""
		allowed := false
		for _, state := range status.Sources {
			if state.ID == source {
				sessionID = state.SessionID
				for _, available := range state.Actions {
					if available == action {
						allowed = true
					}
				}
			}
		}
		if !allowed || (action != "start" && sessionID == "") {
			return
		}
		if action != "start" {
			path = "/api/v1/source-sessions/" + url.PathEscape(sessionID) + "/" + action
		}
	}
	response, err := s.request(ctx, http.MethodPost, path)
	if err != nil {
		s.actionFailed(err)
		return
	}
	response.Body.Close()
}

func (s *Shell) request(ctx context.Context, method, path string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, s.runtime.APIAddress+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+s.runtime.APIToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
		return nil, fmt.Errorf("source action returned HTTP %d", response.StatusCode)
	}
	return response, nil
}

func (s *Shell) actionFailed(err error) {
	s.runtime.Logger.Warn("tray source action failed", "error", err)
	message := "The source could not be changed. Open the widget or Settings for its current status."
	if s.runtime.Settings.Current().UI.Language == "ru" {
		message = "Не удалось изменить состояние источника. Откройте виджет или настройки, чтобы проверить его состояние."
	}
	s.app.Dialog.Warning().SetTitle("memplua").SetMessage(message).Show()
}

func showStartupError(message string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("memplua")
	_, _, _ = user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
