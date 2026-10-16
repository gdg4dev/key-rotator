package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// ruleNames lists the names of the rules that would be sent, in order.
func ruleNames(rules []InjectRule) string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
}

func TestUngroupedRulesCombineFreely(t *testing.T) {
	rules := []InjectRule{
		{Enabled: true, In: "header", Name: "x-a"},
		{Enabled: false, In: "header", Name: "x-b"},
		{Enabled: true, In: "query", Name: "api_key"},
	}
	if got := ruleNames(activeRules(rules)); got != "x-a,api_key" {
		t.Errorf("active = %q, want x-a,api_key", got)
	}
}

func TestGroupIsAllOrNothing(t *testing.T) {
	rules := []InjectRule{
		{Enabled: true, In: "header", Name: "x-d", Group: "gamma"},
		{Enabled: false, In: "header", Name: "x-e", Group: "gamma"}, // one member off holds the group
		{Enabled: true, In: "header", Name: "x-solo"},
	}
	if got := ruleNames(activeRules(rules)); got != "x-solo" {
		t.Errorf("active = %q, want only x-solo", got)
	}
}

func TestGroupNameIsCaseAndSpaceInsensitive(t *testing.T) {
	rules := []InjectRule{
		{Enabled: true, In: "header", Name: "x-a", Group: "Beta"},
		{Enabled: true, In: "header", Name: "x-b", Group: " beta "},
		{Enabled: false, In: "header", Name: "x-c", Group: "BETA"},
	}
	if got := ruleNames(activeRules(rules)); got != "" {
		t.Errorf("active = %q, want none: BETA is held by its off member", got)
	}
}

func TestGroupWithBlankNameIsHeld(t *testing.T) {
	rules := []InjectRule{
		{Enabled: true, In: "header", Name: "", Group: "pair"},
		{Enabled: true, In: "header", Name: "x-p", Group: "pair"},
		{Enabled: true, In: "header", Name: "x-solo"},
	}
	if got := ruleNames(activeRules(rules)); got != "x-solo" {
		t.Errorf("active = %q, want only x-solo", got)
	}
}

func TestKeyPlaceAndFixedValueCanShareAGroup(t *testing.T) {
	rules := []InjectRule{
		{Enabled: true, In: "header", Name: "x-api-key", Source: "key", Group: "tenant"},
		{Enabled: true, In: "header", Name: "x-tenant", Source: "static", Value: "acme", Group: "tenant"},
	}
	if got := ruleNames(activeRules(rules)); got != "x-api-key,x-tenant" {
		t.Errorf("both on: active = %q", got)
	}
	rules[1].Enabled = false
	if got := ruleNames(activeRules(rules)); got != "" {
		t.Errorf("fixed value off: active = %q, want none", got)
	}
}

func TestValidateGroup(t *testing.T) {
	if err := validateGroup("beta"); err != nil {
		t.Errorf("beta rejected: %v", err)
	}
	if err := validateGroup(""); err != nil {
		t.Errorf("empty (no group) rejected: %v", err)
	}
	if err := validateGroup(strings.Repeat("g", maxGroupLen+1)); err == nil {
		t.Error("over-long group accepted")
	}
	if err := validateGroup("bad\tname"); err == nil {
		t.Error("control character accepted")
	}
}

func TestNormalizeKeepsGroupedRuleWithBlankName(t *testing.T) {
	got := normalizeRules([]InjectRule{
		{Enabled: true, Name: "", Group: "pair"}, // kept: it holds its group
		{Enabled: true, Name: "", Group: ""},     // dropped: a rule with nothing in it
		{Enabled: true, Name: "x-a"},
	})
	if len(got) != 2 || got[0].Group != "pair" || got[1].Name != "x-a" {
		t.Errorf("normalize = %+v", got)
	}
}

func TestProxyHoldsGroupWhenAMemberIsOff(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rules := func(versionOn bool) []InjectRule {
		return []InjectRule{
			{Enabled: true, In: "header", Name: "x-api-key", Source: "key", Group: "pair"},
			{Enabled: versionOn, In: "header", Name: "x-version", Source: "static", Value: "2024-02-01", Group: "pair"},
		}
	}
	key := []*Key{{ID: "k1", Value: "secret-1", Enabled: true, Status: statusOK}}
	app.store.AddProvider(&Provider{ID: "held", Name: "held", BaseURL: upstream.URL, Enabled: true,
		Strategy: "round_robin", Inject: rules(false), Keys: key})
	app.store.AddProvider(&Provider{ID: "sent", Name: "sent", BaseURL: upstream.URL, Enabled: true,
		Strategy: "round_robin", Inject: rules(true), Keys: []*Key{{ID: "k2", Value: "secret-2", Enabled: true, Status: statusOK}}})

	call := func(pid string) {
		rec := httptest.NewRecorder()
		app.handleProxy(rec, httptest.NewRequest(http.MethodPost, "/p/"+pid+"/chat", bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", pid, rec.Code, rec.Body.String())
		}
	}

	call("held")
	if seen.Get("x-api-key") != "" || seen.Get("x-version") != "" {
		t.Errorf("held group was sent: x-api-key=%q x-version=%q", seen.Get("x-api-key"), seen.Get("x-version"))
	}
	call("sent")
	if seen.Get("x-api-key") != "secret-2" || seen.Get("x-version") != "2024-02-01" {
		t.Errorf("complete group not sent together: x-api-key=%q x-version=%q", seen.Get("x-api-key"), seen.Get("x-version"))
	}
}
