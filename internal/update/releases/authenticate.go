package releases

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var ErrSignatureMissing = errors.New("release has no publisher signature")

// AuthenticateCandidate fetches only the fixed metadata names in the candidate's
// exact release. It never enables installation or trusts a key from that release.
// Apply must authenticate again against current roots rather than trusting a
// cached candidate's explanatory fields.
func (g *GitHub) AuthenticateCandidate(ctx context.Context, c Candidate) (VerifiedRelease, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedRelease{}, err
	}
	if err := validateCandidate(c); err != nil {
		return VerifiedRelease{}, err
	}
	if g.publisherKeys == nil || len(g.publisherKeys.keys) == 0 {
		return VerifiedRelease{}, ErrUntrusted
	}

	verified, err := g.AuthenticateVersion(ctx, c.Version)
	if err != nil {
		return VerifiedRelease{}, err
	}
	if _, err := verified.Asset(c); err != nil {
		return VerifiedRelease{}, err
	}
	return verified, nil
}

// AuthenticateVersion retrieves only fixed publisher metadata for this exact
// canonical version. It is also used to prove the installed source inventory.
func (g *GitHub) AuthenticateVersion(ctx context.Context, version string) (VerifiedRelease, error) {
	if _, err := ParseVersion(version); err != nil {
		return VerifiedRelease{}, ErrMetadata
	}
	if g.publisherKeys == nil || !g.publisherKeys.Configured() {
		return VerifiedRelease{}, ErrUntrusted
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := g.releaseMetadata(ctx, version, ReleaseManifestName, ReleaseManifestLimit)
	if err != nil {
		return VerifiedRelease{}, err
	}
	signature, err := g.releaseMetadata(ctx, version, ReleaseSignatureName, ed25519.SignatureSize)
	if err != nil {
		return VerifiedRelease{}, err
	}
	return g.publisherKeys.Verify(data, signature, version)
}

func (g *GitHub) releaseMetadata(ctx context.Context, version, name string, maximum int64) ([]byte, error) {
	client := *g.client
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !downloadHost(req.URL) {
			return ErrMetadata
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		return nil
	}
	url := "https://github.com/" + repository + "/releases/download/v" + version + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Sidravia-Updater")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrSignatureMissing
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		return nil, sourceRateLimit(response.Header)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release metadata source returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maximum {
		return nil, ErrMetadata
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > maximum {
		return nil, ErrMetadata
	}
	return data, nil
}
