package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The TUI is a client of the running instance, exactly like the CLI and the
// web dashboard: it reads /api/state and /api/logs and sends the same
// actions. It never touches the config file, so it cannot race the server.

// Colours adapt to light and dark terminals. They mirror the web dashboard.
var (
	tuiInk    = lipgloss.AdaptiveColor{Light: "#1c1b19", Dark: "#ecebe6"}
	tuiMuted  = lipgloss.AdaptiveColor{Light: "#8b877d", Dark: "#7f7b71"}
	tuiLine   = lipgloss.AdaptiveColor{Light: "#d9d6cd", Dark: "#3d3a33"}
	tuiAccent = lipgloss.AdaptiveColor{Light: "#1f5f52", Dark: "#7cc7b3"}
	tuiOK     = lipgloss.AdaptiveColor{Light: "#2b7a53", Dark: "#7cc7a0"}
	tuiWarn   = lipgloss.AdaptiveColor{Light: "#92600f", Dark: "#e0b25c"}
	tuiBad    = lipgloss.AdaptiveColor{Light: "#a13a2b", Dark: "#ec8a7d"}

	stTitle = lipgloss.NewStyle().Bold(true).Foreground(tuiInk)
	stMuted = lipgloss.NewStyle().Foreground(tuiMuted)
	stHead  = lipgloss.NewStyle().Bold(true).Foreground(tuiMuted)
	stRule  = lipgloss.NewStyle().Foreground(tuiLine)
	stSel   = lipgloss.NewStyle().Bold(true).Foreground(tuiAccent)
	stOK    = lipgloss.NewStyle().Foreground(tuiOK)
	stWarn  = lipgloss.NewStyle().Foreground(tuiWarn)
	stBad   = lipgloss.NewStyle().Foreground(tuiBad)
)

type tuiMode int

const (
	modeNormal tuiMode = iota
	modeAddKey
	modeConfirmDelete
	modeRenameKey
)

type tuiModel struct {
	c      *cliClient
	st     *stateResp
	logs   []LogEntry
	err    error
	width  int
	height int

	pi, ki int // selected provider and key
	onKeys bool

	mode     tuiMode
	input    string
	flash    string
	flashBad bool
}

type tuiStateMsg struct {
	st   *stateResp
	logs []LogEntry
	err  error
}

type tuiActionMsg struct {
	msg string
	err error
}

type tuiTickMsg time.Time

// runTUI opens the full-screen dashboard against a running instance.
func runTUI(c *cliClient) int {
	p := tea.NewProgram(tuiModel{c: c}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "keyrotator tui: "+err.Error())
		return 1
	}
	return 0
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(fetchState(m.c), tuiTick())
}

func tuiTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tuiTickMsg(t) })
}

func fetchState(c *cliClient) tea.Cmd {
	return func() tea.Msg {
		st, err := c.state()
		if err != nil {
			return tuiStateMsg{err: err}
		}
		var lg struct {
			Logs []LogEntry `json:"logs"`
		}
		if err := c.call(http.MethodGet, "/api/logs", nil, &lg); err != nil {
			return tuiStateMsg{err: err}
		}
		return tuiStateMsg{st: st, logs: lg.Logs}
	}
}

// act runs one admin action off the UI goroutine, then refreshes the state.
func act(c *cliClient, label string, fn func(c *cliClient) (string, error)) tea.Cmd {
	return func() tea.Msg {
		msg, err := fn(c)
		if err != nil {
			return tuiActionMsg{err: err}
		}
		if msg == "" {
			msg = label
		}
		return tuiActionMsg{msg: msg}
	}
}

func (m *tuiModel) clamp() {
	if m.st == nil || len(m.st.Providers) == 0 {
		m.pi, m.ki = 0, 0
		return
	}
	if m.pi >= len(m.st.Providers) {
		m.pi = len(m.st.Providers) - 1
	}
	n := len(m.st.Providers[m.pi].Keys)
	if m.ki >= n {
		m.ki = max(n-1, 0)
	}
}

func (m tuiModel) selProvider() *Provider {
	if m.st == nil || m.pi >= len(m.st.Providers) {
		return nil
	}
	return m.st.Providers[m.pi]
}

