package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/util"
)

type savingsData struct {
	Lifetime struct {
		TokensSaved  int64   `json:"tokens_saved"`
		TokensBefore int64   `json:"tokens_before"`
		CostUSD      float64 `json:"cost_usd"`
		Calls        int64   `json:"calls"`
		Percent      float64 `json:"savings_percent"`
	} `json:"lifetime"`
	Windows map[string]struct {
		TokensSaved int64   `json:"tokens_saved"`
		CostUSD     float64 `json:"cost_usd"`
		Calls       int64   `json:"calls"`
		Percent     float64 `json:"savings_percent"`
	} `json:"windows"`
	Models []struct {
		Model        string  `json:"model"`
		TokensSaved  int64   `json:"tokens_saved"`
		TokensBefore int64   `json:"tokens_before"`
		CostUSD      float64 `json:"cost_usd"`
		Calls        int64   `json:"calls"`
		Percent      float64 `json:"savings_percent"`
	} `json:"by_model"`
	Clients []struct {
		Client       string  `json:"client"`
		TokensSaved  int64   `json:"tokens_saved"`
		TokensBefore int64   `json:"tokens_before"`
		CostUSD      float64 `json:"cost_usd"`
		Calls        int64   `json:"calls"`
		Percent      float64 `json:"savings_percent"`
	} `json:"by_client"`
}

type perfData struct {
	Models []struct {
		Model        string  `json:"model"`
		Requests     int64   `json:"requests"`
		TokensBefore int64   `json:"tokens_before"`
		TokensSaved  int64   `json:"tokens_saved"`
		SavingsPct   float64 `json:"savings_pct"`
	} `json:"by_model"`
	CacheRead        int64   `json:"cache_read_tokens"`
	CacheWrite       int64   `json:"cache_write_tokens"`
	CacheHit         float64 `json:"cache_hit_pct"`
	Requests         int64   `json:"total_requests"`
	TokensSaved      int64   `json:"total_tokens_saved"`
	ToolSaved        int64   `json:"tool_saved"`
	TotalRequests    int64   `json:"requests"`
	TotalTokensSaved int64   `json:"tokens_saved"`
	Transforms       []struct {
		Transform string `json:"transform"`
		Uses      int64  `json:"uses"`
	} `json:"by_transform"`
}

func parsePerf(raw string) (perfData, error) {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return perfData{}, fmt.Errorf("JSON object not found")
	}
	var data perfData
	err := json.Unmarshal([]byte(raw[start:]), &data)
	return data, err
}

func contextModeStoreStats(home string) string {
	root := filepath.Join(home, ".config", "opencode", "context-mode")
	if util.IsWin {
		if v := os.Getenv("APPDATA"); v != "" {
			root = filepath.Join(v, "context-mode")
		}
	} else if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		root = filepath.Join(v, "context-mode")
	}
	count := 0
	var size int64
	for _, dir := range []string{filepath.Join(root, "sessions"), filepath.Join(root, "content")} {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), ".db") {
				info, e := d.Info()
				if e == nil {
					count++
					size += info.Size()
				}
			}
			return nil
		})
	}
	store := "no indexed data"
	if count > 0 {
		store = fmt.Sprintf("store %d DBs, %.1f MB", count, float64(size)/(1024*1024))
	}
	return store
}

