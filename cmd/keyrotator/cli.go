package main

// cli.go implements the `keyrotator <command>` management CLI.
//
// The CLI is a thin client over the running server's admin API: changes are
// applied to the live process immediately and persisted, with no restart. It
// finds the server (and the admin token) from the same config file the server
// uses, so no extra setup is needed.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type cliOpts struct {
	config string
	addr   string
	token  string
	json   bool
	help   bool
}

type cliClient struct {
	base      string
	proxyBase string
	token     string
	json      bool
	httpc     *http.Client
	cfgPath   string // config file the server uses; start/stop keep their files beside it
}

// cliCommands is the set of leading words that select the management CLI.
var cliCommands = map[string]bool{
	"help": true, "list": true, "ls": true, "providers": true, "show": true,
	"add": true, "rm": true, "remove": true, "delete": true,
	"on": true, "enable": true, "off": true, "disable": true,
	"url": true, "keys": true, "add-key": true, "rm-key": true,
	"key-on": true, "key-off": true, "test": true, "token": true, "settings": true, "alias": true,
	"tui": true, "start": true, "stop": true, "status": true,
}

// valueFlags are the flags that consume the following argument unless they are
// written as --flag=value. The union of server and CLI flags is listed.
var valueFlags = map[string]bool{
	"config": true, "c": true, "listen": true, "service": true,
	"addr": true, "a": true, "admin-token": true,
}

// wantsCLI reports whether the arguments name a management command rather than
// starting the server. Global flags may appear before the command.
func wantsCLI(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			return cliCommands[strings.ToLower(a)]
		}
		name := strings.TrimLeft(a, "-")
		if strings.IndexByte(name, '=') >= 0 {
			continue // the value is inline
		}
		if valueFlags[name] {
			i++ // skip the flag's value
		}
	}
	return false
}

// runCLI handles `keyrotator <command> ...`. It returns a process exit code.
func runCLI(args []string) int {
	o, pos, flags, err := parseCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keyrotator: "+err.Error())
		return 2
	}
	if o.help {
		printCLIHelp()
		return 0
	}
	if len(pos) == 0 {
		printCLIHelp()
		return 2
	}

	cmd := strings.ToLower(pos[0])
	rest := pos[1:]
	if cmd == "help" {
		printCLIHelp()
		return 0
	}

	c, err := newCLIClient(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keyrotator: "+err.Error())
		return 1
	}
	if code := c.dispatch(cmd, rest, flags); code >= 0 {
		return code
	}
	fmt.Fprintf(os.Stderr, "keyrotator: unknown command %q\n\n", cmd)
	printCLIHelp()
	return 2
}

// dispatch runs one command. It returns -1 when the command is not recognised.
func (c *cliClient) dispatch(cmd string, args []string, flags map[string]string) int {
	switch cmd {
	case "list", "ls", "providers":
		return c.cmdList()
	case "show":
		return c.cmdShow(args)
	case "add":
		return c.cmdAdd(args, flags)
	case "rm", "remove", "delete":
		return c.cmdRemove(args)
	case "on", "enable":
		return c.cmdToggle(args, true)
	case "off", "disable":
		return c.cmdToggle(args, false)
	case "url":
		return c.cmdURL(args)
	case "keys":
		return c.cmdKeys(args)
	case "add-key":
		return c.cmdAddKey(args, flags)
	case "rm-key":
		return c.cmdRemoveKey(args)
	case "key-on":
		return c.cmdToggleKey(args, true)
	case "key-off":
		return c.cmdToggleKey(args, false)
	case "test":
		return c.cmdTest(args)
	case "token":
		return c.cmdToken(args)
	case "settings":
		return c.cmdSettings()
	case "alias":
		return c.cmdAlias(args)
	case "tui":
		return runTUI(c)
	case "start":
		return c.cmdStart()
	case "stop":
		return c.cmdStop()
	case "status":
		return c.cmdStatus()
	}
	return -1
}

// ---------- plumbing ----------

func parseCLI(args []string) (*cliOpts, []string, map[string]string, error) {
	o := &cliOpts{}
	pos := []string{}
	flags := map[string]string{}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		val, hasVal := "", false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			val, name, hasVal = name[eq+1:], name[:eq], true
		}
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag -%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "config", "c":
			v, err := value()
			if err != nil {
				return nil, nil, nil, err
			}
			o.config = v
		case "addr", "a":
			v, err := value()
			if err != nil {
				return nil, nil, nil, err
			}
			o.addr = v
		case "admin-token":
			v, err := value()
			if err != nil {
				return nil, nil, nil, err
			}
			o.token = v
		case "json":
			o.json = true
		case "h", "help":
			o.help = true
		default:
			v, err := value()
			if err != nil {
				return nil, nil, nil, err
			}
			flags[name] = v
		}
	}
	return o, pos, flags, nil
}