func (m tuiModel) selKey() (*Provider, *Key) {
	p := m.selProvider()
	if p == nil || m.ki >= len(p.Keys) {
		return p, nil
	}
	return p, p.Keys[m.ki]
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tuiTickMsg:
		return m, tea.Batch(fetchState(m.c), tuiTick())
	case tuiStateMsg:
		m.err = msg.err
		if msg.err == nil {
			m.st, m.logs = msg.st, msg.logs
			m.clamp()
		}
	case tuiActionMsg:
		m.flash, m.flashBad = msg.msg, msg.err != nil
		if msg.err != nil {
			m.flash = msg.err.Error()
		}
		return m, fetchState(m.c)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m tuiModel) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.mode == modeRenameKey {
		return m.handleRenameKey(k)
	}

	switch m.mode {
	case modeAddKey:
		switch k.Type {
		case tea.KeyEsc:
			m.mode, m.input = modeNormal, ""
		case tea.KeyBackspace:
			if r := []rune(m.input); len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
		case tea.KeyEnter:
			value := strings.TrimSpace(m.input)
			m.mode, m.input = modeNormal, ""
			p := m.selProvider()
			if value == "" || p == nil {
				m.flash, m.flashBad = "nothing added", true
				return m, nil
			}
			pid := p.ID
			return m, act(m.c, "key added", func(c *cliClient) (string, error) {
				return "", c.call(http.MethodPost, "/api/providers/"+pid+"/keys", map[string]any{"value": value}, nil)
			})
		case tea.KeySpace:
			m.input += " "
		case tea.KeyRunes:
			m.input += string(k.Runes)
		}
		return m, nil

	case modeConfirmDelete:
		m.mode = modeNormal
		if k.String() != "y" {
			m.flash, m.flashBad = "delete cancelled", false
			return m, nil
		}
		p, key := m.selKey()
		if p == nil || key == nil {
			return m, nil
		}
		pid, kid := p.ID, key.ID
		return m, act(m.c, "key deleted", func(c *cliClient) (string, error) {
			return "", c.call(http.MethodDelete, "/api/providers/"+pid+"/keys/"+kid, nil, nil)
		})
	}

	switch k.String() {
	case "q":
		return m, tea.Quit

	case "up", "k":
		if m.onKeys {
			m.ki = max(m.ki-1, 0)
		} else if m.pi > 0 {
			m.pi--
			m.ki = 0
		}
	case "down", "j":
		if m.onKeys {
			if p := m.selProvider(); p != nil && m.ki < len(p.Keys)-1 {
				m.ki++
			}
		} else if m.st != nil && m.pi < len(m.st.Providers)-1 {
			m.pi++
			m.ki = 0
		}
	case "tab":
		m.onKeys = !m.onKeys
	case "left", "h":
		m.onKeys = false
	case "right", "l":
		m.onKeys = true

	case "g":
		return m, fetchState(m.c)

	case "t":
		p, key := m.selKey()
		if key == nil {
			return m, nil
		}
		pid, kid := p.ID, key.ID
		return m, act(m.c, "", func(c *cliClient) (string, error) {
			var r struct {
				OK   bool `json:"ok"`
				Code int  `json:"code"`
			}
			if err := c.call(http.MethodPost, "/api/providers/"+pid+"/keys/"+kid+"/test", nil, &r); err != nil {
				return "", err
			}
			if !r.OK {
				return "", fmt.Errorf("key failed (HTTP %d)", r.Code)
			}
			return fmt.Sprintf("key OK (HTTP %d)", r.Code), nil
		})

	case "T":
		p := m.selProvider()
		if p == nil {
			return m, nil
		}
		pid := p.ID
		return m, act(m.c, "", func(c *cliClient) (string, error) {
			var r struct {
				Results []struct {
					OK bool `json:"ok"`
				} `json:"results"`
			}
			if err := c.call(http.MethodPost, "/api/providers/"+pid+"/test", nil, &r); err != nil {
				return "", err
			}
			bad := 0
			for _, x := range r.Results {
				if !x.OK {
					bad++
				}
			}
			if bad > 0 {
				return "", fmt.Errorf("%d key(s) failed", bad)
			}
			return "all keys OK", nil
		})

	case "e":
		p, key := m.selKey()
		if key == nil {
			return m, nil
		}
		pid, kid, on := p.ID, key.ID, !key.Enabled
		return m, act(m.c, "", func(c *cliClient) (string, error) {
			err := c.call(http.MethodPut, "/api/providers/"+pid+"/keys/"+kid, map[string]any{"enabled": on}, nil)
			if on {
				return "key enabled", err
			}
			return "key disabled", err
		})

	case "P":
		p := m.selProvider()
		if p == nil {
			return m, nil
		}
		pid, on := p.ID, !p.Enabled
		return m, act(m.c, "", func(c *cliClient) (string, error) {
			err := c.call(http.MethodPut, "/api/providers/"+pid, map[string]any{"enabled": on}, nil)
			if on {
				return "provider enabled", err
			}
			return "provider disabled", err
		})

	case "r":
		p, key := m.selKey()
		if key == nil {
			return m, nil
		}
		pid, kid := p.ID, key.ID
		return m, act(m.c, "key reset", func(c *cliClient) (string, error) {
			return "", c.call(http.MethodPost, "/api/providers/"+pid+"/keys/"+kid+"/reset", nil, nil)
		})

	case "n":
		if _, key := m.selKey(); key != nil {
			m.mode, m.input, m.flash = modeRenameKey, keyLabel(key), ""
		}

	case "a":
		if m.selProvider() == nil {
			m.flash, m.flashBad = "add a provider first: keyrotator add <name> <base-url>", true
			return m, nil
		}
		m.mode, m.input, m.flash = modeAddKey, "", ""

	case "d":
		if _, key := m.selKey(); key != nil {
			m.mode = modeConfirmDelete
			m.flash, m.flashBad = fmt.Sprintf("delete key %q? press y to confirm", keyLabel(key)), false
		}
	}
	return m, nil
}

