package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveRules(t *testing.T) {
	on := func(name, group string) InjectRule {
		return InjectRule{Enabled: true, In: "header", Name: name, Group: group}
	}
	off := func(name, group string) InjectRule {
		return InjectRule{Enabled: false, In: "header", Name: name, Group: group}
	}

	cases := []struct {
		name  string
		rules []InjectRule
		want  string
	}{
		{"ungrouped rules combine freely", []InjectRule{on("a", ""), off("b", ""), on("c", "")}, "a,c"},
		{"a group is all or nothing", []InjectRule{on("a", "g"), off("b", "g"), on("solo", "")}, "solo"},
		{"group names ignore case and spaces", []InjectRule{on("a", "Beta"), on("b", " beta "), off("c", "BETA")}, ""},
		{"a member with no name holds its group", []InjectRule{on("", "g"), on("a", "g"), on("solo", "")}, "solo"},
		{"a complete group is sent", []InjectRule{on("a", "g"), on("b", "g")}, "a,b"},
	}
	for _, c := range cases {
		var names []string
		for _, r := range activeRules(c.rules) {
			names = append(names, r.Name)
		}
		if got := strings.Join(names, ","); got != c.want {
			t.Errorf("%s: active = %q, want %q", c.name, got, c.want)
		}
	}
}

// Saving used to drop rules with no name. For a grouped rule that would
// silently shrink the group, so such rules must survive normalising.
func TestNormalizeKeepsGroupedRuleWithBlankName(t *testing.T) {
	got := normalizeRules([]InjectRule{
		{Enabled: true, Name: "", Group: "pair"}, // kept: it holds its group
		{Enabled: true, Name: "", Group: ""},     // dropped: nothing in it
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
	add := func(id string, versionOn bool) {
		app.store.AddProvider(&Provider{ID: id, Name: id, BaseURL: upstream.URL, Enabled: true, Strategy: "round_robin",
			Inject: []InjectRule{
				{Enabled: true, In: "header", Name: "x-api-key", Source: "key", Group: "pair"},
				{Enabled: versionOn, In: "header", Name: "x-version", Source: "static", Value: "2024-02-01", Group: "pair"},
			},
			Keys: []*Key{{ID: id + "-k", Value: "secret-" + id, Enabled: true, Status: statusOK}},
		})
	}
	add("held", false)
	add("sent", true)

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
	if seen.Get("x-api-key") != "secret-sent" || seen.Get("x-version") != "2024-02-01" {
		t.Errorf("complete group not sent together: x-api-key=%q x-version=%q", seen.Get("x-api-key"), seen.Get("x-version"))
	}
}
