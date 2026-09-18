package headroom

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

const routerRouteHeader = "X-Tokless-Route"

// Router serves only the currently published immutable route snapshot.
type Router struct {
	snapshot  util.RoutingSnapshot
	current   func() (util.RoutingSnapshot, bool)
	owned     func(util.RouteSelection) bool
	authorize func(util.RouteSelection) (context.Context, error)
}

func NewRouter(snapshot util.RoutingSnapshot, owned func(util.RouteSelection) bool) *Router {
	return &Router{snapshot: snapshot, owned: owned}
}

func NewCoordinatorRouter(coordinator *util.RoutingCoordinator) *Router {
	return &Router{
		current:   coordinator.Current,
		authorize: coordinator.Authorize,
	}
}

func (router *Router) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	protocol, ok := util.RouteProtocolForPath(request.URL.Path)
	if !ok {
		http.Error(w, "unsupported route protocol", http.StatusBadGateway)
		return
	}
	credentials := request.Header.Values(routerRouteHeader)
	if len(credentials) != 1 {
		http.Error(w, "route rejected", http.StatusBadGateway)
		return
	}
	snapshot := router.snapshot
	if router.current != nil {
		var current bool
		snapshot, current = router.current()
		if !current {
			http.Error(w, "route unavailable", http.StatusBadGateway)
			return
		}
	}
	ownership := router.owned
	if router.authorize != nil {
		ownership = func(util.RouteSelection) bool { return true }
	}
	selection, err := snapshot.ResolveRequest(util.RouteRequest{
		Credential: credentials[0],
		Protocol:   protocol,
		Method:     request.Method,
		Path:       request.URL.Path,
		RawPath:    request.URL.RawPath,
		RawQuery:   request.URL.RawQuery,
	}, ownership)
	if err != nil {
		http.Error(w, "route rejected", http.StatusBadGateway)
		return
	}
	if err := validateRouterSelection(selection); err != nil {
		http.Error(w, "route listener unavailable", http.StatusBadGateway)
		return
	}
	if router.authorize != nil {
		coordinatorContext, err := router.authorize(selection)
		if err != nil {
			http.Error(w, "route lease unavailable", http.StatusBadGateway)
			return
		}
		requestContext, cancel := context.WithCancel(request.Context())
		stop := context.AfterFunc(coordinatorContext, cancel)
		defer func() {
			stop()
			cancel()
		}()
		request = request.WithContext(requestContext)
	}
	target, err := url.Parse("http://" + selection.Listener)
	if err != nil {
		http.Error(w, "route listener unavailable", http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = routerTransport
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(response, "route backend unavailable", http.StatusBadGateway)
	}
	director := proxy.Director
	proxy.Director = func(outgoing *http.Request) {
		director(outgoing)
		outgoing.Header.Del(routerRouteHeader)
	}
	proxy.ServeHTTP(w, request)
}

var routerTransport http.RoundTripper = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
	IdleConnTimeout:       90 * time.Second,
}

func routerAddress(selection util.RouteSelection) string {
	host, rawPort, err := net.SplitHostPort(selection.Listener)
	port, portErr := strconv.Atoi(rawPort)
	if err != nil || portErr != nil || port <= 0 || port > 65535 || strconv.Itoa(port) != rawPort || (host != "127.0.0.1" && host != "::1") || net.JoinHostPort(host, rawPort) != selection.Listener {
		return ""
	}
	return selection.Listener
}

func validateRouterSelection(selection util.RouteSelection) error {
	if routerAddress(selection) == "" || selection.RouteID == "" || selection.LaneID == "" || selection.Generation == 0 {
		return fmt.Errorf("invalid route selection")
	}
	return nil
}
