package headroom

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const byokRouteHeader = "X-Tokless-Route"

type BYOKRoute = util.BYOKRoute

type byokGatewayOwnership struct {
	PID        int      `json:"pid"`
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Start      string   `json:"start_fingerprint"`
}

type byokRouteTable struct {
	root       string
	generation uint64
	items      map[string]BYOKRoute
	byName     map[string]BYOKRoute
	proxies    map[string]*httputil.ReverseProxy
	proxyMu    sync.Mutex
}

var activeBYOKRoutes atomic.Pointer[byokRouteTable]

type reverseProxyBufferPool struct {
	pool sync.Pool
}

func newReverseProxyBufferPool() *reverseProxyBufferPool {
	return &reverseProxyBufferPool{
		pool: sync.Pool{
			New: func() any {
				b := make([]byte, 32*1024)
				return &b
			},
		},
	}
}

func (p *reverseProxyBufferPool) Get() []byte {
	return *p.pool.Get().(*[]byte)
}

func (p *reverseProxyBufferPool) Put(b []byte) {
	if cap(b) >= 32*1024 {
		p.pool.Put(&b)
	}
}

var byokBufferPool = newReverseProxyBufferPool()

var byokTransportFactory = func() http.RoundTripper {
	return &http.Transport{
		DialContext:           safeDialContext,
		Proxy:                 nil,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
}

var byokGatewayState struct {
	sync.Mutex
	server   *http.Server
	listener net.Listener
}

var byokGatewayLifecycle sync.Mutex
var byokLoadMu sync.Mutex
var byokReloadErr atomic.Pointer[string]

const byokGatewayServeArg = "__byok-gateway-serve"

func byokRoutesPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "byok.routes.json")
}

func byokGatewayPIDPath() string {
	return filepath.Join(util.HeadroomPathsResolved().Root, "byok.gateway.pid")
}

func byokGatewayArgs() []string { return []string{byokGatewayServeArg} }

func byokGatewayOwned() (byokGatewayOwnership, bool) {
	raw, ok := util.ReadFileSafe(byokGatewayPIDPath())
	if !ok {
		return byokGatewayOwnership{}, false
	}
	var record byokGatewayOwnership
	if json.Unmarshal([]byte(raw), &record) != nil || record.PID <= 0 || record.Executable == "" || record.Start == "" || len(record.Args) == 0 {
		return byokGatewayOwnership{}, false
	}
	identity, err := proxyIdentity(record.PID)
	if err != nil || identity.Start != record.Start || !identity.matches(record.Executable, record.Args) {
		return byokGatewayOwnership{}, false
	}
	return record, true
}

// persistByokGatewayOwnership records the running gateway process itself so
// ownership holds whether spawned by the CLI or supervised by a service manager.
func persistByokGatewayOwnership(identity processIdentityInfo) error {
	record := byokGatewayOwnership{PID: os.Getpid(), Executable: identity.Executable, Args: identity.Args, Start: identity.Start}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic(byokGatewayPIDPath(), string(b), 0o600)
}

func SaveBYOKRoutes(routes []BYOKRoute) error {
	if err := util.SaveBYOKRoutes(routes); err != nil {
		return err
	}
	return LoadBYOKRoutes()
}

