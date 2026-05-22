package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type StartInput struct {
	Headless        bool   `json:"headless" jsonschema:"launch Chrome headless instead of in a visible window"`
	ChromePath      string `json:"chrome_path,omitempty" jsonschema:"optional path to the Chrome or Chromium executable"`
	UserDataDir     string `json:"user_data_dir,omitempty" jsonschema:"optional user data directory for the browser profile"`
	RemoteDebugPort int    `json:"remote_debug_port,omitempty" jsonschema:"port to expose for Chrome remote debugging"`
	AttachURL       string `json:"attach_url,omitempty" jsonschema:"websocket or HTTP debugging URL for an already running Chrome"`
	WindowWidth     int    `json:"window_width,omitempty" jsonschema:"browser viewport width"`
	WindowHeight    int    `json:"window_height,omitempty" jsonschema:"browser viewport height"`
}

type StartOutput struct {
	Started  bool   `json:"started" jsonschema:"whether the browser started successfully"`
	DebugURL string `json:"debug_url" jsonschema:"Chrome DevTools debugging URL"`
	Message  string `json:"message" jsonschema:"human-readable status message"`
}

type NavigateInput struct {
	URL string `json:"url" jsonschema:"absolute URL to open"`
}

type NavigateOutput struct {
	URL     string `json:"url" jsonschema:"opened URL"`
	Title   string `json:"title" jsonschema:"page title after navigation"`
	Message string `json:"message" jsonschema:"status message"`
}

type ClickInput struct {
	Selector string `json:"selector" jsonschema:"CSS selector to click"`
}

type TypeInput struct {
	Selector string `json:"selector" jsonschema:"CSS selector for the target element"`
	Text     string `json:"text" jsonschema:"text to type into the element"`
	Clear    bool   `json:"clear" jsonschema:"clear the field before typing"`
}

type WaitInput struct {
	Selector string `json:"selector" jsonschema:"CSS selector to wait for"`
	Timeout  int    `json:"timeout_ms,omitempty" jsonschema:"timeout in milliseconds"`
}

type EvalInput struct {
	Expression string `json:"expression" jsonschema:"JavaScript expression to evaluate in the page context"`
}

type ScreenshotInput struct {
	FullPage bool `json:"full_page" jsonschema:"capture the full page instead of only the viewport"`
}

type BrowserOutput struct {
	Message string `json:"message" jsonschema:"status message"`
}

type EvalOutput struct {
	Value   string `json:"value" jsonschema:"JSON-encoded evaluation result"`
	Message string `json:"message" jsonschema:"status message"`
}

type ScreenshotOutput struct {
	Base64PNG string `json:"base64_png" jsonschema:"PNG image encoded as base64"`
	Message   string `json:"message" jsonschema:"status message"`
}

type SearchInput struct {
	Query string `json:"query" jsonschema:"search terms to look up"`
}

type OpenInput struct {
	URL string `json:"url" jsonschema:"absolute URL to open"`
}

type SnapshotInput struct {
	MaxChars int `json:"max_chars,omitempty" jsonschema:"maximum number of body text characters to return"`
}

type SnapshotOutput struct {
	URL     string `json:"url" jsonschema:"current page URL"`
	Title   string `json:"title" jsonschema:"current page title"`
	Text    string `json:"text" jsonschema:"visible page text"`
	Message string `json:"message" jsonschema:"status message"`
}

type OpenOutput struct {
	URL     string `json:"url" jsonschema:"current page URL"`
	Title   string `json:"title" jsonschema:"current page title"`
	Text    string `json:"text" jsonschema:"visible page text"`
	Message string `json:"message" jsonschema:"status message"`
}

type BrowserSession struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	started     bool
	headless    bool
	chromePath  string
	userDataDir string
	attachURL   string
	debugPort   int
	width       int
	height      int
}

func newBrowserSession() *BrowserSession {
	return &BrowserSession{
		headless:  false,
		debugPort: 9222,
		width:     1440,
		height:    900,
	}
}

func (s *BrowserSession) stopLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx = nil
	s.cancel = nil
	s.started = false
}

