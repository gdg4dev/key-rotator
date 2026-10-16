package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

//go:embed web/ui.html
var uiHTML []byte

func (a *App) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", a.handleRoot)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/p/", a.handleProxy)
	mux.HandleFunc("/api/", a.admin(a.handleAPI))
}

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(uiHTML)
}

// admin guards the /api routes when an admin token is configured.
func (a *App) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := a.store.Settings().AdminToken
		if token == "" {
			next(w, r)
			return
		}
		got := r.Header.Get("X-Admin-Token")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if got != token {
			writeErr(w, http.StatusUnauthorized, "invalid or missing admin token")
			return
		}
		next(w, r)
	}
}

type stateResp struct {
	Version         string      `json:"version"`
	Addr            string      `json:"addr"`
	ProxyBase       string      `json:"proxyBase"`
	ConfigPath      string      `json:"configPath"`
	MaxRetries      int         `json:"maxRetries"`
	CooldownSec     int         `json:"cooldownSec"`
	AdminTokenSet   bool        `json:"adminTokenSet"`
	ProxyToken      string      `json:"proxyToken"`
	RateLimitRPS    float64     `json:"rateLimitRPS"`
	KeyRateLimitRPS float64     `json:"keyRateLimitRPS"`
	Providers       []*Provider `json:"providers"`
}

func (a *App) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	seg := split(path)

	switch {
	case len(seg) == 1 && seg[0] == "state" && r.Method == http.MethodGet:
		a.getState(w)
	case len(seg) == 1 && seg[0] == "logs" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"logs": a.logs.List()})
	case len(seg) == 1 && seg[0] == "settings" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		a.postSettings(w, r)
	case len(seg) == 1 && seg[0] == "providers" && r.Method == http.MethodPost:
		a.postProvider(w, r)
	case len(seg) == 2 && seg[0] == "providers" && r.Method == http.MethodPut:
		a.putProvider(w, r, seg[1])
	case len(seg) == 2 && seg[0] == "providers" && r.Method == http.MethodDelete:
		if !a.store.DeleteProvider(seg[1]) {
			writeErr(w, http.StatusNotFound, "provider not found")
			return
		}
		_ = a.store.Save()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case len(seg) == 3 && seg[0] == "providers" && seg[2] == "keys" && r.Method == http.MethodPost:
		a.postKey(w, r, seg[1])
	case len(seg) == 3 && seg[0] == "providers" && seg[2] == "test" && r.Method == http.MethodPost:
		a.testProvider(w, seg[1])
	case len(seg) == 4 && seg[0] == "providers" && seg[2] == "keys" && r.Method == http.MethodPut:
		a.putKey(w, r, seg[1], seg[3])
	case len(seg) == 4 && seg[0] == "providers" && seg[2] == "keys" && r.Method == http.MethodDelete:
		a.deleteKey(w, seg[1], seg[3])
	case len(seg) == 5 && seg[0] == "providers" && seg[2] == "keys" && seg[4] == "test" && r.Method == http.MethodPost:
		a.testKey(w, seg[1], seg[3])
	case len(seg) == 5 && seg[0] == "providers" && seg[2] == "keys" && seg[4] == "reset" && r.Method == http.MethodPost:
		a.resetKey(w, seg[1], seg[3])
	default:
		writeErr(w, http.StatusNotFound, "no such endpoint")
	}
}

func (a *App) getState(w http.ResponseWriter) {
	cfg := a.store.Snapshot()
	sort.SliceStable(cfg.Providers, func(i, j int) bool {
		return cfg.Providers[i].Name < cfg.Providers[j].Name
	})
	writeJSON(w, http.StatusOK, stateResp{
		Version:         version,
		Addr:            a.Addr(),
		ProxyBase:       "http://" + a.Addr() + "/p/",
		ConfigPath:      a.store.Path(),
		MaxRetries:      cfg.MaxRetries,
		CooldownSec:     cfg.CooldownSec,
		AdminTokenSet:   cfg.AdminToken != "",
		ProxyToken:      cfg.ProxyToken,
		RateLimitRPS:    cfg.RateLimitRPS,
		KeyRateLimitRPS: cfg.KeyRateLimitRPS,
		Providers:       cfg.Providers,
	})
}

type settingsReq struct {
	AdminToken      *string  `json:"adminToken"`
	MaxRetries      *int     `json:"maxRetries"`
	CooldownSec     *int     `json:"cooldownSec"`
	ProxyToken      *string  `json:"proxyToken"`
	RateLimitRPS    *float64 `json:"rateLimitRPS"`
	KeyRateLimitRPS *float64 `json:"keyRateLimitRPS"`
}

func (a *App) postSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s := a.store.Settings()
	if req.AdminToken != nil {
		s.AdminToken = strings.TrimSpace(*req.AdminToken)
	}
	if req.MaxRetries != nil {
		s.MaxRetries = *req.MaxRetries
	}
	if req.CooldownSec != nil {
		s.CooldownSec = *req.CooldownSec
	}
	if req.ProxyToken != nil {
		s.ProxyToken = strings.TrimSpace(*req.ProxyToken)
	}
	if req.RateLimitRPS != nil {
		s.RateLimitRPS = *req.RateLimitRPS
	}
	if req.KeyRateLimitRPS != nil {
		s.KeyRateLimitRPS = *req.KeyRateLimitRPS
	}
	a.store.UpdateSettings(s)
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type providerReq struct {
	Name       string        `json:"name"`
	BaseURL    string        `json:"baseUrl"`
	Inject     *[]InjectRule `json:"inject"`
	HealthPath string        `json:"healthPath"`
	Strategy   string        `json:"strategy"`
	Enabled    *bool         `json:"enabled"`

	// Proxy auth + rate limits. Empty/nil keeps the current value; a pointer to
	// 0 explicitly disables that limit for the provider.
	ProxyToken      *string  `json:"proxyToken"`
	RateLimitRPS    *float64 `json:"rateLimitRPS"`
	KeyRateLimitRPS *float64 `json:"keyRateLimitRPS"`

	// Legacy: a single header, converted into an Inject rule.
	AuthHeader *string `json:"authHeader"`
	AuthPrefix *string `json:"authPrefix"`
}