func LoadBYOKRoutes() error {
	byokLoadMu.Lock()
	defer byokLoadMu.Unlock()

	currentRoot := util.HeadroomPathsResolved().Root
	routes, err := util.ReadBYOKRoutes()
	if err != nil {
		var gen uint64 = 1
		if old := activeBYOKRoutes.Load(); old != nil {
			gen = old.generation + 1
		}
		activeBYOKRoutes.Store(&byokRouteTable{
			root:       currentRoot,
			generation: gen,
			items:      nil,
			byName:     nil,
			proxies:    make(map[string]*httputil.ReverseProxy),
		})
		reloadErrStr := fmt.Sprintf("invalid BYOK route registry: %v", err)
		if os.IsNotExist(err) {
			reloadErrStr = "BYOK route registry not found"
		}
		byokReloadErr.Store(&reloadErrStr)
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("invalid BYOK route registry: %w", err)
	}
	items := make(map[string]BYOKRoute, len(routes))
	byName := make(map[string]BYOKRoute, len(routes))
	for _, route := range routes {
		route.ID = strings.TrimSpace(route.ID)
		route.Token = strings.TrimSpace(route.Token)
		route.Protocol = strings.TrimSpace(route.Protocol)
		route.Upstream = strings.TrimRight(strings.TrimSpace(route.Upstream), "/")
		if err := validateBYOKRoute(route); err != nil {
			var gen uint64 = 1
			if old := activeBYOKRoutes.Load(); old != nil {
				gen = old.generation + 1
			}
			activeBYOKRoutes.Store(&byokRouteTable{
				root:       currentRoot,
				generation: gen,
				items:      nil,
				byName:     nil,
				proxies:    make(map[string]*httputil.ReverseProxy),
			})
			reloadErrStr := fmt.Sprintf("route %q: %v", route.ID, err)
			byokReloadErr.Store(&reloadErrStr)
			return fmt.Errorf("route %q: %w", route.ID, err)
		}
		if items[route.ID].ID != "" {
			var gen uint64 = 1
			if old := activeBYOKRoutes.Load(); old != nil {
				gen = old.generation + 1
			}
			activeBYOKRoutes.Store(&byokRouteTable{
				root:       currentRoot,
				generation: gen,
				items:      nil,
				byName:     nil,
				proxies:    make(map[string]*httputil.ReverseProxy),
			})
			reloadErrStr := fmt.Sprintf("route %q: duplicate ID", route.ID)
			byokReloadErr.Store(&reloadErrStr)
			return fmt.Errorf("route %q: duplicate ID", route.ID)
		}
		items[route.ID] = route
		name := route.ID
		if idx := strings.Index(name, ":"); idx >= 0 {
			name = name[idx+1:]
		}
		if byName[name].ID == "" {
			byName[name] = route
		}
	}
	var gen uint64 = 1
	if old := activeBYOKRoutes.Load(); old != nil {
		gen = old.generation + 1
	}
	activeBYOKRoutes.Store(&byokRouteTable{
		root:       currentRoot,
		generation: gen,
		items:      items,
		byName:     byName,
		proxies:    make(map[string]*httputil.ReverseProxy),
	})
	byokReloadErr.Store(nil)
	return nil
}

// BYOKRoutesConfigured reports whether a valid route registry has entries.
func BYOKRoutesConfigured() bool {
	tbl := activeBYOKRoutes.Load()
	if tbl == nil {
		if err := LoadBYOKRoutes(); err != nil {
			return false
		}
		tbl = activeBYOKRoutes.Load()
	}
	return tbl != nil && len(tbl.items) > 0
}

func lookupBYOKRoute(id string) (BYOKRoute, bool) {
	tbl := activeBYOKRoutes.Load()
	if tbl == nil {
		if err := LoadBYOKRoutes(); err != nil {
			return BYOKRoute{}, false
		}
		tbl = activeBYOKRoutes.Load()
		if tbl == nil || tbl.items == nil {
			return BYOKRoute{}, false
		}
	}
	cleanID := strings.TrimSpace(id)
	route, ok := tbl.items[cleanID]
	if !ok {
		route, ok = tbl.byName[cleanID]
	}
	return route, ok
}

// BYOKGatewayURL is the loopback endpoint used by compatible BYOK clients.
func BYOKGatewayURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(byokGatewayPort())
}

