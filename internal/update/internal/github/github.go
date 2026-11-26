// Package github queries and downloads release assets; local publisher trust remains in release.
package github

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sidravia/internal/release"
)

var (
	ErrIncomplete  = errors.New("update release query incomplete")
	ErrRateLimited = errors.New("update source rate limited")
)

type RateLimitError struct{ RetryAt time.Time }

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

type GitHub struct {
	client        *http.Client
	apiBase       string
	publisherKeys *release.PublisherKeys
}

func NewGitHub(client *http.Client) *GitHub {
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 20 * time.Second
	copyClient.Jar = nil
	source := &GitHub{client: &copyClient, apiBase: "https://api.github.com", publisherKeys: release.PublisherTrust()}
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return release.ErrMetadata
		}
		base, _ := url.Parse(source.apiBase)
		if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host || req.URL.User != nil {
			return release.ErrMetadata
		}
		return nil
	}
	return source
}

type githubRelease struct {
	ID         int64   `json:"id"`
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Body       string  `json:"body"`
	Assets     []asset `json:"assets"`
}
type asset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
	State  string `json:"state"`
}

func (g *GitHub) Check(ctx context.Context, s release.Selection) (release.CheckResult, error) {
	rpm := s.Kind == "gui-rpm" || s.Kind == "core-rpm"
	var suffixes []string
	if rpm {
		if s.OS != "linux" || s.Arch != "amd64" || (s.Channel != "stable" && s.Channel != "preview") {
			return release.CheckResult{}, release.ErrUnsupported
		}
	} else {
		var err error
		suffixes, err = release.PackageSuffixes(s)
		if err != nil {
			return release.CheckResult{}, err
		}
	}
	current, err := release.ParseVersion(s.CurrentVersion)
	if err != nil {
		return release.CheckResult{}, release.ErrVersion
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	endpoint := g.apiBase + "/repos/" + release.Repository + "/releases?per_page=100"
	var selected *release.Candidate
	var selectedVersion release.Version
	seen := map[string]bool{}
	for page := 0; endpoint != ""; page++ {
		if page >= 5 || seen[endpoint] {
			return release.CheckResult{}, ErrIncomplete
		}
		seen[endpoint] = true
		items, next, err := g.page(ctx, endpoint)
		if err != nil {
			return release.CheckResult{}, err
		}
		for _, r := range items {
			if r.Draft || r.ID <= 0 || !strings.HasPrefix(r.Tag, "v") {
				continue
			}
			v, err := release.ParseVersion(strings.TrimPrefix(r.Tag, "v"))
			if err != nil {
				continue
			}
			if s.Channel == "stable" && (r.Prerelease || v.Prerelease()) {
				continue
			}
			if v.Prerelease() != r.Prerelease {
				continue
			}
			var matches []asset
			if rpm {
				matches = rpmAssets(r.Assets, v.String(), s.Kind)
			} else {
				for _, suffix := range suffixes {
					name := "sidravia-v" + v.String() + "-" + s.OS + "-" + s.Arch + "-" + suffix
					for _, a := range r.Assets {
						if a.Name == name {
							matches = append(matches, a)
						}
					}
					if len(matches) > 0 {
						break
					}
				}
			}
			if len(matches) == 0 {
				continue
			}
			if len(matches) != 1 {
				return release.CheckResult{}, release.ErrMetadata
			}
			a := matches[0]
			name := a.Name
			hash := strings.TrimPrefix(a.Digest, "sha256:")
			decoded, decodeErr := hex.DecodeString(hash)
			if a.ID <= 0 || a.State != "uploaded" || a.Size <= 0 || a.Size > 2<<30 || !strings.HasPrefix(a.Digest, "sha256:") || decodeErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != hash {
				return release.CheckResult{}, release.ErrMetadata
			}
			u, err := url.Parse(a.URL)
			expectedPath := "/" + release.Repository + "/releases/download/" + r.Tag + "/" + name
			if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.Path != expectedPath || u.RawQuery != "" || u.Fragment != "" {
				return release.CheckResult{}, release.ErrMetadata
			}
			if selected == nil || v.Compare(selectedVersion) > 0 || v.Compare(selectedVersion) == 0 && strings.HasSuffix(a.Name, ".tar.xz") && !strings.HasSuffix(selected.Name, ".tar.xz") {
				selectedVersion = v
				selected = &release.Candidate{ID: fmt.Sprintf("%d:%d", r.ID, a.ID), Version: v.String(), Name: name, Size: a.Size, SHA256: hash, DownloadURL: a.URL, ReleaseURL: "https://github.com/" + release.Repository + "/releases/tag/" + r.Tag, Notes: releaseNotes(r.Body)}
			}
		}
		endpoint = next
	}
	if selected == nil {
		return release.CheckResult{State: "no_compatible_release"}, nil
	}
	verified := false
	if !rpm && g.publisherKeys.Configured() {
		_, err := g.AuthenticateCandidate(ctx, *selected)
		if err != nil && !errors.Is(err, ErrSignatureMissing) {
			return release.CheckResult{}, err
		}
		verified = err == nil
	}
	state := "up_to_date"
	if selectedVersion.Compare(current) > 0 {
		state = "available"
	} else if selectedVersion.Compare(current) < 0 {
		state = "ahead_of_channel"
	}
	return release.CheckResult{State: state, Candidate: selected, PublisherVerified: verified}, nil
}

// RPM releases are numbered independently of the product version. Within a
// tag, select the highest package revision for the exact installed flavor.
func rpmAssets(assets []asset, version, kind string) []asset {
	packageName := "sidravia"
	if kind == "core-rpm" {
		packageName = "sidravia-core"
	}
	publishedPrefix := packageName + "-" + version + "-"
	nativePrefix := packageName + "-" + strings.Replace(version, "-", "~", 1) + "-"
	var matches []asset
	var highest uint64
	for _, a := range assets {
		revision, ok := strings.CutPrefix(a.Name, publishedPrefix)
		if !ok {
			revision, ok = strings.CutPrefix(a.Name, nativePrefix)
		}
		if !ok {
			continue
		}
		revision, ok = strings.CutSuffix(revision, ".x86_64.rpm")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(revision, 10, 64)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != revision || n < highest {
			continue
		}
		if n > highest {
			highest = n
			matches = matches[:0]
		}
		matches = append(matches, a)
	}
	return matches
}

func (g *GitHub) page(ctx context.Context, endpoint string) ([]githubRelease, string, error) {
	base, _ := url.Parse(g.apiBase)
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.Path != "/repos/"+release.Repository+"/releases" || u.User != nil {
		return nil, "", release.ErrMetadata
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Sidravia-Updater")
	response, err := g.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("query releases: %w", err)
	}
	defer response.Body.Close()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if response.StatusCode == 403 || response.StatusCode == 429 {
		return nil, "", sourceRateLimit(response.Header)
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("release source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(data) > 2<<20 {
		return nil, "", ErrIncomplete
	}
	var items []githubRelease
	if err = json.Unmarshal(data, &items); err != nil || items == nil {
		return nil, "", release.ErrMetadata
	}
	next, err := nextPage(strings.Join(response.Header.Values("Link"), ","))
	if err != nil {
		return nil, "", err
	}
	return items, next, nil
}

// Parse Link parameters without silently dropping an unfamiliar next link. A
// partial release list must never be presented as a complete version check.
func nextPage(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", nil
	}
	if len(header) > 16384 {
		return "", release.ErrMetadata
	}
	var links []string
	start := 0
	quoted, escaped, angle := false, false, false
	for index, ch := range header {
		if escaped {
			escaped = false
			continue
		}
		if quoted {
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		switch ch {
		case '"':
			if !angle {
				quoted = true
			}
		case '<':
			if angle {
				return "", release.ErrMetadata
			}
			angle = true
		case '>':
			if !angle {
				return "", release.ErrMetadata
			}
			angle = false
		case ',':
			if !angle {
				links = append(links, header[start:index])
				start = index + 1
			}
		}
	}
	if quoted || escaped || angle {
		return "", release.ErrMetadata
	}
	links = append(links, header[start:])
	next := ""
	for _, link := range links {
		link = strings.TrimSpace(link)
		end := strings.IndexByte(link, '>')
		if !strings.HasPrefix(link, "<") || end <= 1 {
			return "", release.ErrMetadata
		}
		rest := strings.TrimSpace(link[end+1:])
		if rest != "" && !strings.HasPrefix(rest, ";") {
			return "", release.ErrMetadata
		}
		_, parameters, err := mime.ParseMediaType("link" + rest)
		if err != nil {
			return "", release.ErrMetadata
		}
		for _, relation := range strings.Fields(parameters["rel"]) {
			if !strings.EqualFold(relation, "next") {
				continue
			}
			if next != "" {
				return "", release.ErrMetadata
			}
			next = link[1:end]
			u, err := url.Parse(next)
			if err != nil || u.Fragment != "" {
				return "", release.ErrMetadata
			}
			n, err := strconv.Atoi(u.Query().Get("page"))
			if err != nil || n < 1 || n > 100 {
				return "", release.ErrMetadata
			}
		}
	}
	return next, nil
}

func releaseNotes(text string) string {
	if len(text) <= 65536 {
		return text
	}
	runes := []rune(text)
	if len(runes) > 16000 {
		runes = runes[:16000]
	}
	return string(runes) + "\n发行说明较长，请打开版本页面查看全文。"
}

// Both the release API and signed-metadata downloads use the same bounded
// source backoff; callers must not immediately retry one endpoint via the other.
func sourceRateLimit(header http.Header) *RateLimitError {
	now := time.Now().UTC()
	retry := now.Add(time.Hour)
	if value, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && value > now.Unix() {
		retry = time.Unix(value, 0).UTC()
	}
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		if seconds > 86400 {
			seconds = 86400
		}
		retry = now.Add(time.Duration(seconds) * time.Second)
	} else if date, err := http.ParseTime(header.Get("Retry-After")); err == nil && date.After(now) {
		retry = date
	}
	if maximum := now.Add(24 * time.Hour); retry.After(maximum) {
		retry = maximum
	}
	return &RateLimitError{RetryAt: retry}
}
