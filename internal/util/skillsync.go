package util

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Skills are instruction text owned by other repos. tokless downloads a copy,
// records its version, and renders from that. The copy built into the binary
// is the fallback.

func SkillDir(id string) string {
	return filepath.Join(ToklessDataDir(), "skills", id)
}

func SkillsRoot() string { return filepath.Join(ToklessDataDir(), "skills") }

func SkillInstalledVersion(id string) *string {
	raw, ok := ReadFileSafe(filepath.Join(SkillDir(id), "version"))
	if !ok {
		return nil
	}
	if v := strings.TrimSpace(raw); v != "" {
		return strp(v)
	}
	return nil
}

func SkillContent(id string) (string, bool) {
	if SkillUsingFallback(id) {
		return "", false
	}
	raw, ok := ReadFileSafe(filepath.Join(SkillDir(id), "content.md"))
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false
	}
	return raw, true
}

// SkillUsingFallback: upstream text was too big for its budget.
func SkillUsingFallback(id string) bool { return Exists(skillFallbackPath(id)) }

func skillFallbackPath(id string) string { return filepath.Join(SkillDir(id), "fallback") }

// skillFallbackSize is the rejected size, 0 if nothing was rejected.
func skillFallbackSize(id string) int {
	raw, ok := ReadFileSafe(skillFallbackPath(id))
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return n
}

// SkillLatest is the upstream version: a release tag, or a commit SHA for
// repos that don't tag.
func SkillLatest(s VersionSpec) *string {
	if s.Repo == "" {
		return nil
	}
	if s.UseTag {
		return githubLatestRelease(s.Repo)
	}
	return githubHeadSHA(s.Repo)
}

// Tags lose their "v" when read; the git ref needs it back.
func skillRef(s VersionSpec, version string) string {
	if s.UseTag {
		return "v" + version
	}
	return version
}

// SkillEnsure downloads the current skill text. It never fails the install —
// if the download doesn't work, the old copy keeps being used.
func SkillEnsure(s VersionSpec, report func(string, float64), upgrade bool) (bool, error) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true, nil
	}
	reportf := func(phase string, frac float64) {
		if report != nil {
			report(phase, frac)
		}
	}
	reportf("checking", 0.1)
	latest := SkillLatest(s)
	if latest == nil {
		reportf("offline — keeping current copy", 1)
		return true, nil
	}
	// A raised budget can admit text an older tokless rejected.
	rejected := skillFallbackSize(s.ID)
	fitsNow := rejected > 0 && s.MaxBytes > 0 && rejected <= s.MaxBytes
	if cur := SkillInstalledVersion(s.ID); cur != nil && *cur == *latest && !upgrade && !fitsNow {
		reportf("up to date", 1)
		return true, nil
	}
	reportf("fetching "+s.SkillDoc, 0.5)
	raw, err := skillFetch(s, *latest)
	if err != nil {
		L.Debug("skill fetch failed for " + s.ID + ": " + err.Error())
		return true, nil
	}
	body := NormalizeSkill(raw, SectionsByOwner[s.ID])
	if s.MaxBytes > 0 && len(body) > s.MaxBytes {
		// Record the version, or every update re-offers this same download.
		_ = writeSkillFallback(s.ID, *latest, len(body))
		reportf("over budget — using built-in copy", 1)
		return true, nil
	}
	if err := writeSkillCache(s.ID, *latest, body); err != nil {
		return true, nil
	}
	reportf("ready", 1)
	return true, nil
}

// Tags normally carry a "v"; one repo tagging without it shouldn't freeze
// the skill forever.
func skillFetch(s VersionSpec, version string) (string, error) {
	raw, err := httpGetString(skillURL(s, skillRef(s, version)))
	var status *httpStatusError
	if s.UseTag && errors.As(err, &status) && status.Code == http.StatusNotFound {
		return httpGetString(skillURL(s, version))
	}
	return raw, err
}

func skillURL(s VersionSpec, ref string) string {
	return "https://raw.githubusercontent.com/" + s.Repo + "/" + ref + "/" + s.SkillDoc
}

func writeSkillCache(id, version, body string) error {
	dir := SkillDir(id)
	if err := EnsureDir(dir); err != nil {
		return err
	}
	if err := WriteFile(filepath.Join(dir, "content.md"), body); err != nil {
		return err
	}
	_ = os.Remove(skillFallbackPath(id)) // upstream fits again
	return WriteFile(filepath.Join(dir, "version"), version+"\n")
}

func writeSkillFallback(id, version string, size int) error {
	dir := SkillDir(id)
	if err := EnsureDir(dir); err != nil {
		return err
	}
	// Drop stale text so an older version's copy can't render under the new
	// version number.
	_ = os.Remove(filepath.Join(dir, "content.md"))
	if err := WriteFile(skillFallbackPath(id), strconv.Itoa(size)+"\n"); err != nil {
		return err
	}
	return WriteFile(filepath.Join(dir, "version"), version+"\n")
}

// RemoveSkillCache lets the built-in copy take over again.
func RemoveSkillCache(id string) { _ = os.RemoveAll(SkillDir(id)) }

func httpGetString(u string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "tokless")
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{Status: resp.Status, Code: resp.StatusCode}
	}
	// One byte past the ceiling: a truncated doc could slip under the budget
	// and get cached as if it were whole.
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSkillDownload+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxSkillDownload {
		return "", errors.New("skill doc over " + strconv.Itoa(maxSkillDownload) + " bytes")
	}
	return string(b), nil
}

type httpStatusError struct {
	Status string
	Code   int
}

func (e *httpStatusError) Error() string { return "http " + e.Status }

var reHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// NormalizeSkill turns an upstream doc into one tokless section. Our heading
// has to be the only "## " line — that's how tokless finds its own blocks —
// so upstream headings get pushed down a level.
func NormalizeSkill(raw, canonicalHeading string) string {
	body := stripFrontmatter(raw)
	// HTML comments don't show in rendered markdown but an agent still reads
	// them, so anything hidden in one would be an instruction nobody reviewed.
	body = reHTMLComment.ReplaceAllString(body, "")
	body = dropLeadingH1(body)
	body = demoteHeadings(body)
	body = strings.TrimSpace(body)
	if canonicalHeading == "" {
		return body + "\n"
	}
	if body == "" {
		return canonicalHeading + "\n"
	}
	return canonicalHeading + "\n\n" + body + "\n"
}

func stripFrontmatter(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	trimmed := strings.TrimLeft(s, "\n")
	if !strings.HasPrefix(trimmed, "---\n") {
		return s
	}
	rest := trimmed[len("---\n"):]
	if i := strings.Index(rest, "\n---"); i >= 0 {
		after := rest[i+len("\n---"):]
		if j := strings.IndexByte(after, '\n'); j >= 0 {
			return after[j+1:]
		}
		return ""
	}
	return s
}

// Their title would duplicate ours.
func dropLeadingH1(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if strings.HasPrefix(ln, "# ") {
			return strings.Join(lines[i+1:], "\n")
		}
		break
	}
	return s
}

// Code blocks are left alone.
func demoteHeadings(s string) string {
	lines := strings.Split(s, "\n")
	inFence := false
	for i, ln := range lines {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(ln, "#") {
			continue
		}
		level := 0
		for level < len(ln) && ln[level] == '#' {
			level++
		}
		if level == 0 || level >= 6 || level >= len(ln) || ln[level] != ' ' {
			continue
		}
		lines[i] = "#" + ln
	}
	return strings.Join(lines, "\n")
}
