package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Key is a single API key inside a provider pool.
type Key struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Value         string `json:"value"`
	Enabled       bool   `json:"enabled"`
	Status        string `json:"status"`        // ok | cooldown | invalid | error
	CooldownUntil int64  `json:"cooldownUntil"` // unix seconds
	LastUsed      int64  `json:"lastUsed"`      // unix seconds
	LastError     string `json:"lastError"`
	Success       int64  `json:"success"`
	Errors        int64  `json:"errors"`
}

// Masked returns a safe-to-display version of the key value.
func (k *Key) Masked() string {
	v := k.Value
	switch {
	case len(v) == 0:
		return ""
	case len(v) <= 4:
		return strings.Repeat("*", len(v))
	case len(v) <= 10:
		return v[:2] + strings.Repeat("*", len(v)-4) + v[len(v)-2:]
	default:
		return v[:4] + "..." + v[len(v)-4:]
	}
}

// InjectRule describes one outgoing header or query parameter.
//
// Source decides what is written: "key" uses the rotating pooled API key,
// "static" writes the fixed Value. Prefix/suffix are written around the chosen
// value and are kept verbatim ("Bearer " must keep its trailing space).
type InjectRule struct {
	Enabled bool   `json:"enabled"`
	In      string `json:"in"`     // "header" | "query"
	Name    string `json:"name"`   // header name or query parameter name
	Source  string `json:"source"` // "key" | "static"
	Value   string `json:"value"`  // used when Source == "static"
	Prefix  string `json:"prefix"` // prepended to the value, e.g. "Bearer "
	Suffix  string `json:"suffix"` // appended to the value
}

// Provider is an upstream API plus its pool of keys.
type Provider struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	BaseURL    string       `json:"baseUrl"`
	Inject     []InjectRule `json:"inject"`
	HealthPath string       `json:"healthPath"`
	Strategy   string       `json:"strategy"` // round_robin | random | least_used
	Enabled    bool         `json:"enabled"`
	Keys       []*Key       `json:"keys"`

	// ProxyToken secures this provider's /p/<id>/ URL. Empty means "use the
	// global proxyToken" (see Config.ProxyToken).
	ProxyToken string `json:"proxyToken,omitempty"`

	// Rate limits in requests per second. nil means "inherit the global
	// default"; a pointer to 0 disables the limit for this provider.
	RateLimitRPS    *float64 `json:"rateLimitRPS,omitempty"`
	KeyRateLimitRPS *float64 `json:"keyRateLimitRPS,omitempty"`

	// Legacy single-header config, migrated into Inject on load.
	AuthHeader string `json:"authHeader,omitempty"`
	AuthPrefix string `json:"authPrefix,omitempty"`

	mu sync.Mutex `json:"-"`
	rr int        `json:"-"`
}

func normalizeRules(in []InjectRule) []InjectRule {
	out := make([]InjectRule, 0, len(in))
	for _, r := range in {
		r.In = strings.ToLower(strings.TrimSpace(r.In))
		if r.In != "query" {
			r.In = "header"
		}
		r.Name = strings.TrimSpace(r.Name)
		if r.Name == "" {
			continue
		}
		r.Source = strings.ToLower(strings.TrimSpace(r.Source))
		if r.Source != "static" {
			r.Source = "key"
		}
		r.Value = strings.TrimSpace(r.Value)
		// Prefix and Suffix are deliberately left untouched.
		out = append(out, r)
	}
	return out
}

func (p *Provider) normalize() {
	assignAliases(p.Keys)
	if p.ID == "" {
		p.ID = newID()
	}
	if strings.TrimSpace(p.Name) == "" {
		p.Name = p.ID
	}
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if p.Inject == nil {
		name, prefix := p.AuthHeader, p.AuthPrefix
		if strings.TrimSpace(name) == "" {
			name = "Authorization"
			if prefix == "" {
				prefix = "Bearer "
			}
		}
		p.Inject = []InjectRule{{Enabled: true, In: "header", Name: name, Prefix: prefix}}
	}
	p.Inject = normalizeRules(p.Inject)
	p.AuthHeader, p.AuthPrefix = "", ""
	if p.Strategy == "" {
		p.Strategy = "round_robin"
	}
	if p.HealthPath == "" {
		p.HealthPath = "/models"
	}
	if !strings.HasPrefix(p.HealthPath, "/") {
		p.HealthPath = "/" + p.HealthPath
	}
	p.ProxyToken = strings.TrimSpace(p.ProxyToken)
	p.RateLimitRPS = clampRate(p.RateLimitRPS)
	p.KeyRateLimitRPS = clampRate(p.KeyRateLimitRPS)
	for _, k := range p.Keys {
		if k.ID == "" {
			k.ID = newID()
		}
		if k.Status == "" {
			k.Status = "ok"
		}
	}
}

