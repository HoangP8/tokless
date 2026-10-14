package agents

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

var urlTokenRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"']*`)

// stripURLs removes URL tokens so a provider whose key is a substring of its
// own upstream hostname is not mistaken for an applied provider.
func stripURLs(s string) string { return urlTokenRe.ReplaceAllString(s, " ") }

type BYOKProvider struct {
	Provider string
	Upstream string
	File     string
	Stale    bool
}

// BYOKProvidersFor lists providers from the stash and route registry, marking keys absent from real config files stale.
func BYOKProvidersFor(id string) []BYOKProvider {
	spec, ok := proxySpecFor(id)
	if !ok {
		return nil
	}
	stash := loadProxyRouteStash(id)
	routes := map[string]util.BYOKRoute{}
	for _, route := range util.BYOKRoutesFor(id) {
		prefix := id + ":"
		if strings.HasPrefix(route.ID, prefix) {
			routes[strings.TrimPrefix(route.ID, prefix)] = route
		}
	}
	keys := map[string]bool{}
	for key := range stash {
		keys[key] = true
	}
	for key := range routes {
		keys[key] = true
	}
	files := []string(nil)
	if spec.ConfigFiles != nil {
		files = spec.ConfigFiles()
	}
	texts := make(map[string]string, len(files))
	for _, file := range files {
		if raw, err := os.ReadFile(file); err == nil {
			texts[file] = string(raw)
		}
	}
	providers := make([]BYOKProvider, 0, len(keys))
	for key := range keys {
		provider := BYOKProvider{Provider: key}
		entry := stash[key]
		route := routes[key]
		if entry.BaseURL != "" {
			provider.Upstream = entry.BaseURL
		} else if entry.Upstream != "" {
			provider.Upstream = entry.Upstream
		} else {
			provider.Upstream = route.Upstream
		}
		for _, file := range files {
			if strings.Contains(stripURLs(texts[file]), key) {
				provider.File = file
				break
			}
		}
		provider.Stale = provider.File == ""
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Provider < providers[j].Provider })
	return providers
}
