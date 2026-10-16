package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// App wires the store, the outgoing HTTP client and the request log together.
type App struct {
	quit       chan struct{} // closed by Shutdown, e.g. from the admin API
	quitOnce   sync.Once
	store      *Store
	logs       *LogBuffer
	client     *http.Client
	testClient *http.Client
	listen     string
	limits     *limiter
	metrics    *metrics

	mu     sync.Mutex
	actual string
}

func NewApp(configPath, listenOverride string) (*App, error) {
	store, err := NewStore(configPath)
	if err != nil {
		return nil, err
	}
	addr := store.Settings().Listen
	if listenOverride != "" {
		addr = listenOverride
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
		ExpectContinueTimeout: 2 * time.Second,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
	}
	return &App{
		store:   store,
		logs:    &LogBuffer{max: 500},
		quit:    make(chan struct{}),
		limits:  newLimiter(),
		metrics: newMetrics(),
		listen:  addr,
		client: &http.Client{
			Transport: transport,
			// Return upstream redirects to the caller instead of following them.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		testClient: &http.Client{
			Timeout:       25 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Run serves until stop is closed, then shuts down gracefully.
func (a *App) Run(stop <-chan struct{}) error {
	// Stop on a signal or on Shutdown, whichever comes first.
	merged := make(chan struct{})
	go func() {
		select {
		case <-stop:
		case <-a.quit:
		}
		close(merged)
	}()
	stop = merged

	mux := http.NewServeMux()
	a.routes(mux)

	ln, err := net.Listen("tcp", a.listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.listen, err)
	}
	a.setActual(ln.Addr().String())
	if exposedWithoutAuth(ln.Addr().String(), a.store.Settings()) {
		log.Printf("WARNING: listening on %s with no proxy or admin token. Anyone who can reach this port can use your API keys. Set a token in Settings first.", ln.Addr())
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}
	log.Printf("keyrotator %s - UI http://%s/  (config: %s)", version, a.actual, a.store.Path())

	go a.sweeper(stop)
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := a.store.Save(); err != nil {
					log.Printf("save config: %v", err)
				}
			}
		}
	}()

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Persist anything changed since the last periodic save.
		if err := a.store.Save(); err != nil {
			log.Printf("save config on shutdown: %v", err)
		}
		return nil
	}
}

// exposedWithoutAuth reports whether the server is reachable from other
// machines while neither a proxy token nor an admin token protects it.
func exposedWithoutAuth(addr string, st Settings) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return st.ProxyToken == "" && st.AdminToken == ""
}

func (a *App) setActual(addr string) {
	a.mu.Lock()
	a.actual = addr
	a.mu.Unlock()
}

// Addr is the address the server actually bound to.
func (a *App) Addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.actual == "" {
		return a.listen
	}
	return a.actual
}

// URL is the browser-facing base URL of the admin UI.
func (a *App) URL() string {
	addr := a.Addr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/", host, port)
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		if exe, e := os.Executable(); e == nil {
			return filepath.Join(filepath.Dir(exe), "keyrotator-config.json")
		}
		return "keyrotator-config.json"
	}
	return filepath.Join(dir, "KeyRotator", "config.json")
}

// LogEntry is one proxied request.
type LogEntry struct {
	Time     int64  `json:"time"`
	Provider string `json:"provider"`
	Key      string `json:"key"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Code     int    `json:"code"`
	OK       bool   `json:"ok"`
	Ms       int64  `json:"ms"`
	Msg      string `json:"msg"`
}

// LogBuffer is a bounded in-memory ring of recent requests.
type LogBuffer struct {
	mu    sync.Mutex
	items []LogEntry
	max   int
}

func (l *LogBuffer) Add(e LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, e)
	if len(l.items) > l.max {
		l.items = l.items[len(l.items)-l.max:]
	}
}

// List returns newest first.
func (l *LogBuffer) List() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LogEntry, 0, len(l.items))
	for i := len(l.items) - 1; i >= 0; i-- {
		out = append(out, l.items[i])
	}
	return out
}

// openBrowser opens url in the default browser. It is best effort: if no
// opener is available the URL is still printed in the startup banner.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// sweeper re-tests cooled-down and invalid keys in the background.
func (a *App) sweeper(stop <-chan struct{}) {
	t := time.NewTicker(90 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			now := time.Now().Unix()
			for _, p := range a.store.Providers() {
				if !p.Enabled {
					continue
				}
				p.mu.Lock()
				var candidates []*Key
				for _, k := range p.Keys {
					if !k.Enabled {
						continue
					}
					if k.Status == statusCooldown && k.CooldownUntil <= now {
						candidates = append(candidates, k)
					}
					if k.Status == statusInvalid {
						candidates = append(candidates, k)
					}
				}
				p.mu.Unlock()
				for _, k := range candidates {
					a.testAndRecord(p, k)
				}
			}
		}
	}
}

// probe makes a lightweight authenticated request to the provider health path.
func (a *App) probe(p *Provider, k *Key) (int, string) {
	base := strings.TrimRight(p.BaseURL, "/")
	path := p.HealthPath
	if strings.TrimSpace(path) == "" {
		path = "/models"
	}
	url := base + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Accept", "application/json")
	applyInjection(p, k, req)
	resp, err := a.testClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	return resp.StatusCode, resp.Status
}

// testAndRecord probes a key against the provider health path and updates its
// stored status. It returns the status code and a human message.
func (a *App) testAndRecord(p *Provider, k *Key) (int, string) {
	code, msg := a.probe(p, k)
	if code >= 200 && code < 400 {
		p.mu.Lock()
		k.Status = statusOK
		k.CooldownUntil = 0
		k.LastError = ""
		p.mu.Unlock()
		return code, msg
	}
	if code == 401 || code == 403 {
		p.mu.Lock()
		k.Status = statusInvalid
		k.LastError = fmt.Sprintf("%d %s", code, msg)
		p.mu.Unlock()
		return code, msg
	}
	if code == 429 || code >= 500 || code == 0 {
		p.mu.Lock()
		k.Status = statusCooldown
		if k.CooldownUntil <= time.Now().Unix() {
			k.CooldownUntil = time.Now().Unix() + 60
		}
		k.LastError = fmt.Sprintf("%d %s", code, msg)
		p.mu.Unlock()
	}
	return code, msg
}

// Shutdown asks a running server to stop gracefully. It is safe to call more
// than once, and from any goroutine, including the admin API.
func (a *App) Shutdown() {
	a.quitOnce.Do(func() { close(a.quit) })
}
