package headroom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

// Route proxy: one tiny reverse-proxy. Headroom compresses, then posts here;
// we pick the real host by API key.

const (
	routeProxyReadyTimeout = 3 * time.Second
	routeProxyStopTimeout  = 2 * time.Second
	routeProxyServeArg     = "__route-proxy-serve"
)

var (
	routeServerMu sync.Mutex
	routeListener net.Listener
	routeServer   *http.Server
	routeProxyInline bool
)

func routeProxyPort() int {
	return ProxyPort() + 1
}

// RouteProxyURL is the loopback base headroom should use as both openai and
// anthropic upstream when BYOK routes exist.
func RouteProxyURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(routeProxyPort())
}

func routeProxyPidFile() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "proxy.route.pid")
}

// StartRouteProxy ensures the key→host router is listening.
func StartRouteProxy() error {
	if routeProxyLive() {
		return nil
	}
	_ = LoadRouteMap()
	if routeProxyInline {
		return startRouteProxyInline()
	}
	if err := spawnRouteProxyDaemon(); err != nil {
		return startRouteProxyInline()
	}
	deadline := time.Now().Add(routeProxyReadyTimeout)
	for time.Now().Before(deadline) {
		if routeProxyLive() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return startRouteProxyInline()
}

func spawnRouteProxyDaemon() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	if util.IsGoTestExecutable(self) {
		return fmt.Errorf("route proxy: refuse test binary spawn")
	}
	logPath := filepath.Join(util.HeadroomPathsResolved().Root, "proxy.route.log")
	_ = util.EnsureDir(filepath.Dir(logPath))
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(self, routeProxyServeArg)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = os.Environ()
	if err := spawnDetached(cmd); err != nil {
		_ = log.Close()
		return err
	}
	_ = log.Close()
	return nil
}

func startRouteProxyInline() error {
	routeServerMu.Lock()
	defer routeServerMu.Unlock()
	if routeListener != nil {
		return nil
	}
	if routeProxyLive() {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(routeProxyPort()))
	if err != nil {
		if routeProxyLive() {
			return nil
		}
		return fmt.Errorf("route proxy listen: %w", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(serveRoute), ReadHeaderTimeout: 10 * time.Second}
	routeListener = ln
	routeServer = srv
	go func() { _ = srv.Serve(ln) }()
	deadline := time.Now().Add(routeProxyReadyTimeout)
	for time.Now().Before(deadline) {
		if routeProxyLive() {
			_ = os.WriteFile(routeProxyPidFile(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = srv.Close()
	routeListener, routeServer = nil, nil
	return fmt.Errorf("route proxy did not become ready")
}

// RunRouteProxyServe is the detached worker entrypoint (tokless __route-proxy-serve).
func RunRouteProxyServe() int {
	_ = LoadRouteMap()
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(routeProxyPort()))
	if err != nil {
		if routeProxyLive() {
			return 0
		}
		fmt.Fprintln(os.Stderr, "route proxy listen:", err)
		return 1
	}
	srv := &http.Server{Handler: http.HandlerFunc(serveRoute), ReadHeaderTimeout: 10 * time.Second}
	_ = os.WriteFile(routeProxyPidFile(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
	err = srv.Serve(ln)
	_ = os.Remove(routeProxyPidFile())
	if err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "route proxy:", err)
		return 1
	}
	return 0
}

// StopRouteProxy shuts down the router (in-process owner or detached child).
func StopRouteProxy() error {
	routeServerMu.Lock()
	if routeServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), routeProxyStopTimeout)
		_ = routeServer.Shutdown(ctx)
		cancel()
		routeListener, routeServer = nil, nil
		routeServerMu.Unlock()
		_ = os.Remove(routeProxyPidFile())
		return nil
	}
	routeServerMu.Unlock()

	raw, ok := util.ReadFileSafe(routeProxyPidFile())
	if !ok {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || pid <= 0 {
		_ = os.Remove(routeProxyPidFile())
		return nil
	}
	if !processIdentitySupported() {
		proc, err := os.FindProcess(pid)
		if err == nil {
			_ = proc.Kill()
		}
		_ = os.Remove(routeProxyPidFile())
		return nil
	}
	identity, err := proxyIdentity(pid)
	if err != nil {
		_ = os.Remove(routeProxyPidFile())
		return nil
	}
	if len(identity.Args) < 1 || identity.Args[0] != routeProxyServeArg {
		if !routeProxyLive() {
			_ = os.Remove(routeProxyPidFile())
		}
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	_ = proxyKill(proc)
	deadline := time.Now().Add(routeProxyStopTimeout)
	for time.Now().Before(deadline) {
		if proxyGone(proc) && !routeProxyLive() {
			_ = os.Remove(routeProxyPidFile())
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = os.Remove(routeProxyPidFile())
	return nil
}

func routeProxyLive() bool {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get(RouteProxyURL() + "/livez")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Service string `json:"service"`
	}
	return json.NewDecoder(resp.Body).Decode(&body) == nil && body.Service == "tokless-route"
}

func serveRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/livez" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"tokless-route"}`))
		return
	}
	key := extractRouteKey(r.Header)
	base, ok := LookupRoute(key)
	if !ok || base == "" {
		http.Error(w, "no upstream route for credential", http.StatusBadGateway)
		return
	}
	target, err := joinUpstream(base, r.URL.Path, r.URL.RawQuery)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}
	proxy := httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = u
			req.Host = u.Host
			req.Header.Del("Proxy-Connection")
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			http.Error(rw, "upstream: "+err.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func extractRouteKey(h http.Header) string {
	if v := h.Get("Authorization"); v != "" {
		fields := strings.Fields(v)
		if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
			return fields[1]
		}
		if len(fields) == 1 {
			return fields[0]
		}
	}
	if v := h.Get("x-api-key"); v != "" {
		return v
	}
	if v := h.Get("api-key"); v != "" {
		return v
	}
	return ""
}

func isLocalRouteURL(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "127.0.0.1:"+strconv.Itoa(routeProxyPort())) ||
		strings.Contains(u, "localhost:"+strconv.Itoa(routeProxyPort()))
}

// joinUpstream builds the absolute upstream URL.
func joinUpstream(base, path, rawQuery string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", fmt.Errorf("empty base")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	out := base + path
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		out = base + path[len("/v1"):]
	} else if strings.HasSuffix(base, "/v1") && path == "/v1" {
		out = base
	}
	if rawQuery != "" {
		out += "?" + rawQuery
	}
	return out, nil
}

// drain for tests
func routeProxyDrain(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}
