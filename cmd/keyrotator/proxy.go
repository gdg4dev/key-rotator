package main

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxRequestBody = 64 << 20 // 64 MiB

var hopHeaders = map[string]bool{
	"Connection":          true,
	"Proxy-Connection":    true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// handleProxy serves /p/{providerID}/{...} and forwards to the upstream API,
// injecting a rotating API key.
func (a *App) handleProxy(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/p/")
	parts := strings.SplitN(rest, "/", 2)
	pid := strings.TrimSpace(parts[0])
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	p := a.store.Provider(pid)
	if p == nil || !p.Enabled {
		writeErr(w, http.StatusNotFound, "unknown or disabled provider: "+pid)
		return
	}

	st := a.store.Settings()

	// Proxy authentication. The provider's own token wins; otherwise the
	// global one applies. No token configured means the URL is open.
	required := effectiveProxyToken(p, st.ProxyToken)
	if required != "" {
		// Preferred form: the token is a path segment, so the generated URL can
		// be used verbatim as a base URL (/p/<provider>/<token>/<path>).
		if stripped, viaPath := stripPathToken(sub, required); viaPath {
			sub = stripped
		} else if ok, viaQuery := authorizedProxy(r, required); ok {
			if viaQuery {
				stripTokenQuery(r)
			}
		} else {
			writeErr(w, http.StatusUnauthorized, "missing or invalid proxy token")
			return
		}
	}

	// Provider-wide rate limit: one token per proxied request.
	provRPS := effectiveProviderRPS(p, st.RateLimitRPS)
	if allowed, waitTime := a.limits.take("p:"+pid, provRPS); !allowed {
		retryAfter(w, waitTime)
		a.metrics.rateLimited(pid, "provider")
		a.metrics.request(pid, http.StatusTooManyRequests, time.Since(started))
		a.logs.Add(LogEntry{
			Time: time.Now().Unix(), Provider: pid, Method: r.Method,
			Path: r.URL.Path, Code: http.StatusTooManyRequests, OK: false,
			Msg: "provider rate limit (" + fmtRPS(provRPS) + "/s)",
		})
		writeErr(w, http.StatusTooManyRequests, "rate limit exceeded for provider "+pid+
			": limited to "+fmtRPS(provRPS)+" req/s")
		return
	}
	keyRPS := effectiveKeyRPS(p, st.KeyRateLimitRPS)

	body, err := readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "reading request body: "+err.Error())
		return
	}

	base := strings.TrimRight(p.BaseURL, "/")
	target := base
	if sub != "" {
		target = base + "/" + sub
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	maxRetries, cooldown := st.MaxRetries, st.CooldownSec
	pin := r.Header.Get("X-Rotator-Key")
	start := time.Now()

	var resp *http.Response
	var used []*Key
	var lastErr string
	unreachable := false // the last attempt never got an answer from the API

	for attempt := 0; attempt < maxRetries; attempt++ {
		keys, throttled := p.PickKeys(pin, func(k *Key) bool {
			return a.limits.allow("k:"+k.ID, keyRPS)
		})
		if keys == nil {
			if throttled {
				retryAfter(w, p.shortestKeyWait(keyRPS, a.limits))
				a.metrics.rateLimited(pid, "key")
				a.metrics.request(pid, http.StatusTooManyRequests, time.Since(started))
				a.logs.Add(LogEntry{
					Time: time.Now().Unix(), Provider: pid, Method: r.Method,
					Path: r.URL.Path, Code: http.StatusTooManyRequests, OK: false,
					Msg: "per-key rate limit (" + fmtRPS(keyRPS) + "/s each)",
				})
				writeErr(w, http.StatusTooManyRequests, "every key for provider "+pid+
					" is at its per-key rate limit ("+fmtRPS(keyRPS)+" req/s each)")
				return
			}
			writeErr(w, http.StatusServiceUnavailable, "no usable API keys configured for provider "+pid)
			return
		}

		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		copyHeaders(req.Header, r.Header)
		applyInjection(p, keyFinder(keys), req)
		req.Header.Del("X-Rotator-Key")
		req.Header.Del("X-Rotator-Token")
		req.Header.Del("Content-Length")
		if len(body) > 0 {
			req.ContentLength = int64(len(body))
		}

		resp, err = a.client.Do(req)
		if err != nil {
			// No answer at all (API down, wrong address, timeout). That says
			// nothing about the keys, so they are not marked.
			lastErr, unreachable = transportError(err), true
			a.metrics.retry(pid)
			resp = nil
			continue
		}

		code := resp.StatusCode
		if retryable(code) {
			wait := cooldownFor(resp.Header, cooldown)
			if attempt < maxRetries-1 {
				snippet := peek(resp.Body)
				resp.Body.Close()
				p.markFailAll(keys, code, snippet, wait)
				lastErr, unreachable = fmt.Sprintf("%d %s", code, snippet), false
				a.metrics.retry(pid)
				resp = nil
				continue
			}
			p.markFailAll(keys, code, http.StatusText(code), wait)
			used = keys
			break
		}

		for _, k := range keys {
			p.markOK(k)
		}
		used = keys
		break
	}

	elapsed := time.Since(start).Milliseconds()

	if resp == nil {
		msg := "all keys failed: " + lastErr
		if unreachable {
			msg = "could not reach " + base + ": " + lastErr
		}
		a.metrics.request(pid, http.StatusBadGateway, time.Since(started))
		a.logs.Add(LogEntry{
			Time: time.Now().Unix(), Provider: pid, Key: "", Method: r.Method,
			Path: "/" + sub, Code: http.StatusBadGateway, OK: false, Ms: elapsed,
			Msg: msg,
		})
		writeErr(w, http.StatusBadGateway, msg)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		if hopHeaders[k] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	setCORS(w)
	w.Header().Set("X-Rotator-Provider", p.ID)
	label, ids := keyLabels(used)
	if len(used) > 0 {
		w.Header().Set("X-Rotator-Key", label)
		w.Header().Set("X-Rotator-Key-Id", ids)
	}
	w.WriteHeader(resp.StatusCode)

	if _, err := copyStream(w, resp.Body); err != nil {
		// Client went away or upstream broke mid-stream; nothing to do.
		_ = err
	}

	a.metrics.request(pid, resp.StatusCode, time.Since(started))
	a.logs.Add(LogEntry{
		Time: time.Now().Unix(), Provider: pid, Key: label, Method: r.Method,
		Path: "/" + sub, Code: resp.StatusCode, OK: resp.StatusCode < 400,
		Ms: elapsed, Msg: http.StatusText(resp.StatusCode),
	})
}

// transportError describes a failed call to the API without its URL. The URL
// can carry a key in its query string, and this text reaches the client, the
// Activity log and the dashboard.
func transportError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return err.Error()
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		if hopHeaders[k] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	// Remove any header listed in Connection.
	for _, v := range src.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				dst.Del(tok)
			}
		}
	}
}