// handleRenameKey edits the selected key's alias in place. The input starts
// with the current alias, so a key press only changes what is typed.
func (m tuiModel) handleRenameKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode, m.input = modeNormal, ""
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case tea.KeyEnter:
		alias := strings.TrimSpace(m.input)
		m.mode, m.input = modeNormal, ""
		p, key := m.selKey()
		if alias == "" || key == nil {
			m.flash, m.flashBad = "alias cannot be empty", true
			return m, nil
		}
		pid, kid := p.ID, key.ID
		return m, act(m.c, "alias saved", func(c *cliClient) (string, error) {
			return "", c.call(http.MethodPut, "/api/providers/"+pid+"/keys/"+kid, map[string]any{"label": alias}, nil)
		})
	case tea.KeySpace:
		m.input += " "
	case tea.KeyRunes:
		m.input += string(k.Runes)
	}
	return m, nil
}

// ---------- rendering ----------

func (m tuiModel) View() string {
	if m.width == 0 {
		return ""
	}
	w := m.width
	var lines []string

	status := stOK.Render("● online")
	meta := ""
	if m.st != nil {
		meta = "v" + m.st.Version + "  " + m.st.Addr
	}
	if m.err != nil {
		status = stBad.Render("● offline")
	}
	lines = append(lines, stTitle.Render("KeyRotator")+"  "+stMuted.Render(meta)+"  "+status)
	lines = append(lines, stRule.Render(strings.Repeat("─", w)))

	if m.err != nil {
		lines = append(lines,
			"",
			stBad.Render("Cannot reach keyrotator: "+m.err.Error()),
			stMuted.Render("Start it first, e.g. `keyrotator -open`, or pass -addr / -admin-token."),
		)
	} else {
		lines = append(lines, m.body()...)
	}

	lines = append(lines, "", stRule.Render(strings.Repeat("─", w)))
	lines = append(lines, stMuted.Render("↑↓ move   ←→ pane   t test key   T test all   e key on/off   P provider on/off"))
	lines = append(lines, stMuted.Render("n rename   r reset key   a add key   d delete key   g refresh   q quit"))
	lines = append(lines, m.statusLine())

	for i, l := range lines {
		lines[i] = lipglossClip(l, w)
	}
	return strings.Join(lines, "\n")
}

// body is the provider list, the keys of the selected provider, and recent
// requests.
func (m tuiModel) body() []string {
	w := m.width
	leftW := min(30, w/3)
	rightW := w - leftW - 3

	left := m.providerPane(leftW)
	right := m.keyPane(rightW)
	rows := max(len(left), len(right))

	var out []string
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padW(l, leftW)+stRule.Render(" │ ")+r)
	}

	out = append(out, "", stHead.Render("RECENT REQUESTS"))
	if len(m.logs) == 0 {
		out = append(out, stMuted.Render("no requests yet - send one through a proxy URL"))
	}
	for i, l := range m.logs {
		if i == 5 {
			break
		}
		badge := stOK.Render(fmt.Sprintf("%d", l.Code))
		if !l.OK {
			badge = stBad.Render(fmt.Sprintf("%d", l.Code))
		}
		key := l.Key
		if key == "" {
			key = "-"
		}
		out = append(out, fmt.Sprintf("%s  %s  %s  %s  %s  %s",
			stMuted.Render(time.Unix(l.Time, 0).Format("15:04:05")),
			badge,
			clipPlain(l.Method+" "+l.Path, 34),
			clipPlain(l.Provider, 14),
			stMuted.Render(key),
			stMuted.Render(fmt.Sprintf("%dms", l.Ms)),
		))
	}
	return out
}