func (req providerReq) apply(p *Provider) {
	if strings.TrimSpace(req.Name) != "" {
		p.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.BaseURL) != "" {
		p.BaseURL = strings.TrimSpace(req.BaseURL)
	}
	switch {
	case req.Inject != nil:
		p.Inject = normalizeRules(*req.Inject)
	case req.AuthHeader != nil || req.AuthPrefix != nil:
		name, prefix := "Authorization", "Bearer "
		if req.AuthHeader != nil {
			name = strings.TrimSpace(*req.AuthHeader)
		}
		if req.AuthPrefix != nil {
			prefix = *req.AuthPrefix
		}
		p.Inject = []InjectRule{{Enabled: true, In: "header", Name: name, Prefix: prefix}}
	}
	if strings.TrimSpace(req.HealthPath) != "" {
		p.HealthPath = strings.TrimSpace(req.HealthPath)
	}
	if req.Strategy != "" {
		p.Strategy = req.Strategy
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.ProxyToken != nil {
		p.ProxyToken = strings.TrimSpace(*req.ProxyToken)
	}
	if req.RateLimitRPS != nil {
		p.RateLimitRPS = clampRate(req.RateLimitRPS)
	}
	if req.KeyRateLimitRPS != nil {
		p.KeyRateLimitRPS = clampRate(req.KeyRateLimitRPS)
	}
}

func (a *App) postProvider(w http.ResponseWriter, r *http.Request) {
	var req providerReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		writeErr(w, http.StatusBadRequest, "baseUrl is required")
		return
	}
	p := &Provider{
		Enabled: true,
		Inject:  []InjectRule{{Enabled: true, In: "header", Name: "Authorization", Prefix: "Bearer "}},
	}
	req.apply(p)
	a.store.AddProvider(p)
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": p})
}

func (a *App) putProvider(w http.ResponseWriter, r *http.Request, id string) {
	var req providerReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.store.Mutate(id, func(p *Provider) error {
		req.apply(p)
		return nil
	}) {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type keyReq struct {
	Label   string `json:"label"`
	Value   string `json:"value"`
	Enabled *bool  `json:"enabled"`
}

func (a *App) postKey(w http.ResponseWriter, r *http.Request, pid string) {
	var req keyReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Value) == "" {
		writeErr(w, http.StatusBadRequest, "value is required")
		return
	}
	p := a.store.Provider(pid)
	if p == nil {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	k := &Key{
		ID:      newID(),
		Label:   strings.TrimSpace(req.Label),
		Value:   strings.TrimSpace(req.Value),
		Enabled: true,
		Status:  statusOK,
	}
	if k.Label == "" {
		k.Label = k.Masked()
	}
	if req.Enabled != nil {
		k.Enabled = *req.Enabled
	}
	p.mu.Lock()
	p.Keys = append(p.Keys, k)
	p.mu.Unlock()
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": k})
}

func (a *App) putKey(w http.ResponseWriter, r *http.Request, pid, kid string) {
	var req keyReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.store.MutateKey(pid, kid, func(_ *Provider, k *Key) error {
		if strings.TrimSpace(req.Label) != "" {
			k.Label = strings.TrimSpace(req.Label)
		}
		if strings.TrimSpace(req.Value) != "" {
			k.Value = strings.TrimSpace(req.Value)
		}
		if req.Enabled != nil {
			k.Enabled = *req.Enabled
		}
		return nil
	}) {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) deleteKey(w http.ResponseWriter, pid, kid string) {
	if !a.store.Mutate(pid, func(p *Provider) error {
		for i, k := range p.Keys {
			if k.ID == kid {
				p.Keys = append(p.Keys[:i], p.Keys[i+1:]...)
				return nil
			}
		}
		return nil
	}) {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) resetKey(w http.ResponseWriter, pid, kid string) {
	if !a.store.MutateKey(pid, kid, func(_ *Provider, k *Key) error {
		k.Status = statusOK
		k.CooldownUntil = 0
		k.LastError = ""
		return nil
	}) {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) testKey(w http.ResponseWriter, pid, kid string) {
	p := a.store.Provider(pid)
	if p == nil {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	k := a.store.FindKey(pid, kid)
	if k == nil {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	code, msg := a.testAndRecord(p, k)
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": code >= 200 && code < 400, "code": code, "message": msg,
	})
}

func (a *App) testProvider(w http.ResponseWriter, pid string) {
	p := a.store.Provider(pid)
	if p == nil {
		writeErr(w, http.StatusNotFound, "provider not found")
		return
	}
	type res struct {
		Label   string `json:"label"`
		ID      string `json:"id"`
		Code    int    `json:"code"`
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	var out []res
	keys := a.store.Snapshot()
	for _, sp := range keys.Providers {
		if sp.ID != pid {
			continue
		}
		for _, k := range sp.Keys {
			code, msg := a.testAndRecord(p, k)
			out = append(out, res{Label: k.Label, ID: k.ID, Code: code,
				OK: code >= 200 && code < 400, Message: msg})
		}
	}
	_ = a.store.Save()
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	// Tolerate a UTF-8 BOM, as written by several Windows clients.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return json.Unmarshal(data, v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func split(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