func copyStream(w http.ResponseWriter, r io.Reader) (int64, error) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

func peek(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 2048))
	return truncate(string(b), 300)
}

func setCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD")
	h.Set("Access-Control-Allow-Headers", "*")
	h.Set("Access-Control-Expose-Headers", "X-Rotator-Provider,X-Rotator-Key,X-Rotator-Key-Id")
	h.Set("Access-Control-Max-Age", "86400")
}

// authorizedProxy reports whether r carries the required proxy token and, if
// it arrived as "?token=", that the parameter must be stripped before the
// request is forwarded upstream.
func authorizedProxy(r *http.Request, required string) (ok bool, viaQuery bool) {
	match := func(got string) bool {
		return subtle.ConstantTimeCompare([]byte(got), []byte(required)) == 1
	}
	if h := r.Header.Get("X-Rotator-Token"); h != "" && match(h) {
		return true, false
	}
	if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
		if match(strings.TrimSpace(a[7:])) {
			return true, false
		}
	}
	if q := r.URL.Query().Get("token"); q != "" && match(q) {
		return true, true
	}
	return false, false
}

// stripPathToken removes a leading "<token>/" segment from sub when it matches
// the required token, returning the remaining upstream path.
func stripPathToken(sub, required string) (string, bool) {
	if required == "" || sub == "" {
		return sub, false
	}
	seg, rest := sub, ""
	if i := strings.IndexByte(sub, '/'); i >= 0 {
		seg, rest = sub[:i], sub[i+1:]
	}
	if subtle.ConstantTimeCompare([]byte(seg), []byte(required)) == 1 {
		return rest, true
	}
	return sub, false
}

// stripTokenQuery drops the auth token so it is never forwarded upstream.
func stripTokenQuery(r *http.Request) {
	q := r.URL.Query()
	q.Del("token")
	r.URL.RawQuery = q.Encode()
}

// retryAfter writes a Retry-After header of at least one second.
func retryAfter(w http.ResponseWriter, seconds float64) {
	n := int(math.Ceil(seconds))
	if n < 1 {
		n = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(n))
}

// fmtRPS renders a rate without a trailing ".0".
func fmtRPS(rps float64) string {
	if rps == math.Trunc(rps) {
		return strconv.FormatInt(int64(rps), 10)
	}
	return strconv.FormatFloat(rps, 'f', -1, 64)
}
