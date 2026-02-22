package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	updateCheckInterval = 24 * time.Hour
	updateRequestTTL    = 5 * time.Second
	updateRepoOwner     = "jakebf"
	updateRepoName      = "planc"
)

var (
	updateAPIBaseURL    = "https://api.github.com"
	updateNow           = time.Now
	fetchLatestReleaseF = fetchLatestRelease
)

type updateState struct {
	CheckedAt       time.Time `json:"checked_at"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	ReleaseURL      string    `json:"release_url,omitempty"`
	LastSeenVersion string    `json:"last_seen_version,omitempty"`
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

//go:embed CHANGELOG.md
var bundledChangelog string

func startupUpdateCmd(currentVersion string) tea.Cmd {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" || currentVersion == "dev" {
		return nil
	}
	return func() tea.Msg {
		var msg startupUpdateMsg
		if cmd := checkForUpdate(currentVersion); cmd != nil {
			if m := cmd(); m != nil {
				if up, ok := m.(updateAvailableMsg); ok {
					msg.update = &up
				}
			}
		}
		if cmd := checkForReleaseNotes(currentVersion); cmd != nil {
			if m := cmd(); m != nil {
				if notes, ok := m.(releaseNotesMsg); ok {
					msg.releaseNotes = &notes
				}
			}
		}
		if msg.update == nil && msg.releaseNotes == nil {
			return nil
		}
		return msg
	}
}

func updateStatePath() (string, error) {
	cfg, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), "update-check.json"), nil
}

func loadUpdateState(path string) (updateState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return updateState{}, nil
		}
		return updateState{}, err
	}
	var st updateState
	if err := json.Unmarshal(data, &st); err != nil {
		return updateState{}, err
	}
	return st, nil
}

func saveUpdateState(path string, st updateState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// Atomic write: temp file + rename to avoid corruption on crash.
	tmp, err := os.CreateTemp(dir, ".update-check-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func fetchLatestRelease(owner, repo string) (*releaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateRequestTTL)
	defer cancel()

	url := fmt.Sprintf(
		"%s/repos/%s/%s/releases/latest",
		strings.TrimRight(updateAPIBaseURL, "/"),
		owner,
		repo,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "planc-update-check")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github latest release: %s", resp.Status)
	}

	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("github latest release missing tag_name")
	}
	return &rel, nil
}

func checkForUpdate(currentVersion string) tea.Cmd {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" || currentVersion == "dev" {
		return nil
	}
	return func() tea.Msg {
		path, err := updateStatePath()
		if err != nil {
			return nil
		}

		st, err := loadUpdateState(path)
		if err == nil && !st.CheckedAt.IsZero() && updateNow().Sub(st.CheckedAt) < updateCheckInterval {
			if isNewerVersion(currentVersion, st.LatestVersion) {
				return updateAvailableMsg{version: st.LatestVersion, url: st.ReleaseURL}
			}
			return nil
		}

		latest, err := fetchLatestReleaseF(updateRepoOwner, updateRepoName)
		if err != nil {
			// Per UX decision: failed checks do not advance checked_at.
			return nil
		}

		st.CheckedAt = updateNow().UTC()
		st.LatestVersion = latest.TagName
		st.ReleaseURL = latest.HTMLURL
		_ = saveUpdateState(path, st)

		if isNewerVersion(currentVersion, latest.TagName) {
			return updateAvailableMsg{version: latest.TagName, url: latest.HTMLURL}
		}
		return nil
	}
}

func checkForReleaseNotes(currentVersion string) tea.Cmd {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" || currentVersion == "dev" {
		return nil
	}
	currentCanonical, ok := canonicalSemver(currentVersion)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		path, err := updateStatePath()
		if err != nil {
			return nil
		}
		st, err := loadUpdateState(path)
		if err != nil {
			return nil
		}

		if st.LastSeenVersion == "" {
			st.LastSeenVersion = currentCanonical
			_ = saveUpdateState(path, st)
			return nil
		}
		if !isNewerVersion(st.LastSeenVersion, currentCanonical) {
			if st.LastSeenVersion != currentCanonical {
				st.LastSeenVersion = currentCanonical
				_ = saveUpdateState(path, st)
			}
			return nil
		}

		notes := releaseNotesSince(st.LastSeenVersion, currentCanonical, bundledChangelog)
		if strings.TrimSpace(notes) == "" {
			notes = fmt.Sprintf("## %s\n\nUpdated to %s.\n", currentCanonical, currentCanonical)
		}
		return releaseNotesMsg{
			version:  currentCanonical,
			markdown: notes,
		}
	}
}

func markReleaseNotesSeen(version string) tea.Cmd {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	return func() tea.Msg {
		path, err := updateStatePath()
		if err != nil {
			return nil
		}
		st, err := loadUpdateState(path)
		if err != nil {
			return nil
		}
		st.LastSeenVersion = version
		_ = saveUpdateState(path, st)
		return nil
	}
}

type changelogSection struct {
	heading string
	version string
	body    string
}

func releaseNotesSince(lastSeenVersion, currentVersion, changelog string) string {
	sections := parseChangelogSections(changelog)
	var picked []string
	for _, sec := range sections {
		cmpLow, ok := compareVersions(sec.version, lastSeenVersion)
		if !ok || cmpLow <= 0 {
			continue
		}
		cmpHigh, ok := compareVersions(sec.version, currentVersion)
		if !ok || cmpHigh > 0 {
			continue
		}
		text := sec.heading
		if body := strings.TrimSpace(sec.body); body != "" {
			text += "\n" + body
		}
		picked = append(picked, text)
	}
	return strings.TrimSpace(strings.Join(picked, "\n\n"))
}

func parseChangelogSections(changelog string) []changelogSection {
	lines := strings.Split(changelog, "\n")
	var sections []changelogSection
	var current *changelogSection

	flush := func() {
		if current == nil {
			return
		}
		current.body = strings.TrimSpace(current.body)
		sections = append(sections, *current)
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			if v, ok := versionFromHeading(line); ok {
				current = &changelogSection{
					heading: strings.TrimSpace(line),
					version: v,
				}
			} else {
				current = nil
			}
			continue
		}
		if current != nil {
			if current.body == "" {
				current.body = line
			} else {
				current.body += "\n" + line
			}
		}
	}
	flush()
	return sections
}

func versionFromHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "## ") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "## "))
	if rest == "" {
		return "", false
	}

	var candidate string
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end <= 1 {
			return "", false
		}
		candidate = rest[1:end]
	} else {
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return "", false
		}
		candidate = fields[0]
	}
	candidate = strings.Trim(candidate, "[]()")
	candidate = strings.TrimSuffix(candidate, ":")
	return canonicalSemver(candidate)
}

func compareVersions(a, b string) (int, bool) {
	va, ok := parseSemver(a)
	if !ok {
		return 0, false
	}
	vb, ok := parseSemver(b)
	if !ok {
		return 0, false
	}
	return compareSemver(va, vb), true
}

func canonicalSemver(s string) (string, bool) {
	v, ok := parseSemver(s)
	if !ok {
		return "", false
	}
	if v.prerelease != "" {
		return fmt.Sprintf("v%d.%d.%d-%s", v.major, v.minor, v.patch, v.prerelease), true
	}
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch), true
}

type parsedSemver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func isNewerVersion(current, latest string) bool {
	cur, ok := parseSemver(current)
	if !ok {
		return false
	}
	next, ok := parseSemver(latest)
	if !ok {
		return false
	}
	return compareSemver(next, cur) > 0
}

func parseSemver(s string) (parsedSemver, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return parsedSemver{}, false
	}
	if strings.HasPrefix(s, "v") {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	prerelease := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		prerelease = s[i+1:]
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return parsedSemver{}, false
	}

	major, ok := parseSemverInt(parts[0])
	if !ok {
		return parsedSemver{}, false
	}
	minor, ok := parseSemverInt(parts[1])
	if !ok {
		return parsedSemver{}, false
	}
	patch, ok := parseSemverInt(parts[2])
	if !ok {
		return parsedSemver{}, false
	}

	return parsedSemver{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: prerelease,
	}, true
}

func parseSemverInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

func compareSemver(a, b parsedSemver) int {
	if a.major != b.major {
		return cmpInt(a.major, b.major)
	}
	if a.minor != b.minor {
		return cmpInt(a.minor, b.minor)
	}
	if a.patch != b.patch {
		return cmpInt(a.patch, b.patch)
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}

// comparePrerelease follows semver precedence: no prerelease (stable) ranks
// higher than any prerelease. Dot-separated identifiers are compared
// numerically when both are digits, lexically otherwise.
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1 // stable > prerelease
	}
	if b == "" {
		return -1
	}

	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, an := parseSemverInt(as[i])
		bi, bn := parseSemverInt(bs[i])
		switch {
		case an && bn:
			if ai != bi {
				return cmpInt(ai, bi)
			}
		case an && !bn:
			return -1
		case !an && bn:
			return 1
		default:
			if as[i] != bs[i] {
				return strings.Compare(as[i], bs[i])
			}
		}
	}
	return cmpInt(len(as), len(bs))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