// clampRate rejects negative rates and rounds tiny ones away.
func clampRate(v *float64) *float64 {
	if v == nil {
		return nil
	}
	if *v < 0 {
		z := 0.0
		return &z
	}
	return v
}

// Config is the persisted application state.
type Config struct {
	Listen      string      `json:"listen"`
	AdminToken  string      `json:"adminToken"`
	MaxRetries  int         `json:"maxRetries"`
	CooldownSec int         `json:"cooldownSec"`
	Providers   []*Provider `json:"providers"`

	// ProxyToken, when set, is required by every /p/ request. Clients may send
	// it as "X-Rotator-Token: <t>", "Authorization: Bearer <t>" or "?token=<t>".
	ProxyToken string `json:"proxyToken"`

	// Global rate limits in requests per second; 0 means unlimited. Providers
	// may override either value.
	RateLimitRPS    float64 `json:"rateLimitRPS"`
	KeyRateLimitRPS float64 `json:"keyRateLimitRPS"`
}

// Settings is a plain copy of the global (non-provider) settings.
type Settings struct {
	Listen          string
	AdminToken      string
	MaxRetries      int
	CooldownSec     int
	ProxyToken      string
	RateLimitRPS    float64
	KeyRateLimitRPS float64
}

func defaultConfig() *Config {
	return &Config{
		Listen:      "127.0.0.1:8787",
		MaxRetries:  3,
		CooldownSec: 60,
		Providers:   []*Provider{},
	}
}

func (c *Config) normalize() {
	if strings.TrimSpace(c.Listen) == "" {
		c.Listen = "127.0.0.1:8787"
	}
	if c.MaxRetries < 1 {
		c.MaxRetries = 1
	}
	if c.MaxRetries > 10 {
		c.MaxRetries = 10
	}
	if c.CooldownSec < 1 {
		c.CooldownSec = 60
	}
	c.ProxyToken = strings.TrimSpace(c.ProxyToken)
	if c.RateLimitRPS < 0 {
		c.RateLimitRPS = 0
	}
	if c.KeyRateLimitRPS < 0 {
		c.KeyRateLimitRPS = 0
	}
	for _, p := range c.Providers {
		p.normalize()
	}
}

// effectiveProviderRPS resolves a provider's own limit against the global one.
func effectiveProviderRPS(p *Provider, global float64) float64 {
	if p.RateLimitRPS != nil {
		return *p.RateLimitRPS
	}
	return global
}

// effectiveKeyRPS resolves the per-key limit for a provider.
func effectiveKeyRPS(p *Provider, global float64) float64 {
	if p.KeyRateLimitRPS != nil {
		return *p.KeyRateLimitRPS
	}
	return global
}

// effectiveProxyToken resolves a provider's own proxy token against the global
// one. An empty result means the provider's /p/ URL needs no token.
func effectiveProxyToken(p *Provider, global string) string {
	if p.ProxyToken != "" {
		return p.ProxyToken
	}
	return global
}

// Store owns the config file and serialises access to it.
type Store struct {
	path string
	mu   sync.RWMutex
	cfg  *Config
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		s.cfg = defaultConfig()
		if err := s.Save(); err != nil {
			return nil, err
		}
		return s, nil
	case err != nil:
		return nil, err
	}
	// Tolerate a UTF-8 BOM, as written by several Windows editors.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	cfg := defaultConfig()
	if len(data) > 0 {
		if perr := json.Unmarshal(data, cfg); perr != nil {
			// A malformed config must not keep an always-on service from starting.
			backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
			if rerr := os.Rename(path, backup); rerr != nil {
				log.Printf("config %s is invalid (%v) and could not be backed up: %v", path, perr, rerr)
			} else {
				log.Printf("config %s is invalid (%v); moved to %s and starting with defaults", path, perr, backup)
			}
			cfg = defaultConfig()
		}
	}
	cfg.normalize()
	s.cfg = cfg
	return s, nil
}