func (m tuiModel) providerPane(w int) []string {
	out := []string{stHead.Render("PROVIDERS"), ""}
	if m.st == nil || len(m.st.Providers) == 0 {
		return append(out, stMuted.Render("none yet"), stMuted.Render("keyrotator add <name> <url>"))
	}
	for i, p := range m.st.Providers {
		cursor := "  "
		name := clipPlain(p.Name, w-12)
		if i == m.pi {
			cursor = "› "
			if !m.onKeys {
				name = stSel.Render(name)
			} else {
				name = stTitle.Render(name)
			}
		} else if !p.Enabled {
			name = stMuted.Render(name)
		} else {
			name = stTitle.Render(name)
		}
		count := stMuted.Render(fmt.Sprintf("%d keys", len(p.Keys)))
		out = append(out, cursor+padW(name, w-10)+count)
	}
	return out
}

func (m tuiModel) keyPane(w int) []string {
	p := m.selProvider()
	if p == nil {
		return []string{stHead.Render("KEYS"), "", stMuted.Render("select a provider")}
	}
	healthy := 0
	for _, k := range p.Keys {
		if k.Enabled && k.Status == statusOK {
			healthy++
		}
	}
	out := []string{
		stHead.Render("KEYS") + "  " + stTitle.Render(clipPlain(p.Name, 24)) + "  " +
			stMuted.Render(fmt.Sprintf("%d/%d healthy", healthy, len(p.Keys))),
		stMuted.Render(fmt.Sprintf("  %-12s %-14s %-12s %4s %4s  %s", "LABEL", "KEY", "STATUS", "OK", "ERR", "LAST USED")),
	}
	if len(p.Keys) == 0 {
		return append(out, "", stMuted.Render("  no keys yet - press a to add one"))
	}
	now := time.Now().Unix()
	for i, k := range p.Keys {
		cursor := "  "
		label := padW(clipPlain(keyLabel(k), 12), 12)
		if i == m.ki && m.onKeys {
			cursor = stSel.Render("› ")
			label = stSel.Render(padW(clipPlain(keyLabel(k), 12), 12))
		} else if i == m.ki {
			cursor = "› "
		}
		text, colour := keyStatus(k, now)
		st := colour.Render(padW(text, 12))
		used := "-"
		if k.LastUsed > 0 {
			used = ago(k.LastUsed)
		}
		out = append(out, fmt.Sprintf("%s%s %s %s %4d %4d  %s",
			cursor, label, padW(maskSecret(k.Value), 14), st, k.Success, k.Errors, stMuted.Render(used)))
	}
	return out
}

func (m tuiModel) statusLine() string {
	switch m.mode {
	case modeAddKey:
		return stSel.Render("add key ") + stMuted.Render(maskTyped(m.input)) + stSel.Render("▏") + stMuted.Render("   enter save · esc cancel")
	case modeRenameKey:
		return stSel.Render("alias ") + stMuted.Render(m.input) + stSel.Render("▏") + stMuted.Render("   enter save · esc cancel")
	case modeConfirmDelete:
		return stWarn.Render(m.flash)
	}
	if m.flash == "" {
		return ""
	}
	if m.flashBad {
		return stBad.Render(m.flash)
	}
	return stOK.Render(m.flash)
}

// ---------- helpers ----------

func keyLabel(k *Key) string {
	if k.Label != "" {
		return k.Label
	}
	return k.ID
}

// keyStatus mirrors the web dashboard's badges, as text plus colour.
func keyStatus(k *Key, now int64) (string, lipgloss.Style) {
	switch {
	case !k.Enabled:
		return "disabled", stMuted
	case k.Status == statusCooldown && k.CooldownUntil > now:
		return fmt.Sprintf("cooling %ds", k.CooldownUntil-now), stWarn
	case k.Status == statusInvalid, k.Status == statusError:
		return k.Status, stBad
	case k.Status == statusOK:
		return "ok", stOK
	default:
		return k.Status, stMuted
	}
}

// maskTyped shows the length of a key being typed without echoing it.
func maskTyped(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("•", len(r))
	}
	return strings.Repeat("•", len(r)-4) + string(r[len(r)-4:])
}

// padW pads to the visible width, so styled strings line up.
func padW(s string, w int) string {
	n := lipgloss.Width(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func clipPlain(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func lipglossClip(s string, w int) string {
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}
