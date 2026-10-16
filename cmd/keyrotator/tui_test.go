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
	for _, want := range []string{"KeyRotator", "Anthropic", "OpenAI", "team A", "team B", "healthy", "resting", "off", "RECENT REQUESTS"} {
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