func newCLIClient(o *cliOpts) (*cliClient, error) {
	path := o.config
	if path == "" {
		path = defaultConfigPath()
	}
	cfg := readConfigQuiet(path)

	addr := strings.TrimSpace(o.addr)
	if addr == "" && cfg != nil {
		addr = cfg.Listen
	}
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	token := strings.TrimSpace(o.token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("KEYROTATOR_ADMIN_TOKEN"))
	}
	if token == "" && cfg != nil {
		token = cfg.AdminToken
	}
	return &cliClient{
		cfgPath: path,
		base:    httpBase(addr),
		token:   token,
		json:    o.json,
		httpc:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// readConfigQuiet parses the config if present. It never creates or writes one.
func readConfigQuiet(path string) *Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	cfg := &Config{}
	if json.Unmarshal(data, cfg) != nil {
		return nil
	}
	return cfg
}

func httpBase(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, "8787"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// clientFacing rewrites a bind address such as 0.0.0.0:8787 into a usable URL.
func clientFacing(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch u.Hostname() {
	case "", "0.0.0.0", "::":
		u.Host = net.JoinHostPort("127.0.0.1", u.Port())
	}
	return u.String()
}

func (c *cliClient) call(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Admin-Token", c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach keyrotator at %s (is it running?)", c.base)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(data))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		if resp.StatusCode == 401 {
			msg += " - set the admin token with -admin-token or KEYROTATOR_ADMIN_TOKEN"
		}
		return fmt.Errorf("%s", msg)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *cliClient) state() (*stateResp, error) {
	var st stateResp
	if err := c.call(http.MethodGet, "/api/state", nil, &st); err != nil {
		return nil, err
	}
	c.proxyBase = st.ProxyBase
	return &st, nil
}

// pbase is the /p/ namespace root, as the server reports it.
func (c *cliClient) pbase() string {
	if c.proxyBase != "" {
		return clientFacing(c.proxyBase)
	}
	return c.base + "/p/"
}

func (c *cliClient) resolve(st *stateResp, ref string) (*Provider, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("a provider is required")
	}
	for _, p := range st.Providers {
		if p.ID == ref {
			return p, nil
		}
	}
	var matches []*Provider
	for _, p := range st.Providers {
		if strings.EqualFold(p.Name, ref) || strings.HasPrefix(p.ID, ref) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no provider matches %q", ref)
	case 1:
		return matches[0], nil
	default:
		var names []string
		for _, p := range matches {
			names = append(names, fmt.Sprintf("%s (%s)", p.Name, p.ID))
		}
		sort.Strings(names)
		return nil, fmt.Errorf("%q matches several providers: %s", ref, strings.Join(names, ", "))
	}
}

func (c *cliClient) withProvider(ref string, fn func(*stateResp, *Provider) int) int {
	st, err := c.state()
	if err != nil {
		return fail(err)
	}
	p, err := c.resolve(st, ref)
	if err != nil {
		return fail(err)
	}
	return fn(st, p)
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "keyrotator: "+err.Error())
	return 1
}

func ok(msg string) int {
	fmt.Println(msg)
	return 0
}

func need(args []string, n int, usage string) bool {
	if len(args) < n {
		fmt.Fprintln(os.Stderr, "usage: keyrotator "+usage)
		return false
	}
	return true
}

// parseRate accepts a positive number, "0" or "off"/"none" for unlimited.
func parseRate(s string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off", "none", "unlimited":
		return 0, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid rate %q", s)
	}
	return v, nil
}

func rpsLabel(v float64) string {
	if v <= 0 {
		return "-"
	}
	return fmtRPS(v) + "/s"
}

func newTable() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

// ---------- commands ----------