func (s *BrowserSession) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *BrowserSession) startLocked(parent context.Context, input StartInput) (string, error) {
	s.stopLocked()

	if input.WindowWidth > 0 {
		s.width = input.WindowWidth
	}
	if input.WindowHeight > 0 {
		s.height = input.WindowHeight
	}
	if input.RemoteDebugPort > 0 {
		s.debugPort = input.RemoteDebugPort
	}
	if input.ChromePath != "" {
		s.chromePath = input.ChromePath
	}
	if input.UserDataDir != "" {
		s.userDataDir = input.UserDataDir
	}
	if input.AttachURL != "" {
		s.attachURL = input.AttachURL
	}
	s.headless = input.Headless

	if s.attachURL != "" {
		return s.attachToDebugURL(parent, s.attachURL)
	}

	if s.userDataDir == "" {
		s.userDataDir = filepath.Join(os.Getenv("HOME"), ".codex", "chrome-automation-profile")
	}

	debugURL := fmt.Sprintf("http://127.0.0.1:%d", s.debugPort)
	if s.debugEndpointReady(parent, debugURL) {
		return s.attachToDebugURL(parent, debugURL)
	}

	if err := s.launchVisibleChrome(parent); err != nil {
		return "", err
	}
	if err := activateChrome(parent); err != nil {
		return "", err
	}

	return s.attachToDebugURL(parent, debugURL)
}

