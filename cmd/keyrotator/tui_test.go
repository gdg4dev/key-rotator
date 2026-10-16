package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func testTUIModel() tuiModel {
	now := time.Now().Unix()
	st := &stateResp{
		Version: "test",
		Addr:    "127.0.0.1:8787",
		Providers: []*Provider{
			{ID: "p1", Name: "Anthropic", Enabled: true},
			{ID: "p2", Name: "OpenAI", Enabled: true, Keys: []*Key{
				{ID: "k1", Label: "team A", Value: "sk-proj-AAAA1111BBBB2222CCCC", Enabled: true, Status: statusOK, Success: 4, LastUsed: now - 30},
				{ID: "k2", Label: "team B", Value: "sk-proj-DDDD3333EEEE4444FFFF", Enabled: true, Status: statusCooldown, CooldownUntil: now + 45},
				{ID: "k3", Label: "spare", Value: "sk-proj-GGGG5555HHHH6666IIII", Enabled: false},
			}},
		},
	}
	m := tuiModel{st: st, width: 110, height: 40}
	return m
}

func press(m tuiModel, key string) tuiModel {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	return next.(tuiModel)
}

func TestTUIRendersProvidersAndKeys(t *testing.T) {
	m := testTUIModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
	m = next.(tuiModel)
	m = press(m, "down") // select OpenAI, which has the keys

	view := m.View()
	for _, want := range []string{"KeyRotator", "Anthropic", "OpenAI", "team A", "team B", "ok", "cooling", "disabled", "RECENT REQUESTS"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q", want)
		}
	}
	if strings.Contains(view, "AAAA1111BBBB2222CCCC") {
		t.Error("full API key leaked into the TUI view")
	}
}

func TestTUINavigationMovesBetweenProvidersAndKeys(t *testing.T) {
	m := testTUIModel()
	m = press(m, "down") // Anthropic -> OpenAI
	if m.pi != 1 {
		t.Fatalf("pi = %d, want 1", m.pi)
	}
	m = press(m, "right") // focus keys
	m = press(m, "down")
	m = press(m, "down")
	m = press(m, "down") // clamp at the last key
	if m.ki != 2 {
		t.Fatalf("ki = %d, want 2 (last key)", m.ki)
	}
	m = press(m, "left")
	if m.onKeys {
		t.Error("left should return focus to the provider list")
	}
}

func TestTUIAddKeyCollectsInputAndCancels(t *testing.T) {
	m := testTUIModel()
	m = press(m, "down") // OpenAI has keys
	m = press(m, "a")
	if m.mode != modeAddKey {
		t.Fatal("'a' should enter add-key mode")
	}
	for _, r := range "sk-xyz" {
		m = press(m, string(r))
	}
	m = press(m, "backspace")
	if m.input != "sk-xy" {
		t.Errorf("input = %q, want %q", m.input, "sk-xy")
	}
	m = press(m, "esc")
	if m.mode != modeNormal || m.input != "" {
		t.Error("esc should cancel add-key mode and clear the input")
	}
}

func TestTUIDeleteNeedsConfirmation(t *testing.T) {
	m := testTUIModel()
	m = press(m, "down")
	m = press(m, "d")
	if m.mode != modeConfirmDelete {
		t.Fatal("'d' should ask for confirmation")
	}
	m = press(m, "n")
	if m.mode != modeNormal {
		t.Error("any key other than y should cancel the delete")
	}
}

func TestTUIMaskTyped(t *testing.T) {
	if got := maskTyped("sk-abcdef"); got != "•••••cdef" {
		t.Errorf("maskTyped = %q", got)
	}
	if got := maskTyped("abc"); got != "•••" {
		t.Errorf("maskTyped short = %q", got)
	}
}

func TestClipPlainKeepsWidth(t *testing.T) {
	if got := clipPlain("abcdefghij", 5); got != "abcd…" {
		t.Errorf("clipPlain = %q", got)
	}
	if got := clipPlain("abc", 5); got != "abc" {
		t.Errorf("clipPlain short = %q", got)
	}
}

func TestTUIRenameKeyEditsAliasInPlace(t *testing.T) {
	m := testTUIModel()
	m = press(m, "down") // OpenAI
	m = press(m, "n")
	if m.mode != modeRenameKey {
		t.Fatal("'n' should enter rename mode")
	}
	if m.input != "team A" {
		t.Fatalf("rename should start with the current alias, got %q", m.input)
	}
	m = press(m, "backspace")
	m = press(m, "B")
	if m.input != "team B" {
		t.Errorf("input = %q", m.input)
	}
	m = press(m, "esc")
	if m.mode != modeNormal {
		t.Error("esc should cancel rename")
	}
}
