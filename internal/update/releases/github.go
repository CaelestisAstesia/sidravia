package releases

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
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repository = "CaelestisAstesia/sidravia"

var (
	ErrIncomplete  = errors.New("update release query incomplete")
	ErrMetadata    = errors.New("invalid update release metadata")
	ErrRateLimited = errors.New("update source rate limited")
	ErrUnsupported = errors.New("update target unsupported")
)

type Selection struct {
	OS             string
	Arch           string
	Kind           string
	Channel        string
	CurrentVersion string
}
type Candidate struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"downloadUrl"`
	ReleaseURL  string `json:"releaseUrl"`
	Notes       string `json:"notes"`
	// Digest is transport integrity metadata, not a publisher signature.
	CanApply    bool   `json:"canApply"`
	ApplyReason string `json:"applyReason"`
}
type RateLimitError struct{ RetryAt time.Time }

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

type CheckResult struct {
	State     string     `json:"state"`
	Candidate *Candidate `json:"candidate,omitempty"`
}
type GitHub struct {
	client        *http.Client
	apiBase       string
	publisherKeys *PublisherKeys
}

func NewGitHub(client *http.Client) *GitHub {
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 20 * time.Second
	copyClient.Jar = nil
	source := &GitHub{client: &copyClient, apiBase: "https://api.github.com", publisherKeys: PublisherTrust()}
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return ErrMetadata
		}
		base, _ := url.Parse(source.apiBase)
		if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host || req.URL.User != nil {
			return ErrMetadata
		}
		return nil
	}
	return source
}

type release struct {
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

func packageSuffix(s Selection) (string, error) {
	if s.Channel != "stable" && s.Channel != "preview" {
		return "", ErrUnsupported
	}
	if s.Arch != "amd64" && s.Arch != "arm64" {
		return "", ErrUnsupported
	}
	switch s.Kind {
	case "setup":
		if s.OS == "windows" && s.Arch == "amd64" {
			return "setup.exe", nil
		}
	case "gui-portable":
		if s.OS == "windows" && s.Arch == "amd64" {
			return "gui-portable.zip", nil
		}
		if s.OS == "linux" && s.Arch == "amd64" {
			return "gui-portable.tar.gz", nil
		}
	case "core-portable":
		if s.OS == "windows" {
			return "core-portable.zip", nil
		}
		if s.OS == "linux" {
			return "core-portable.tar.gz", nil
		}
	}
	return "", ErrUnsupported
}

func (g *GitHub) Check(ctx context.Context, s Selection) (CheckResult, error) {
	suffix, err := packageSuffix(s)
	if err != nil {
		return CheckResult{}, err
	}
	current, err := ParseVersion(s.CurrentVersion)
	if err != nil {
		return CheckResult{}, ErrVersion
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	endpoint := g.apiBase + "/repos/" + repository + "/releases?per_page=100"
	var selected *Candidate
	var selectedVersion Version
	seen := map[string]bool{}
	for page := 0; endpoint != ""; page++ {
		if page >= 5 || seen[endpoint] {
			return CheckResult{}, ErrIncomplete
		}
		seen[endpoint] = true
		items, next, err := g.page(ctx, endpoint)
		if err != nil {
			return CheckResult{}, err
		}
		for _, r := range items {
			if r.Draft || r.ID <= 0 || !strings.HasPrefix(r.Tag, "v") {
				continue
			}
			v, err := ParseVersion(strings.TrimPrefix(r.Tag, "v"))
			if err != nil {
				continue
			}
			if s.Channel == "stable" && (r.Prerelease || v.Prerelease()) {
				continue
			}
			if v.Prerelease() != r.Prerelease {
				continue
			}
			name := "sidravia-v" + v.String() + "-" + s.OS + "-" + s.Arch + "-" + suffix
			var matches []asset
			for _, a := range r.Assets {
				if a.Name == name {
					matches = append(matches, a)
				}
			}
			if len(matches) == 0 {
				continue
			}
			if len(matches) != 1 {
				return CheckResult{}, ErrMetadata
			}
			a := matches[0]
			hash := strings.TrimPrefix(a.Digest, "sha256:")
			decoded, decodeErr := hex.DecodeString(hash)
			if a.ID <= 0 || a.State != "uploaded" || a.Size <= 0 || a.Size > 2<<30 || !strings.HasPrefix(a.Digest, "sha256:") || decodeErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != hash {
				return CheckResult{}, ErrMetadata
			}
			u, err := url.Parse(a.URL)
			expectedPath := "/" + repository + "/releases/download/" + r.Tag + "/" + name
			if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.Path != expectedPath || u.RawQuery != "" || u.Fragment != "" {
				return CheckResult{}, ErrMetadata
			}
			if selected == nil || v.Compare(selectedVersion) > 0 {
				selectedVersion = v
				selected = &Candidate{ID: fmt.Sprintf("%d:%d", r.ID, a.ID), Version: v.String(), Name: name, Size: a.Size, SHA256: hash, DownloadURL: a.URL, ReleaseURL: "https://github.com/" + repository + "/releases/tag/" + r.Tag, Notes: releaseNotes(r.Body), ApplyReason: "publisher_signature_required"}
			}
		}
		endpoint = next
	}
	if selected == nil {
		return CheckResult{State: "no_compatible_release"}, nil
	}
	// Builds without configured publisher roots keep legacy check/download
	// behavior. Native portable apply still requires installed-source proof and
	// explicit consent at the command boundary.
	if g.publisherKeys != nil && len(g.publisherKeys.keys) > 0 {
		if _, err := g.AuthenticateCandidate(ctx, *selected); err != nil {
			if !errors.Is(err, ErrSignatureMissing) {
				return CheckResult{}, err
			}
		} else {
			selected.ApplyReason = "platform_not_supported"
			if selectedVersion.Compare(current) <= 0 {
				selected.ApplyReason = "not_newer"
			}
			if s.OS == "linux" && runtime.GOOS == "linux" && s.Arch == runtime.GOARCH && (s.Kind == "core-portable" || s.Kind == "gui-portable") && selectedVersion.Compare(current) > 0 {
				selected.CanApply = true
				selected.ApplyReason = "source_verification_and_confirmation_required"
			}
			if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" && s.OS == "windows" && s.Arch == "amd64" && (s.Kind == "setup" || s.Kind == "core-portable" || s.Kind == "gui-portable") && selectedVersion.Compare(current) > 0 {
				selected.CanApply = true
				selected.ApplyReason = "source_verification_and_confirmation_required"
			}
		}
	}
	state := "up_to_date"
	if selectedVersion.Compare(current) > 0 {
		state = "available"
	} else if selectedVersion.Compare(current) < 0 {
		state = "ahead_of_channel"
	}
	return CheckResult{State: state, Candidate: selected}, nil
}
func (g *GitHub) page(ctx context.Context, endpoint string) ([]release, string, error) {
	base, _ := url.Parse(g.apiBase)
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.Path != "/repos/"+repository+"/releases" || u.User != nil {
		return nil, "", ErrMetadata
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
	var items []release
	if err = json.Unmarshal(data, &items); err != nil || items == nil {
		return nil, "", ErrMetadata
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
		return "", ErrMetadata
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
				return "", ErrMetadata
			}
			angle = true
		case '>':
			if !angle {
				return "", ErrMetadata
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
		return "", ErrMetadata
	}
	links = append(links, header[start:])
	next := ""
	for _, link := range links {
		link = strings.TrimSpace(link)
		end := strings.IndexByte(link, '>')
		if !strings.HasPrefix(link, "<") || end <= 1 {
			return "", ErrMetadata
		}
		rest := strings.TrimSpace(link[end+1:])
		if rest != "" && !strings.HasPrefix(rest, ";") {
			return "", ErrMetadata
		}
		_, parameters, err := mime.ParseMediaType("link" + rest)
		if err != nil {
			return "", ErrMetadata
		}
		for _, relation := range strings.Fields(parameters["rel"]) {
			if !strings.EqualFold(relation, "next") {
				continue
			}
			if next != "" {
				return "", ErrMetadata
			}
			next = link[1:end]
			u, err := url.Parse(next)
			if err != nil || u.Fragment != "" {
				return "", ErrMetadata
			}
			n, err := strconv.Atoi(u.Query().Get("page"))
			if err != nil || n < 1 || n > 100 {
				return "", ErrMetadata
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