func (c *cliClient) cmdList() int {
	st, err := c.state()
	if err != nil {
		return fail(err)
	}
	if c.json {
		return printJSON(st)
	}
	if len(st.Providers) == 0 {
		return ok("no providers yet - add one with: keyrotator add <name> <base-url>")
	}
	w := newTable()
	fmt.Fprintln(w, "NAME\tID\tSTATE\tKEYS\tHEALTHY\tFIELDS\tRATE\tPER-KEY")
	for _, p := range st.Providers {
		healthy, total := 0, 0
		for _, k := range p.Keys {
			total++
			if k.Enabled && k.Status == statusOK {
				healthy++
			}
		}
		state := "disabled"
		if p.Enabled {
			state = "enabled"
		}
		fields := 0
		for _, r := range p.Inject {
			if r.Enabled {
				fields++
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d/%d\t%d\t%s\t%s\n",
			p.Name, p.ID, state, total, healthy, total, fields,
			rpsLabel(effectiveProviderRPS(p, st.RateLimitRPS)),
			rpsLabel(effectiveKeyRPS(p, st.KeyRateLimitRPS)))
	}
	return flush(w)
}

func (c *cliClient) cmdShow(args []string) int {
	if !need(args, 1, "show <provider>") {
		return 2
	}
	return c.withProvider(args[0], func(st *stateResp, p *Provider) int {
		if c.json {
			return printJSON(p)
		}
		w := newTable()
		fmt.Fprintf(w, "name\t%s\n", p.Name)
		fmt.Fprintf(w, "id\t%s\n", p.ID)
		fmt.Fprintf(w, "base url\t%s\n", p.BaseURL)
		fmt.Fprintf(w, "state\t%s\n", map[bool]string{true: "enabled", false: "disabled"}[p.Enabled])
		fmt.Fprintf(w, "strategy\t%s\n", p.Strategy)
		fmt.Fprintf(w, "health path\t%s\n", p.HealthPath)
		fmt.Fprintf(w, "provider rate\t%s\n", rpsLabel(effectiveProviderRPS(p, st.RateLimitRPS)))
		fmt.Fprintf(w, "per-key rate\t%s\n", rpsLabel(effectiveKeyRPS(p, st.KeyRateLimitRPS)))
		fmt.Fprintf(w, "proxy token\t%s\n", maskSecret(effectiveProxyToken(p, st.ProxyToken)))
		_ = w.Flush()

		fmt.Println("\nfields:")
		fw := newTable()
		fmt.Fprintln(fw, "  IN\tNAME\tSOURCE\tVALUE\tPREFIX\tSUFFIX\tPOOL\tGROUP")
		for _, r := range p.Inject {
			src, val, pool := "rotating key", "", poolKey(r.Pool)
			if r.Source == "static" {
				src, val, pool = "fixed", r.Value, ""
			}
			mark := "  "
			if !r.Enabled {
				mark = "x "
			}
			fmt.Fprintf(fw, "%s%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				mark, r.In, r.Name, src, val, r.Prefix, r.Suffix, pool, r.Group)
		}
		_ = fw.Flush()

		fmt.Printf("\nkeys (%d):\n", len(p.Keys))
		if len(p.Keys) == 0 {
			fmt.Println("  none - add one with: keyrotator add-key " + p.ID + " <secret>")
			return 0
		}
		kw := newTable()
		fmt.Fprintln(kw, "  ID\tLABEL\tPOOL\tSTATUS\tOK\tERR\tLAST USED")
		for _, k := range p.Keys {
			status := k.Status
			if !k.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(kw, "  %s\t%s\t%s\t%s\t%d\t%d\t%s\n",
				k.ID, k.Label, poolKey(k.Pool), status, k.Success, k.Errors, ago(k.LastUsed))
		}
		return flush(kw)
	})
}

func (c *cliClient) cmdAdd(args []string, flags map[string]string) int {
	if !need(args, 2, "add <name> <base-url> [--strategy round_robin|random|least_used] [--health /models] [--provider-rps N] [--key-rps N] [--token T]") {
		return 2
	}
	body := map[string]any{"name": args[0], "baseUrl": args[1]}
	if v := flags["strategy"]; v != "" {
		body["strategy"] = v
	}
	if v := flags["health"]; v != "" {
		body["healthPath"] = v
	}
	if v, set := flags["provider-rps"]; set {
		f, err := parseRate(v)
		if err != nil {
			return fail(err)
		}
		body["rateLimitRPS"] = f
	}
	if v, set := flags["key-rps"]; set {
		f, err := parseRate(v)
		if err != nil {
			return fail(err)
		}
		body["keyRateLimitRPS"] = f
	}
	if v, set := flags["token"]; set {
		body["proxyToken"] = v
	}
	var out struct {
		Provider *Provider `json:"provider"`
	}
	if err := c.call(http.MethodPost, "/api/providers", body, &out); err != nil {
		return fail(err)
	}
	if c.json {
		return printJSON(out.Provider)
	}
	if out.Provider == nil {
		return ok("provider created")
	}
	// Re-read state so the printed URL reflects any global proxy token.
	if st, err := c.state(); err == nil {
		if p, err := c.resolve(st, out.Provider.ID); err == nil {
			return ok(fmt.Sprintf("created provider %s (%s)\n  proxy URL: %s",
				p.Name, p.ID, c.proxyURL(p, st.ProxyToken)))
		}
	}
	return ok(fmt.Sprintf("created provider %s (%s)", out.Provider.Name, out.Provider.ID))
}

func (c *cliClient) cmdRemove(args []string) int {
	if !need(args, 1, "rm <provider>") {
		return 2
	}
	return c.withProvider(args[0], func(_ *stateResp, p *Provider) int {
		if err := c.call(http.MethodDelete, "/api/providers/"+p.ID, nil, nil); err != nil {
			return fail(err)
		}
		return ok("deleted provider " + p.Name + " (" + p.ID + ")")
	})
}

func (c *cliClient) cmdToggle(args []string, enable bool) int {
	if !need(args, 1, "on|off <provider>") {
		return 2
	}
	return c.withProvider(args[0], func(_ *stateResp, p *Provider) int {
		if err := c.call(http.MethodPut, "/api/providers/"+p.ID, map[string]any{"enabled": enable}, nil); err != nil {
			return fail(err)
		}
		verb := "disabled"
		if enable {
			verb = "enabled"
		}
		return ok(verb + " provider " + p.Name)
	})
}

func (c *cliClient) cmdURL(args []string) int {
	if !need(args, 1, "url <provider>") {
		return 2
	}
	return c.withProvider(args[0], func(st *stateResp, p *Provider) int {
		u := c.proxyURL(p, st.ProxyToken)
		if c.json {
			return printJSON(map[string]any{
				"provider": p.ID, "url": u,
				"token":  effectiveProxyToken(p, st.ProxyToken),
				"direct": clientFacing(st.ProxyBase) + p.ID + "/",
			})
		}
		fmt.Println(u)
		return 0
	})
}

// proxyURL builds the authenticated base URL for a provider.
func (c *cliClient) proxyURL(p *Provider, globalToken string) string {
	u := c.pbase() + p.ID + "/"
	if tok := effectiveProxyToken(p, globalToken); tok != "" {
		u += tok + "/"
	}
	return u
}

func (c *cliClient) cmdKeys(args []string) int {
	if !need(args, 1, "keys <provider>") {
		return 2
	}
	return c.withProvider(args[0], func(_ *stateResp, p *Provider) int {
		if c.json {
			return printJSON(p.Keys)
		}
		if len(p.Keys) == 0 {
			return ok("no keys - add one with: keyrotator add-key " + p.ID + " <secret>")
		}
		w := newTable()
		fmt.Fprintln(w, "ID\tLABEL\tPOOL\tSTATE\tSTATUS\tOK\tERR\tLAST USED")
		for _, k := range p.Keys {
			state := "enabled"
			if !k.Enabled {
				state = "disabled"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
				k.ID, k.Label, poolKey(k.Pool), state, k.Status, k.Success, k.Errors, ago(k.LastUsed))
		}
		return flush(w)
	})
}

func (c *cliClient) cmdAddKey(args []string, flags map[string]string) int {
	if !need(args, 2, "add-key <provider> <secret> [--label L] [--pool P]") {
		return 2
	}
	return c.withProvider(args[0], func(_ *stateResp, p *Provider) int {
		body := map[string]any{"value": args[1]}
		if l := flags["label"]; l != "" {
			body["label"] = l
		}
		if pool := flags["pool"]; pool != "" {
			body["pool"] = pool
		}
		var out struct {
			Key *Key `json:"key"`
		}
		if err := c.call(http.MethodPost, "/api/providers/"+p.ID+"/keys", body, &out); err != nil {
			return fail(err)
		}
		if c.json {
			return printJSON(out.Key)
		}
		if out.Key != nil {
			return ok(fmt.Sprintf("added key %s (%s) to %s", out.Key.Masked(), out.Key.ID, p.Name))
		}
		return ok("key added")
	})
}

func (c *cliClient) cmdRemoveKey(args []string) int {
	if !need(args, 2, "rm-key <provider> <key-id>") {
		return 2
	}
	return c.withProvider(args[0], func(st *stateResp, p *Provider) int {
		kid, err := c.resolveKey(st, p, args[1])
		if err != nil {
			return fail(err)
		}
		if err := c.call(http.MethodDelete, "/api/providers/"+p.ID+"/keys/"+kid.ID, nil, nil); err != nil {
			return fail(err)
		}
		return ok("deleted key " + kid.Label + " (" + kid.ID + ")")
	})
}

func (c *cliClient) cmdToggleKey(args []string, enable bool) int {
	if !need(args, 2, "key-on|key-off <provider> <key-id>") {
		return 2
	}
	return c.withProvider(args[0], func(st *stateResp, p *Provider) int {
		kid, err := c.resolveKey(st, p, args[1])
		if err != nil {
			return fail(err)
		}
		if err := c.call(http.MethodPut, "/api/providers/"+p.ID+"/keys/"+kid.ID,
			map[string]any{"enabled": enable}, nil); err != nil {
			return fail(err)
		}
		verb := "disabled"
		if enable {
			verb = "enabled"
		}
		return ok(verb + " key " + kid.ID)
	})
}

// resolveKey accepts a key id or an exact label.
func (c *cliClient) resolveKey(_ *stateResp, p *Provider, ref string) (*Key, error) {
	for _, k := range p.Keys {
		if k.ID == ref {
			return k, nil
		}
	}
	var match *Key
	for _, k := range p.Keys {
		if strings.EqualFold(k.Label, ref) {
			if match != nil {
				return nil, fmt.Errorf("key label %q is not unique; use the key id", ref)
			}
			match = k
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no key matches %q on provider %s", ref, p.Name)
	}
	return match, nil
}

func (c *cliClient) cmdTest(args []string) int {
	if len(args) == 0 {
		st, err := c.state()
		if err != nil {
			return fail(err)
		}
		bad := 0
		for _, p := range st.Providers {
			bad += c.testOne(p)
		}
		if bad > 0 {
			fmt.Fprintf(os.Stderr, "%d key(s) failed\n", bad)
			return 1
		}
		return 0
	}
	return c.withProvider(args[0], func(_ *stateResp, p *Provider) int {
		if bad := c.testOne(p); bad > 0 {
			fmt.Fprintf(os.Stderr, "%d key(s) failed\n", bad)
			return 1
		}
		return 0
	})
}

func (c *cliClient) testOne(p *Provider) int {
	var out struct {
		Results []struct {
			Label   string `json:"label"`
			ID      string `json:"id"`
			Code    int    `json:"code"`
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		} `json:"results"`
	}
	if err := c.call(http.MethodPost, "/api/providers/"+p.ID+"/test", nil, &out); err != nil {
		return fail(err)
	}
	if c.json {
		_ = printJSON(out.Results)
		return 0
	}
	if len(out.Results) == 0 {
		fmt.Printf("%s: no keys to test\n", p.Name)
		return 0
	}
	bad := 0
	fmt.Printf("%s:\n", p.Name)
	w := newTable()
	for _, r := range out.Results {
		verdict := "ok"
		if !r.OK {
			verdict = "FAILED"
			bad++
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%d\n", r.Label, r.ID, verdict, r.Code)
	}
	_ = w.Flush()
	return bad
}

func (c *cliClient) cmdToken(args []string) int {
	sub := "show"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "show", "":
		st, err := c.state()
		if err != nil {
			return fail(err)
		}
		if c.json {
			return printJSON(map[string]any{"proxyToken": st.ProxyToken})
		}
		if st.ProxyToken == "" {
			return ok("no proxy token set - proxy URLs are open to anyone who can reach " + st.Addr)
		}
		fmt.Printf("proxy token: %s\n", st.ProxyToken)
		return 0
	case "gen", "generate":
		v := RandomToken()
		if err := c.setProxyToken(v); err != nil {
			return fail(err)
		}
		return ok("proxy token set to " + v)
	case "set":
		if !need(args, 2, "token set <value>") {
			return 2
		}
		if err := c.setProxyToken(args[1]); err != nil {
			return fail(err)
		}
		return ok("proxy token set")
	case "clear", "off", "none":
		if err := c.setProxyToken(""); err != nil {
			return fail(err)
		}
		return ok("proxy token cleared - proxy URLs are open again")
	}
	fmt.Fprintln(os.Stderr, "usage: keyrotator token [show|gen|set <value>|clear]")
	return 2
}

func (c *cliClient) setProxyToken(v string) error {
	return c.call(http.MethodPost, "/api/settings", map[string]any{"proxyToken": v}, nil)
}

func (c *cliClient) cmdSettings() int {
	st, err := c.state()
	if err != nil {
		return fail(err)
	}
	if c.json {
		return printJSON(map[string]any{
			"addr": st.Addr, "configPath": st.ConfigPath,
			"maxRetries": st.MaxRetries, "cooldownSec": st.CooldownSec,
			"adminTokenSet":   st.AdminTokenSet,
			"proxyTokenSet":   st.ProxyToken != "",
			"rateLimitRPS":    st.RateLimitRPS,
			"keyRateLimitRPS": st.KeyRateLimitRPS,
		})
	}
	fmt.Printf("config file    %s\n", st.ConfigPath)
	fmt.Printf("listen         %s\n", st.Addr)
	fmt.Printf("max retries    %d\n", st.MaxRetries)
	fmt.Printf("cooldown       %ds\n", st.CooldownSec)
	fmt.Printf("provider rate  %s\n", rpsLabel(st.RateLimitRPS))
	fmt.Printf("per-key rate   %s\n", rpsLabel(st.KeyRateLimitRPS))
	fmt.Printf("admin token    %s\n", map[bool]string{true: "set", false: "not set"}[st.AdminTokenSet])
	fmt.Printf("proxy token    %s\n", maskSecret(st.ProxyToken))
	return 0
}

// ---------- output helpers ----------

func flush(w *tabwriter.Writer) int {
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	return 0
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(err)
	}
	return 0
}

func ago(ts int64) string {
	if ts == 0 {
		return "-"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}

func maskSecret(s string) string {
	if s == "" {
		return "(none)"
	}
	if len(s) <= 6 {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + strings.Repeat("*", 6) + s[len(s)-3:]
}

func printCLIHelp() {
	fmt.Print(`keyrotator - manage a running keyrotator instance

usage:
  keyrotator <command> [arguments] [flags]

commands:
  list                            list providers (also: ls, providers)
  show <provider>                 everything about one provider
  add <name> <base-url>           add a provider
                                    --strategy round_robin|random|least_used
                                    --health /models
                                    --provider-rps N    0 or "off" = unlimited
                                    --key-rps N         0 or "off" = unlimited
                                    --token SECRET      per-provider proxy token
  rm <provider>                   delete a provider (also: remove, delete)
  on <provider> | off <provider>  enable / disable a provider
  url <provider>                  print the authenticated proxy URL
  keys <provider>                 list a provider's keys
  add-key <provider> <secret>     add a key            [--label ALIAS] [--pool POOL]
  alias <provider> <key> <name>   rename a key (by id or alias)
  rm-key <provider> <key-id>      delete a key
  key-on <provider> <key-id>      enable a key   (key-off to disable)
  test [provider]                 probe keys against the upstream health path
  token                           show the proxy token (see also: token gen,
                                  token set <value>, token clear)
  settings                        show global settings
  tui                             full-screen terminal dashboard (q to quit)
  start                           run in the background (survives closing the terminal)
  stop                            stop the background server
  status                          show whether it is running

  A <provider> may be given as its id, its name, or an unambiguous id prefix.

global flags:
  -config PATH       config file (default: KeyRotator/config.json in the OS user config dir)
  -addr ADDR         admin API address (default: the config's listen address)
  -admin-token T     admin token (default: from the config, or
                     KEYROTATOR_ADMIN_TOKEN)
  -json              print raw JSON
  -h, -help          this text

Running the server itself is unchanged: start it with no command
(see keyrotator -h), and use the commands above to manage it live.
`)
}

// cmdAlias renames a key. The key can be referenced by id or by its current alias.
func (c *cliClient) cmdAlias(args []string) int {
	if !need(args, 3, "alias <provider> <key> <new-alias>") {
		return 2
	}
	return c.withProvider(args[0], func(st *stateResp, p *Provider) int {
		kid, err := c.resolveKey(st, p, args[1])
		if err != nil {
			return fail(err)
		}
		alias := strings.Join(args[2:], " ")
		if err := c.call(http.MethodPut, "/api/providers/"+p.ID+"/keys/"+kid.ID,
			map[string]any{"label": alias}, nil); err != nil {
			return fail(err)
		}
		return ok("renamed key " + kid.ID + " to " + alias)
	})
}