func contextModeSidecarStats(sessionsDir string) string {
	files, err := filepath.Glob(filepath.Join(sessionsDir, "stats-*.json"))
	if err != nil {
		return ""
	}
	var newest, newestFallback string
	var newestTime, fallbackTime int64
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var candidate struct {
			UpdatedAt int64 `json:"updated_at"`
		}
		if json.Unmarshal(raw, &candidate) != nil {
			continue
		}
		updated := candidate.UpdatedAt
		if updated == 0 {
			updated = info.ModTime().UnixMilli()
		}
		if newestFallback == "" || updated > fallbackTime {
			newestFallback, fallbackTime = file, updated
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "stats-pid-"), ".json"))
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		cmd := string(cmdline)
		if err != nil || !strings.Contains(cmd, "context-mode") || (!strings.Contains(cmd, "node") && !strings.Contains(cmd, "/context-mode")) || strings.Contains(cmd, "tokless run-mcp") {
			continue
		}
		parentRaw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(parentRaw))
		if len(fields) < 4 {
			continue
		}
		parentPID, err := strconv.Atoi(fields[3])
		if err != nil {
			continue
		}
		parentCmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", parentPID))
		parent := strings.ToLower(string(parentCmd))
		if err != nil || !strings.Contains(parent, "opencode") || strings.Contains(parent, "--mcp-config") {
			continue
		}
		if newest == "" || updated > newestTime {
			newest, newestTime = file, updated
		}
	}
	if newest == "" {
		newest = newestFallback
	}
	if newest == "" {
		return ""
	}
	raw, err := os.ReadFile(newest)
	if err != nil {
		return ""
	}
	var data struct {
		TotalCalls           *int64   `json:"total_calls"`
		TokensSaved          *int64   `json:"tokens_saved"`
		Reduction            *float64 `json:"reduction_percent"`
		TokensSavedSession   *int64   `json:"tokens_saved_session"`
		DollarsSavedSession  *float64 `json:"dollars_saved_session"`
		TokensSavedLifetime  *int64   `json:"tokens_saved_lifetime"`
		DollarsSavedLifetime *float64 `json:"dollars_saved_lifetime"`
		DollarsSaved         *float64 `json:"dollars_saved"`
		ReductionPct         *float64 `json:"reduction_pct"`
		CacheHits            *int64   `json:"cache_hits"`
		CacheBytesSaved      *int64   `json:"cache_bytes_saved"`
		KeptOut              *int64   `json:"kept_out"`
		BytesIndexed         *int64   `json:"bytes_indexed"`
		BytesReturned        *int64   `json:"bytes_returned"`
		BytesSandboxed       *int64   `json:"bytes_sandboxed"`
		TotalProcessed       *int64   `json:"total_processed"`
		ByTool               map[string]struct {
			Calls int64 `json:"calls"`
			Bytes int64 `json:"bytes"`
		} `json:"by_tool"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return ""
	}
	var parts []string
	if data.TotalCalls == nil {
		return "stats unavailable"
	}
	// total_calls and the session counters belong to one MCP server process
	// (the sidecar file is stats-pid-<pid>.json); the lifetime counters are global.
	pid := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(newest), "stats-pid-"), ".json")
	session := []string{humanCount(*data.TotalCalls) + " calls"}
	if data.TokensSaved != nil {
		session = append(session, humanCount(*data.TokensSaved)+" tokens")
	}
	if data.DollarsSavedSession != nil {
		session = append(session, humanUSD(*data.DollarsSavedSession))
	}
	if data.ReductionPct != nil {
		session = append(session, fmt.Sprintf("%.1f%% reduction", *data.ReductionPct))
	} else if data.Reduction != nil {
		session = append(session, fmt.Sprintf("%.1f%% reduction", *data.Reduction))
	}
	parts = append(parts, "session (pid "+pid+"): "+strings.Join(session, ", "))
	if data.TokensSavedLifetime == nil && data.TokensSaved != nil {
		data.TokensSavedLifetime = data.TokensSaved
	}
	var lifetime []string
	if data.TokensSavedLifetime != nil {
		lifetime = append(lifetime, humanCount(*data.TokensSavedLifetime)+" tokens")
	}
	if data.DollarsSavedLifetime != nil {
		lifetime = append(lifetime, humanUSD(*data.DollarsSavedLifetime))
	} else if data.DollarsSaved != nil {
		lifetime = append(lifetime, humanUSD(*data.DollarsSaved))
	}
	if len(lifetime) > 0 {
		parts = append(parts, "lifetime: "+strings.Join(lifetime, " / "))
	}
	if data.CacheHits != nil || data.CacheBytesSaved != nil {
		parts = append(parts, fmt.Sprintf("cache %s hits / %s saved", ptrCount(data.CacheHits), ptrCount(data.CacheBytesSaved)))
	}
	if data.KeptOut != nil {
		parts = append(parts, "kept out "+humanCount(*data.KeptOut))
	}
	if data.BytesIndexed != nil || data.BytesReturned != nil || data.BytesSandboxed != nil || data.TotalProcessed != nil {
		parts = append(parts, fmt.Sprintf("bytes indexed/returned/sandboxed/processed %s/%s/%s/%s", ptrCount(data.BytesIndexed), ptrCount(data.BytesReturned), ptrCount(data.BytesSandboxed), ptrCount(data.TotalProcessed)))
	}
	tools := make([]string, 0, len(data.ByTool))
	for name := range data.ByTool {
		tools = append(tools, name)
	}
	sort.Slice(tools, func(i, j int) bool { return data.ByTool[tools[i]].Calls > data.ByTool[tools[j]].Calls })
	for i, name := range tools {
		if i == 3 {
			break
		}
		item := data.ByTool[name]
		parts = append(parts, fmt.Sprintf("tool %s %s calls/%s bytes", name, humanCount(item.Calls), humanCount(item.Bytes)))
	}
	return strings.Join(parts, " | ")
}

func ptrCount(n *int64) string {
	if n == nil {
		return "—"
	}
	return humanCount(*n)
}

func parseSavings(raw string) (savingsData, error) {
	var stats savingsData
	err := json.Unmarshal([]byte(raw), &stats)
	return stats, err
}

func humanCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return strconv.FormatInt(n, 10)
}

func humanUSD(n float64) string { return fmt.Sprintf("$%.2f", n) }

func runDoctorCommand(binary string, args ...string) (string, error) {
	return runDoctorCommandTimeout(4*time.Second, binary, args...)
}

func runDoctorCommandTimeout(d time.Duration, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// toolStatsBinary returns the executable to invoke for local stats.
// Node tools expose a package directory via InstalledPathFor, so resolve the
// real binary instead.
func toolStatsBinary(tool string) string {
	if tool == "codegraph" {
		return util.ResolveCodegraphBin()
	}
	return util.InstalledPathFor(tool)
}

func doctorToolStats(projectDir string, isProject bool) map[string][]string {
	stats := make(map[string][]string)
	for _, tool := range []string{"headroom", "projectmem", "rtk", "codegraph", "context-mode"} {
		if tool == "context-mode" {
			if toolStatsBinary(tool) != "" {
				stats[tool] = contextModeStats(util.Home())
			}
			continue
		}
		bin := toolStatsBinary(tool)
		if bin == "" {
			continue
		}
		var args []string
		switch tool {
		case "headroom":
			stats[tool] = headroomStatsSummary()
			continue
		case "projectmem":
			args = []string{"stats", "--format", "json"}
		case "rtk":
			args = []string{"gain"}
		case "codegraph":
			if !isProject {
				continue
			}
			args = []string{"status", "--json", projectDir}
		}
		out, err := runDoctorCommand(bin, args...)
		if err != nil {
			stats[tool] = []string{statusInfo("unavailable")}
			continue
		}
		stats[tool] = labelledStats(tool, out)
	}
	return stats
}

func contextModeStats(home string) []string {
	root := filepath.Join(home, ".config", "opencode", "context-mode")
	if util.IsWin {
		if v := os.Getenv("APPDATA"); v != "" {
			root = filepath.Join(v, "context-mode")
		}
	} else if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		root = filepath.Join(v, "context-mode")
	}
	rows := []string{contextModeStoreStats(home)}
	if side := contextModeSidecarStats(filepath.Join(root, "sessions")); side != "" {
		rows = append(rows, strings.Split(side, " | ")...)
	}
	return rows
}

func headroomStatsRows(savingsRaw, perfRaw string) []string {
	data, err := parseSavings(savingsRaw)
	if err != nil {
		return []string{util.C.Dim("unavailable")}
	}
	rows := []string{fmt.Sprintf("lifetime: %s saved, %s, %s calls, %.1f%%", humanCount(data.Lifetime.TokensSaved), humanUSD(data.Lifetime.CostUSD), humanCount(data.Lifetime.Calls), data.Lifetime.Percent)}
	for _, window := range []struct{ key, label string }{{"today", "today"}, {"last_7_days", "7d"}, {"last_30_days", "30d"}} {
		if w, ok := data.Windows[window.key]; ok {
			rows = append(rows, fmt.Sprintf("%s: %s saved, %s, %s calls", window.label, humanCount(w.TokensSaved), humanUSD(w.CostUSD), humanCount(w.Calls)))
		}
	}
	if perfRaw != "" {
		if perf, err := parsePerf(perfRaw); err == nil {
			requests, saved := perf.Requests, perf.TokensSaved
			if requests == 0 {
				requests = perf.TotalRequests
			}
			if saved == 0 {
				saved = perf.TotalTokensSaved
			}
			rows = append(rows, fmt.Sprintf("perf: %s requests, cache hit %.1f%%, %s tokens saved (%s tool saved)", humanCount(requests), perf.CacheHit, humanCount(saved), humanCount(perf.ToolSaved)))
			if len(perf.Transforms) > 0 {
				rows = append(rows, fmt.Sprintf("transform %s: %s uses", perf.Transforms[0].Transform, humanCount(perf.Transforms[0].Uses)))
			}
		}
	}
	rows = append(rows, "per-port stats unavailable: headroom has no port dimension")
	return rows
}

func headroomStatsSummary() []string {
	bin := util.InstalledPathFor("headroom")
	if bin == "" {
		return []string{util.C.Dim("unavailable")}
	}
	raw, err := runDoctorCommandTimeout(20*time.Second, bin, "savings", "--json")
	if err != nil {
		return []string{util.C.Dim("unavailable")}
	}
	perfRaw, perfErr := runDoctorCommandTimeout(20*time.Second, bin, "perf", "--format", "json", "--hours", "720")
	if perfErr != nil {
		perfRaw = ""
	}
	return headroomStatsRows(raw, perfRaw)
}

func labelledStats(tool, out string) []string {
	if tool == "projectmem" {
		return projectmemStats(out)
	}
	if tool == "codegraph" {
		return codegraphStats(out)
	}
	if tool == "context-mode" {
		return []string{out}
	}
	if tool == "rtk" {
		return rtkStats(out)
	}
	var rows []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Score:") && tool == "projectmem",
			strings.HasPrefix(line, "Total Tokens Saved:") && tool == "projectmem",
			strings.HasPrefix(line, "Estimated USD Saved:") && tool == "projectmem",
			strings.HasPrefix(line, "Total commands:") && tool == "rtk",
			strings.HasPrefix(line, "Tokens saved:") && tool == "rtk",
			strings.HasPrefix(line, "Efficiency meter:") && tool == "rtk",
			strings.HasPrefix(line, "Files:") && tool == "codegraph",
			strings.HasPrefix(line, "Nodes:") && tool == "codegraph",
			strings.HasPrefix(line, "Edges:") && tool == "codegraph",
			strings.HasPrefix(line, "DB Size:") && tool == "codegraph":
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		return []string{statusInfo("unavailable — no recognized stats output")}
	}
	return rows
}

func projectmemStats(raw string) []string {
	var d struct {
		Score      int              `json:"score"`
		Grade      string           `json:"grade"`
		Tokens     int64            `json:"tokens_saved"`
		USD        float64          `json:"usd_saved"`
		Hours      float64          `json:"debugging_hours_saved"`
		Components map[string]int64 `json:"components"`
		Capture    struct {
			Auto   int64   `json:"auto_captured"`
			Manual int64   `json:"manual"`
			Total  int64   `json:"total"`
			Rate   float64 `json:"auto_rate"`
		} `json:"capture"`
	}
	if json.Unmarshal([]byte(raw), &d) != nil || d.Grade == "" {
		return []string{statusInfo("unavailable — no recognized stats output")}
	}
	return []string{fmt.Sprintf("Score: %d/100 (%s)", d.Score, d.Grade), fmt.Sprintf("Saved: %s tokens / %s / %.1f debugging hours", humanCount(d.Tokens), humanUSD(d.USD), d.Hours), fmt.Sprintf("Capture: %s auto / %s manual / %s total (%.1f%% auto)", humanCount(d.Capture.Auto), humanCount(d.Capture.Manual), humanCount(d.Capture.Total), d.Capture.Rate), fmt.Sprintf("Knowledge: %d failed / %d successful / %d decisions / %d fixes / %d notes / %d gotcha files / %d high-churn", d.Components["failed_approaches"], d.Components["successful_attempts"], d.Components["decisions_documented"], d.Components["fixes_with_context"], d.Components["notes_count"], d.Components["files_with_gotchas"], d.Components["high_churn_files"])}
}

func codegraphStats(raw string) []string {
	var d struct {
		Files   int    `json:"fileCount"`
		Nodes   int    `json:"nodeCount"`
		Edges   int    `json:"edgeCount"`
		DB      int64  `json:"dbSizeBytes"`
		WAL     int64  `json:"walSizeBytes"`
		Last    string `json:"lastIndexed"`
		Pending struct {
			Added    int `json:"added"`
			Modified int `json:"modified"`
			Removed  int `json:"removed"`
		} `json:"pendingChanges"`
		Kinds     map[string]int `json:"nodesByKind"`
		Languages []string       `json:"languages"`
	}
	if json.Unmarshal([]byte(raw), &d) != nil || d.Files == 0 {
		return []string{statusInfo("unavailable — no recognized stats output")}
	}
	rows := []string{fmt.Sprintf("Files %d / Nodes %s / Edges %s", d.Files, humanCount(int64(d.Nodes)), humanCount(int64(d.Edges))), fmt.Sprintf("DB %.2f MB / WAL %.2f MB", float64(d.DB)/1048576, float64(d.WAL)/1048576), "Indexed: " + d.Last, fmt.Sprintf("Pending: +%d ~%d -%d", d.Pending.Added, d.Pending.Modified, d.Pending.Removed)}
	rows = append(rows, "Kinds: "+topMap(d.Kinds, 3), "Languages: "+strings.Join(capped(d.Languages, 3), ", "))
	return rows
}

func capped(items []string, n int) []string {
	if len(items) > n {
		return items[:n]
	}
	return items
}

func topMap[T ~int | ~int64](m map[string]T, cap int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] })
	if len(keys) > cap {
		keys = keys[:cap]
	}
	vals := make([]string, 0, len(keys))
	for _, k := range keys {
		vals = append(vals, fmt.Sprintf("%s %v", k, m[k]))
	}
	return strings.Join(vals, ", ")
}

func rtkStats(raw string) []string {
	var rows []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total commands:") || strings.HasPrefix(line, "Tokens saved:") || strings.HasPrefix(line, "Efficiency meter:") || strings.HasPrefix(line, "Input tokens:") || strings.HasPrefix(line, "Output tokens:") || strings.HasPrefix(line, "Total exec time:") || strings.HasPrefix(line, "Excluded:") {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		return []string{statusInfo("unavailable — no recognized stats output")}
	}
	return rows
}