func StartBYOKGateway() error {
	byokGatewayLifecycle.Lock()
	defer byokGatewayLifecycle.Unlock()
	byokGatewayState.Lock()
	localRunning := byokGatewayState.listener != nil
	byokGatewayState.Unlock()
	if localRunning {
		return nil
	}
	if _, owned := byokGatewayOwned(); owned && byokGatewayLive() {
		return nil
	}
	if err := LoadBYOKRoutes(); err != nil {
		return err
	}
	byokGatewayState.Lock()
	localRunning = byokGatewayState.listener != nil
	byokGatewayState.Unlock()
	if localRunning {
		return nil
	}
	if byokGatewayLive() {
		return fmt.Errorf("BYOK gateway port %d is occupied by an unowned process", byokGatewayPort())
	}
	self := util.ToklessAbs()
	cmd := exec.Command(self, byokGatewayServeArg)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := spawnDetached(cmd); err != nil {
		return fmt.Errorf("start BYOK gateway: %w", err)
	}
	if cmd.Process == nil {
		return fmt.Errorf("BYOK gateway started without a process")
	}
	identity, err := verifyIdentityWithRetry(cmd.Process.Pid, self, byokGatewayArgs())
	if err != nil {
		_ = proxyKill(cmd.Process)
		return err
	}
	record := byokGatewayOwnership{PID: cmd.Process.Pid, Executable: identity.Executable, Args: identity.Args, Start: identity.Start}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := util.WriteFileAtomic(byokGatewayPIDPath(), string(b), 0o600); err != nil {
		_ = proxyKill(cmd.Process)
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if byokGatewayLive() {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = os.Remove(byokGatewayPIDPath())
	_ = proxyKill(cmd.Process)
	return fmt.Errorf("BYOK gateway did not become ready")
}

func RunBYOKGatewayServe() error {
	if err := LoadBYOKRoutes(); err != nil {
		return err
	}
	coordinator, err := util.NewRoutingCoordinator(30 * time.Second)
	if err != nil {
		return fmt.Errorf("start routing coordinator: %w", err)
	}
	defer coordinator.Close()
	routes := configuredBYOKRoutes()
	if len(routes) == 0 {
		return fmt.Errorf("no BYOK routes configured")
	}
	laneListeners := make([]net.Listener, 0, len(routes))
	defer func() {
		for _, laneListener := range laneListeners {
			_ = laneListener.Close()
		}
	}()
	current, ok := coordinator.Current()
	if !ok {
		return fmt.Errorf("routing snapshot unavailable")
	}
	if currentState, err := util.ReadRoutingState(); err != nil {
		return fmt.Errorf("read routing state: %w", err)
	} else if currentState.State == util.RoutingStatePreparing {
		stopped := currentState
		stopped.Generation++
		stopped.PreviousGeneration = currentState.Generation
		stopped.State = util.RoutingStateStopped
		stopped.CommitNonce = ""
		for i := range stopped.Lanes {
			stopped.Lanes[i].Generation = stopped.Generation
			stopped.Lanes[i].Ready = false
		}
		for i := range stopped.Routes {
			stopped.Routes[i].Generation = stopped.Generation
			stopped.Routes[i].Revoked = true
			stopped.Routes[i].RevokedGeneration = stopped.Generation
		}
		if err := coordinator.Publish(stopped); err != nil {
			return fmt.Errorf("recover stale BYOK preparation: %w", err)
		}
		current, ok = coordinator.Current()
		if !ok {
			return fmt.Errorf("routing snapshot unavailable after recovery")
		}
	}
	preparing, committed, laneListeners, err := buildBYOKRoutingStates(coordinator.Nonce(), current.Generation()+1, current.Generation(), routes)
	if err != nil {
		return err
	}
	targets := make([]*url.URL, len(routes))
	for i, route := range routes {
		targets[i], err = url.Parse(route.Upstream)
		if err != nil {
			return fmt.Errorf("invalid BYOK upstream: %w", err)
		}
	}
	if err := coordinator.Publish(preparing); err != nil {
		return fmt.Errorf("publish BYOK routing preparation: %w", err)
	}
	laneServers := make([]*http.Server, 0, len(routes))
	laneErrors := make(chan error, len(routes))
	for i, route := range routes {
		laneServer := &http.Server{Handler: byokProxy(route.ID, targets[i])}
		laneServers = append(laneServers, laneServer)
		go func(server *http.Server, listener net.Listener) { laneErrors <- server.Serve(listener) }(laneServer, laneListeners[i])
	}
	for _, laneListener := range laneListeners {
		conn, dialErr := net.DialTimeout("tcp", laneListener.Addr().String(), time.Second)
		if dialErr != nil {
			return fmt.Errorf("BYOK lane not ready: %w", dialErr)
		}
		_ = conn.Close()
	}
	if err := coordinator.Publish(committed); err != nil {
		return fmt.Errorf("publish BYOK routing state: %w", err)
	}
	for _, lane := range committed.Lanes {
		if _, err := coordinator.Acquire(committed, lane); err != nil {
			return fmt.Errorf("acquire BYOK lane %s: %w", lane.ID, err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(byokGatewayPort()))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: BYOKGatewayHandlerWithCoordinator(coordinator), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 0, IdleTimeout: 90 * time.Second}
	byokGatewayState.Lock()
	byokGatewayState.listener, byokGatewayState.server = listener, server
	byokGatewayState.Unlock()
	if identity, identityErr := proxyIdentity(os.Getpid()); identityErr == nil {
		_ = persistByokGatewayOwnership(identity)
	}
	defer func() {
		byokGatewayState.Lock()
		byokGatewayState.listener, byokGatewayState.server = nil, nil
		byokGatewayState.Unlock()
	}()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	// Fail-closed revocation watcher: invalidates active routes on file deletion, corruption, or token revocation.
	go runBYOKWatcher(byokRoutesPath(), stopWatcher, 100*time.Millisecond)
	select {
	case err := <-serverErrors:
		for _, laneServer := range laneServers {
			_ = laneServer.Close()
		}
		return err
	case err := <-laneErrors:
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = server.Shutdown(shutdownContext)
		cancel()
		_ = server.Close()
		for _, laneServer := range laneServers {
			_ = laneServer.Close()
		}
		if err == http.ErrServerClosed {
			return fmt.Errorf("BYOK lane stopped")
		}
		return fmt.Errorf("BYOK lane stopped: %w", err)
	}
}

func runBYOKWatcher(routesPath string, stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastHash [32]byte
	var hasHash bool

	// Initialize hash baseline immediately before entering loop
	if initialData, err := os.ReadFile(routesPath); err == nil {
		lastHash = sha256.Sum256(initialData)
		hasHash = true
	} else {
		reloadErrStr := fmt.Sprintf("failed reading BYOK routes: %v", err)
		if os.IsNotExist(err) {
			reloadErrStr = "BYOK route registry not found"
		}
		byokReloadErr.Store(&reloadErrStr)
		_ = LoadBYOKRoutes()
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			data, err := os.ReadFile(routesPath)
			if err != nil {
				hasHash = false
				lastHash = [32]byte{}
				reloadErrStr := fmt.Sprintf("failed reading BYOK routes: %v", err)
				if os.IsNotExist(err) {
					reloadErrStr = "BYOK route registry not found"
				}
				byokReloadErr.Store(&reloadErrStr)
				_ = LoadBYOKRoutes()
				continue
			}
			h := sha256.Sum256(data)
			if !hasHash || h != lastHash {
				lastHash = h
				hasHash = true
				_ = LoadBYOKRoutes()
			}
		}
	}
}

func configuredBYOKRoutes() []BYOKRoute {
	tbl := activeBYOKRoutes.Load()
	if tbl == nil {
		if err := LoadBYOKRoutes(); err != nil {
			return nil
		}
		tbl = activeBYOKRoutes.Load()
	}
	if tbl == nil || len(tbl.items) == 0 {
		return nil
	}
	routes := make([]BYOKRoute, 0, len(tbl.items))
	for _, route := range tbl.items {
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	return routes
}

func buildBYOKRoutingStates(coordinatorNonce string, generation, previousGeneration uint64, routes []BYOKRoute) (util.RoutingState, util.RoutingState, []net.Listener, error) {
	preparing := util.RoutingState{Version: util.RoutingStateVersion, Generation: generation, PreviousGeneration: previousGeneration, State: util.RoutingStatePreparing, CoordinatorNonce: coordinatorNonce}
	listeners, err := appendBYOKBindings(&preparing, routes, false)
	if err != nil {
		return util.RoutingState{}, util.RoutingState{}, nil, err
	}
	committed := preparing
	committed.Lanes = append([]util.LaneBinding(nil), preparing.Lanes...)
	committed.Routes = append([]util.RouteBinding(nil), preparing.Routes...)
	committed.Generation++
	committed.PreviousGeneration = preparing.Generation
	committed.State = util.RoutingStateCommitted
	committed.CommitNonce, err = util.NewRoutingToken()
	if err != nil {
		return util.RoutingState{}, util.RoutingState{}, nil, err
	}
	for i := range committed.Lanes {
		committed.Lanes[i].Generation = committed.Generation
		committed.Lanes[i].Ready = true
	}
	for i := range committed.Routes {
		committed.Routes[i].Generation = committed.Generation
		committed.Routes[i].Revoked = false
		committed.Routes[i].RevokedGeneration = 0
	}
	if err := util.ValidateRoutingState(committed); err != nil {
		return util.RoutingState{}, util.RoutingState{}, nil, err
	}
	return preparing, committed, listeners, nil
}

func appendBYOKBindings(state *util.RoutingState, routes []BYOKRoute, ready bool) ([]net.Listener, error) {
	listeners := make([]net.Listener, 0, len(routes))
	for _, route := range routes {
		laneListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			for _, listener := range listeners {
				_ = listener.Close()
			}
			return nil, err
		}
		listeners = append(listeners, laneListener)
		port := laneListener.Addr().(*net.TCPAddr).Port
		laneID := "byok-lane-" + route.ID
		instanceNonce, err := util.NewRoutingToken()
		if err != nil {
			for _, listener := range listeners {
				_ = listener.Close()
			}
			_ = laneListener.Close()
			return nil, err
		}
		instance := "byok-instance-" + instanceNonce
		fingerprint := sha256.Sum256([]byte(route.Protocol + "\x00" + route.Upstream))
		state.Lanes = append(state.Lanes, util.LaneBinding{ID: laneID, InstanceNonce: instance, ProviderInstanceID: instance, Protocol: route.Protocol, UpstreamID: route.ID, Listener: laneListener.Addr().String(), Port: port, Generation: state.Generation, CoordinatorNonce: state.CoordinatorNonce, Ready: ready})
		revokedGeneration := uint64(0)
		if !ready {
			revokedGeneration = state.Generation
		}
		state.Routes = append(state.Routes, util.RouteBinding{ID: route.ID, Token: route.Token, ProfileID: route.ID, SurfaceID: "byok", ProviderInstanceID: instance, Protocol: route.Protocol, LaneID: laneID, LaneInstanceNonce: instance, CoordinatorNonce: state.CoordinatorNonce, UpstreamID: route.ID, ConfigFingerprint: hex.EncodeToString(fingerprint[:]), Generation: state.Generation, Revoked: !ready, RevokedGeneration: revokedGeneration})
	}
	return listeners, nil
}

func StopBYOKGateway() error {
	byokGatewayLifecycle.Lock()
	defer byokGatewayLifecycle.Unlock()
	byokGatewayState.Lock()
	server := byokGatewayState.server
	byokGatewayState.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
	record, ok := byokGatewayOwned()
	if !ok {
		if !byokGatewayLive() {
			_ = os.Remove(byokGatewayPIDPath())
			return nil
		}
		return fmt.Errorf("BYOK gateway is running but ownership cannot be verified")
	}
	proc, err := os.FindProcess(record.PID)
	if err != nil {
		return err
	}
	if err := stopByokGatewaySupervisor(); err != nil {
		util.L.Warn("stop BYOK gateway supervisor: " + err.Error())
	}
	_ = proxyKill(proc)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if proxyGone(proc) && !byokGatewayLive() {
			_ = os.Remove(byokGatewayPIDPath())
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("BYOK gateway did not stop (pid %d)", record.PID)
}

func byokGatewayLive() bool {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	response, err := client.Get(BYOKGatewayURL() + "/livez")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var live struct {
		Service string `json:"service"`
	}
	return json.NewDecoder(response.Body).Decode(&live) == nil && live.Service == "tokless-byok-gateway"
}

func byokGatewayPort() int {
	return util.BYOKGatewayPort()
}

func validBYOKUpstream(raw string) bool {
	return util.ValidBYOKUpstream(raw)
}

func validBYOKToken(value string) bool {
	if value == "" || strings.Contains(value, ".") || strings.Contains(value, ":") {
		return false
	}
	for _, c := range value {
		if !(c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

func validateBYOKRoute(route BYOKRoute) error {
	if !util.ValidBYOKID(route.ID) || !validBYOKToken(route.Token) || route.Protocol == "" {
		return fmt.Errorf("missing ID, token, or protocol")
	}
	if !validBYOKUpstream(route.Upstream) {
		return fmt.Errorf("upstream must be an HTTPS URL without credentials")
	}
	switch route.Protocol {
	case "openai-chat", "openai-responses", "anthropic-messages":
		return nil
	default:
		return fmt.Errorf("unsupported protocol %q", route.Protocol)
	}
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if util.IsLoopbackHost(host) {
		targetHost := host
		if strings.EqualFold(host, "localhost") {
			targetHost = "127.0.0.1"
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(targetHost, port))
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, address := range addresses {
		ip := address.IP
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}
		if !isPublicUpstreamIP(ip) {
			continue
		}
		if network == "tcp4" && ip.To4() == nil || network == "tcp6" && ip.To4() != nil {
			continue
		}
		dialer := net.Dialer{}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("upstream address is not publicly routable")
}

func isPublicUpstreamIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "198.18.0.0/15"} {
		_, blocked, _ := net.ParseCIDR(cidr)
		if blocked.Contains(ip) {
			return false
		}
	}
	return true
}

func routeParts(raw string) (id, token string, ok bool) {
	idx := strings.LastIndex(raw, ".")
	if idx <= 0 || idx >= len(raw)-1 {
		return "", "", false
	}
	return raw[:idx], raw[idx+1:], true
}

func parseVirtualPathPrefix(path string) (providerID, remainingPath string, isVirtual bool) {
	for _, prefix := range []string{"/tokless/byok/", "/byok/"} {
		if strings.HasPrefix(path, prefix) {
			rest := strings.TrimPrefix(path, prefix)
			slashIdx := strings.Index(rest, "/")
			if slashIdx < 0 {
				return "", "", false
			}
			prov := rest[:slashIdx]
			remaining := rest[slashIdx:]
			if !isValidVirtualProviderID(prov) {
				return "", "", false
			}
			return prov, remaining, true
		}
	}
	return "", "", false
}

func isValidVirtualProviderID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func byokRoutePath(protocol, path string) bool {
	return util.RouteProtocolPath(protocol, path)
}

func byokTargetPath(target, incoming string) string {
	target = strings.TrimRight(target, "/")
	incoming = strings.TrimRight(incoming, "/")
	if target == "" || target == "/" {
		return incoming
	}
	// If target is /v1 and incoming starts with /v1, strip /v1 from incoming
	if target == "/v1" && strings.HasPrefix(incoming, "/v1") {
		if incoming == "/v1" {
			return "/"
		}
		return incoming[3:]
	}
	// If target ends with /v1 and incoming starts with /v1, normalize
	if strings.HasSuffix(target, "/v1") && strings.HasPrefix(incoming, "/v1") {
		if incoming == "/v1" {
			return target
		}
		return target + incoming[3:]
	}
	// If incoming starts with target, strip target prefix
	if strings.HasPrefix(incoming, target) {
		rest := incoming[len(target):]
		if rest == "" {
			return "/"
		}
		if strings.HasPrefix(rest, "/") {
			return rest
		}
	}
	return incoming
}

func BYOKGatewayHandlerWithCoordinator(coordinator *util.RoutingCoordinator) http.Handler {
	return byokGatewayHandler(coordinator)
}

func byokGatewayHandlerForTest() http.Handler { return byokGatewayHandler(nil) }

func byokGatewayHandlerWithCoordinatorForTest(coordinator *util.RoutingCoordinator) http.Handler {
	return byokGatewayHandler(coordinator)
}

func byokGatewayHandler(coordinator *util.RoutingCoordinator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/livez" {
			w.Header().Set("Content-Type", "application/json")
			if errPtr := byokReloadErr.Load(); errPtr != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"service":"tokless-byok-gateway","status":"degraded","error":` + strconv.Quote(*errPtr) + `}`))
				return
			}
			_, _ = w.Write([]byte(`{"service":"tokless-byok-gateway","status":"ok"}`))
			return
		}
		credentials := r.Header.Values(byokRouteHeader)
		if len(credentials) > 1 {
			http.Error(w, "duplicate route header", http.StatusBadGateway)
			return
		}
		provID, remainingPath, isVirtual := parseVirtualPathPrefix(r.URL.Path)
		var route BYOKRoute
		var routeOK bool
		var cred string

		if isVirtual {
			route, routeOK = lookupBYOKRoute(provID)
			if !routeOK {
				http.Error(w, "unknown BYOK route", http.StatusBadGateway)
				return
			}
			if len(credentials) == 1 {
				hID, hToken, hOK := routeParts(credentials[0])
				cleanHID := hID
				if idx := strings.Index(cleanHID, ":"); idx >= 0 {
					cleanHID = cleanHID[idx+1:]
				}
				if !hOK || (cleanHID != provID && hID != route.ID) || subtle.ConstantTimeCompare([]byte(hToken), []byte(route.Token)) != 1 {
					http.Error(w, "unknown BYOK route", http.StatusBadGateway)
					return
				}
			}
			r.URL.Path = remainingPath
			cred = route.ID + "." + route.Token
		} else {
			id, token, ok := "", "", false
			if len(credentials) == 1 {
				id, token, ok = routeParts(credentials[0])
				cred = credentials[0]
			}
			route, routeOK = lookupBYOKRoute(id)
			ok = ok && routeOK && subtle.ConstantTimeCompare([]byte(token), []byte(route.Token)) == 1
			if !ok {
				http.Error(w, "unknown BYOK route", http.StatusBadGateway)
				return
			}
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" || r.URL.RawPath != "" || strings.Contains(r.URL.Path, "%") || !util.RouteProtocolPath(route.Protocol, r.URL.Path) {
			http.Error(w, "non-canonical route path", http.StatusBadGateway)
			return
		}
		proxyTarget := route.Upstream
		if coordinator != nil {
			selection, coordinatorContext, authorizeErr := coordinator.ResolveAndAuthorize(util.RouteRequest{
				Credential: cred, Protocol: route.Protocol, Method: r.Method,
				Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery,
			})
			if authorizeErr != nil {
				http.Error(w, "coordinator authorization failed", http.StatusBadGateway)
				return
			}
			proxyTarget = "http://" + selection.Listener
			requestContext, cancel := context.WithCancel(r.Context())
			stop := context.AfterFunc(coordinatorContext, cancel)
			defer func() { stop(); cancel() }()
			r = r.WithContext(requestContext)
		}
		target, err := url.Parse(proxyTarget)
		if err != nil {
			http.Error(w, "invalid BYOK upstream", http.StatusBadGateway)
			return
		}
		proxy := byokProxy(route.ID, target)
		proxy.ServeHTTP(w, r)
	})
}

func byokProxy(id string, target *url.URL) *httputil.ReverseProxy {
	tbl := activeBYOKRoutes.Load()
	key := id + "\x00" + target.String()
	if tbl != nil {
		tbl.proxyMu.Lock()
		proxy := tbl.proxies[key]
		tbl.proxyMu.Unlock()
		if proxy != nil {
			return proxy
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = byokTransportFactory()
	proxy.BufferPool = byokBufferPool
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		req.URL.Path = byokTargetPath(target.Path, req.URL.Path)
		req.URL.RawPath = ""
		originalDirector(req)
		req.Host = target.Host
		req.Header.Del(byokRouteHeader)
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(rw, "BYOK upstream unavailable", http.StatusBadGateway)
	}

	if tbl != nil {
		tbl.proxyMu.Lock()
		if existing := tbl.proxies[key]; existing != nil {
			tbl.proxyMu.Unlock()
			return existing
		}
		tbl.proxies[key] = proxy
		tbl.proxyMu.Unlock()
	}
	return proxy
}