func (s *BrowserSession) attachToDebugURL(parent context.Context, debugURL string) (string, error) {
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(parent, debugURL)
	ctx, cancel := chromedp.NewContext(allocCtx)
	var readyState string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.readyState`, &readyState)); err != nil {
		cancel()
		allocCancel()
		return "", err
	}

	s.ctx = ctx
	s.cancel = func() {
		cancel()
		allocCancel()
	}
	s.started = true

	return debugURL, nil
}

func chromeBinaryPath() string {
	return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
}

func (s *BrowserSession) launchVisibleChrome(parent context.Context) error {
	if s.chromePath == "" {
		s.chromePath = chromeBinaryPath()
	}

	if _, err := os.Stat(s.chromePath); err != nil {
		return fmt.Errorf("chrome binary not found at %s: %w", s.chromePath, err)
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", s.debugPort),
		fmt.Sprintf("--user-data-dir=%s", s.userDataDir),
		fmt.Sprintf("--window-size=%d,%d", s.width, s.height),
		"--new-window",
		"about:blank",
	}
	cmd := exec.CommandContext(parent, s.chromePath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch chrome: %w", err)
	}
	go func() {
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(30 * time.Second)
	probeURL := fmt.Sprintf("http://127.0.0.1:%d/json/version", s.debugPort)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(parent, http.MethodGet, probeURL, nil)
		if err == nil {
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				_ = resp.Body.Close()
				return nil
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("chrome debug endpoint did not become ready at %s", probeURL)
}

func (s *BrowserSession) debugEndpointReady(parent context.Context, debugURL string) bool {
	probeURL := debugURL + "/json/version"
	req, err := http.NewRequestWithContext(parent, http.MethodGet, probeURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func activateChrome(parent context.Context) error {
	cmd := exec.CommandContext(parent, "osascript", "-e", `tell application "Google Chrome" to activate`)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("activate chrome: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *BrowserSession) ensureStarted(parent context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Sprintf("http://127.0.0.1:%d", s.debugPort), nil
	}
	return s.startLocked(parent, StartInput{})
}

func shouldRestartBrowser(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no browser is open") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "browser has been closed") ||
		strings.Contains(msg, "target closed") ||
		strings.Contains(msg, "session not found")
}

func (s *BrowserSession) run(parent context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	_, err := s.ensureStarted(parent)
	if err != nil {
		return fmt.Errorf("start browser: %w", err)
	}

	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("browser context is not available")
	}

	actionCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		actionCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if err := chromedp.Run(actionCtx, actions...); err != nil {
		if shouldRestartBrowser(err) {
			s.stop()
			if _, startErr := s.ensureStarted(parent); startErr == nil {
				s.mu.Lock()
				ctx = s.ctx
				s.mu.Unlock()
				if ctx != nil {
					actionCtx = ctx
					if timeout > 0 {
						var cancel context.CancelFunc
						actionCtx, cancel = context.WithTimeout(ctx, timeout)
						defer cancel()
					}
					return chromedp.Run(actionCtx, actions...)
				}
			}
		}
		return err
	}

	return nil
}

func (s *BrowserSession) title(parent context.Context) (string, error) {
	var title string
	err := s.run(parent, 20*time.Second, chromedp.Evaluate(`document.title`, &title))
	return title, err
}

func (s *BrowserSession) currentURL(parent context.Context) (string, error) {
	var pageURL string
	err := s.run(parent, 20*time.Second, chromedp.Evaluate(`window.location.href`, &pageURL))
	return pageURL, err
}

func (s *BrowserSession) pageText(parent context.Context, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 8000
	}
	var text string
	script := fmt.Sprintf(`(() => {
		const text = document.body ? document.body.innerText || "" : "";
		return text.slice(0, %d);
	})()`, maxChars)
	err := s.run(parent, 20*time.Second, chromedp.Evaluate(script, &text))
	return text, err
}

var browser = newBrowserSession()

func startBrowser(ctx context.Context, _ *mcp.CallToolRequest, input StartInput) (*mcp.CallToolResult, StartOutput, error) {
	browser.mu.Lock()
	defer browser.mu.Unlock()

	debugURL, err := browser.startLocked(ctx, input)
	if err != nil {
		return nil, StartOutput{Started: false, Message: err.Error()}, err
	}

	return nil, StartOutput{
		Started:  true,
		DebugURL: debugURL,
		Message:  "browser started",
	}, nil
}

func navigate(ctx context.Context, _ *mcp.CallToolRequest, input NavigateInput) (*mcp.CallToolResult, NavigateOutput, error) {
	if err := browser.run(ctx, 30*time.Second, chromedp.Navigate(input.URL)); err != nil {
		return nil, NavigateOutput{URL: input.URL, Message: err.Error()}, err
	}
	if err := activateChrome(ctx); err != nil {
		return nil, NavigateOutput{URL: input.URL, Message: err.Error()}, err
	}

	title, err := browser.title(ctx)
	if err != nil {
		return nil, NavigateOutput{URL: input.URL, Message: err.Error()}, err
	}

	return nil, NavigateOutput{URL: input.URL, Title: title, Message: "navigation complete"}, nil
}

func click(ctx context.Context, _ *mcp.CallToolRequest, input ClickInput) (*mcp.CallToolResult, BrowserOutput, error) {
	err := browser.run(ctx, 20*time.Second, chromedp.Click(input.Selector, chromedp.ByQuery))
	if err != nil {
		return nil, BrowserOutput{Message: err.Error()}, err
	}
	return nil, BrowserOutput{Message: "click complete"}, nil
}

func typeText(ctx context.Context, _ *mcp.CallToolRequest, input TypeInput) (*mcp.CallToolResult, BrowserOutput, error) {
	actions := []chromedp.Action{}
	if input.Clear {
		actions = append(actions, chromedp.SetValue(input.Selector, "", chromedp.ByQuery))
	}
	actions = append(actions, chromedp.SendKeys(input.Selector, input.Text, chromedp.ByQuery))

	if err := browser.run(ctx, 20*time.Second, actions...); err != nil {
		return nil, BrowserOutput{Message: err.Error()}, err
	}
	return nil, BrowserOutput{Message: "text input complete"}, nil
}

func waitFor(ctx context.Context, _ *mcp.CallToolRequest, input WaitInput) (*mcp.CallToolResult, BrowserOutput, error) {
	timeout := time.Duration(input.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	err := browser.run(ctx, timeout, chromedp.WaitVisible(input.Selector, chromedp.ByQuery))
	if err != nil {
		return nil, BrowserOutput{Message: err.Error()}, err
	}
	return nil, BrowserOutput{Message: "selector became visible"}, nil
}

func evaluate(ctx context.Context, _ *mcp.CallToolRequest, input EvalInput) (*mcp.CallToolResult, EvalOutput, error) {
	var raw any
	err := browser.run(ctx, 20*time.Second, chromedp.Evaluate(input.Expression, &raw))
	if err != nil {
		return nil, EvalOutput{Message: err.Error()}, err
	}

	value, marshalErr := json.MarshalIndent(raw, "", "  ")
	if marshalErr != nil {
		value = []byte(fmt.Sprintf("%v", raw))
	}

	return nil, EvalOutput{Value: string(value), Message: "evaluation complete"}, nil
}

func screenshot(ctx context.Context, _ *mcp.CallToolRequest, input ScreenshotInput) (*mcp.CallToolResult, ScreenshotOutput, error) {
	var buf []byte
	var err error
	if input.FullPage {
		err = browser.run(ctx, 30*time.Second, chromedp.FullScreenshot(&buf, 90))
	} else {
		err = browser.run(ctx, 30*time.Second, chromedp.CaptureScreenshot(&buf))
	}
	if err != nil {
		return nil, ScreenshotOutput{Message: err.Error()}, err
	}

	return nil, ScreenshotOutput{Base64PNG: base64.StdEncoding.EncodeToString(buf), Message: "screenshot captured"}, nil
}

func searchBrowser(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, NavigateOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, NavigateOutput{Message: "query cannot be empty"}, fmt.Errorf("query cannot be empty")
	}

	target := "https://www.google.com/search?q=" + url.QueryEscape(query)
	if err := browser.run(ctx, 30*time.Second, chromedp.Navigate(target)); err != nil {
		return nil, NavigateOutput{URL: target, Message: err.Error()}, err
	}
	if err := activateChrome(ctx); err != nil {
		return nil, NavigateOutput{URL: target, Message: err.Error()}, err
	}

	title, err := browser.title(ctx)
	if err != nil {
		return nil, NavigateOutput{URL: target, Message: err.Error()}, err
	}

	return nil, NavigateOutput{URL: target, Title: title, Message: "search opened"}, nil
}

func openURL(ctx context.Context, _ *mcp.CallToolRequest, input OpenInput) (*mcp.CallToolResult, OpenOutput, error) {
	if err := browser.run(ctx, 30*time.Second, chromedp.Navigate(input.URL)); err != nil {
		return nil, OpenOutput{URL: input.URL, Message: err.Error()}, err
	}
	if err := activateChrome(ctx); err != nil {
		return nil, OpenOutput{URL: input.URL, Message: err.Error()}, err
	}

	pageURL, err := browser.currentURL(ctx)
	if err != nil {
		return nil, OpenOutput{URL: input.URL, Message: err.Error()}, err
	}
	title, err := browser.title(ctx)
	if err != nil {
		return nil, OpenOutput{URL: input.URL, Message: err.Error()}, err
	}
	text, err := browser.pageText(ctx, 8000)
	if err != nil {
		return nil, OpenOutput{URL: input.URL, Message: err.Error()}, err
	}

	return nil, OpenOutput{URL: pageURL, Title: title, Text: text, Message: "page opened and snapshot captured"}, nil
}

func snapshot(ctx context.Context, _ *mcp.CallToolRequest, input SnapshotInput) (*mcp.CallToolResult, SnapshotOutput, error) {
	pageURL, err := browser.currentURL(ctx)
	if err != nil {
		return nil, SnapshotOutput{Message: err.Error()}, err
	}
	title, err := browser.title(ctx)
	if err != nil {
		return nil, SnapshotOutput{Message: err.Error()}, err
	}
	text, err := browser.pageText(ctx, input.MaxChars)
	if err != nil {
		return nil, SnapshotOutput{Message: err.Error()}, err
	}

	return nil, SnapshotOutput{URL: pageURL, Title: title, Text: text, Message: "snapshot captured"}, nil
}

func stopBrowser(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, BrowserOutput, error) {
	browser.stop()
	return nil, BrowserOutput{Message: "browser stopped"}, nil
}

func status(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, BrowserOutput, error) {
	browser.mu.Lock()
	defer browser.mu.Unlock()

	if !browser.started {
		return nil, BrowserOutput{Message: "browser is not running"}, nil
	}

	return nil, BrowserOutput{Message: fmt.Sprintf("browser running at http://127.0.0.1:%d", browser.debugPort)}, nil
}

func main() {
	defer browser.stop()

	server := mcp.NewServer(&mcp.Implementation{Name: "chrome-automation-mcp", Version: "v0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "browser_start", Description: "start a Chrome browser in debug mode"}, startBrowser)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_navigate", Description: "navigate the browser to a URL"}, navigate)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_search", Description: "search the web and open the browser to the results page"}, searchBrowser)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_open_url", Description: "open a URL in Chrome, focus the window, and return a snapshot"}, openURL)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_click", Description: "click an element by CSS selector"}, click)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_type", Description: "type text into an element by CSS selector"}, typeText)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_wait_visible", Description: "wait for an element to become visible"}, waitFor)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_eval", Description: "evaluate JavaScript in the page context"}, evaluate)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_screenshot", Description: "capture a screenshot of the current page"}, screenshot)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_snapshot", Description: "return the current URL, title, and visible page text"}, snapshot)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_stop", Description: "stop the managed browser"}, stopBrowser)
	mcp.AddTool(server, &mcp.Tool{Name: "browser_status", Description: "report whether the managed browser is running"}, status)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		panic(err)
	}
}