// Save writes the config atomically. The whole tree is snapshotted under lock.
func (s *Store) Save() error {
	s.mu.RLock()
	for _, p := range s.cfg.Providers {
		p.mu.Lock()
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	for _, p := range s.cfg.Providers {
		p.mu.Unlock()
	}
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Path() string { return s.path }

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := Config{
		Listen:          s.cfg.Listen,
		AdminToken:      s.cfg.AdminToken,
		MaxRetries:      s.cfg.MaxRetries,
		CooldownSec:     s.cfg.CooldownSec,
		ProxyToken:      s.cfg.ProxyToken,
		RateLimitRPS:    s.cfg.RateLimitRPS,
		KeyRateLimitRPS: s.cfg.KeyRateLimitRPS,
		Providers:       []*Provider{},
	}
	for _, p := range s.cfg.Providers {
		p.mu.Lock()
		cp := &Provider{
			ID:              p.ID,
			Name:            p.Name,
			BaseURL:         p.BaseURL,
			Inject:          append([]InjectRule{}, p.Inject...),
			HealthPath:      p.HealthPath,
			Strategy:        p.Strategy,
			Enabled:         p.Enabled,
			ProxyToken:      p.ProxyToken,
			RateLimitRPS:    p.RateLimitRPS,
			KeyRateLimitRPS: p.KeyRateLimitRPS,
			Keys:            make([]*Key, 0, len(p.Keys)),
		}
		for _, k := range p.Keys {
			kk := *k
			cp.Keys = append(cp.Keys, &kk)
		}
		p.mu.Unlock()
		out.Providers = append(out.Providers, cp)
	}
	return out
}

// Settings returns a copy of the global settings.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Settings{
		Listen:          s.cfg.Listen,
		AdminToken:      s.cfg.AdminToken,
		MaxRetries:      s.cfg.MaxRetries,
		CooldownSec:     s.cfg.CooldownSec,
		ProxyToken:      s.cfg.ProxyToken,
		RateLimitRPS:    s.cfg.RateLimitRPS,
		KeyRateLimitRPS: s.cfg.KeyRateLimitRPS,
	}
}

func (s *Store) Providers() []*Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Provider(nil), s.cfg.Providers...)
}

func (s *Store) Provider(id string) *Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.cfg.Providers {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// FindProvider resolves a provider by id, by name (case-insensitive) or by an
// unambiguous id prefix. It is used by the CLI.
func (s *Store) FindProvider(ref string) *Provider {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p := s.findLocked(ref); p != nil {
		return p
	}
	var match *Provider
	for _, p := range s.cfg.Providers {
		if strings.EqualFold(p.Name, ref) || strings.HasPrefix(p.ID, ref) {
			if match != nil && match != p {
				return nil // ambiguous
			}
			match = p
		}
	}
	return match
}

func (s *Store) findLocked(id string) *Provider {
	for _, p := range s.cfg.Providers {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (s *Store) AddProvider(p *Provider) {
	p.normalize()
	s.mu.Lock()
	s.cfg.Providers = append(s.cfg.Providers, p)
	s.mu.Unlock()
}

func (s *Store) DeleteProvider(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.cfg.Providers {
		if p.ID == id {
			s.cfg.Providers = append(s.cfg.Providers[:i], s.cfg.Providers[i+1:]...)
			return true
		}
	}
	return false
}

// Mutate runs fn against a provider under the provider lock. Returns false if
// the provider does not exist.
func (s *Store) Mutate(id string, fn func(*Provider) error) bool {
	p := s.Provider(id)
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = fn(p)
	return true
}

func (s *Store) MutateKey(pid, kid string, fn func(*Provider, *Key) error) bool {
	p := s.Provider(pid)
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range p.Keys {
		if k.ID == kid {
			_ = fn(p, k)
			return true
		}
	}
	return false
}

func (s *Store) FindKey(pid, kid string) *Key {
	p := s.Provider(pid)
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range p.Keys {
		if k.ID == kid {
			cp := *k
			return &cp
		}
	}
	return nil
}

func (s *Store) UpdateSettings(u Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.MaxRetries >= 1 && u.MaxRetries <= 10 {
		s.cfg.MaxRetries = u.MaxRetries
	}
	if u.CooldownSec >= 1 {
		s.cfg.CooldownSec = u.CooldownSec
	}
	if u.Listen != "" {
		// Takes effect the next time the server starts.
		s.cfg.Listen = u.Listen
	}
	s.cfg.AdminToken = u.AdminToken
	s.cfg.ProxyToken = u.ProxyToken
	if u.RateLimitRPS >= 0 {
		s.cfg.RateLimitRPS = u.RateLimitRPS
	}
	if u.KeyRateLimitRPS >= 0 {
		s.cfg.KeyRateLimitRPS = u.KeyRateLimitRPS
	}
}

func newID() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "id" + hex.EncodeToString([]byte{byte(os.Getpid())})
	}
	return hex.EncodeToString(b)
}

// RandomToken returns a URL-safe random secret, used for proxy/admin tokens.
func RandomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte{byte(os.Getpid())})
	}
	return hex.EncodeToString(b)
}
