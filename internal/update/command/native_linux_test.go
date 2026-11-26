//go:build linux

package command

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/update"
	"sidravia/internal/update/internal/testfixture"
	"sidravia/internal/update/linuxplatform"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/packagefiles"
	"sidravia/internal/update/releases"
)

type cliImage struct {
	archive, manifest, signature, index []byte
	candidate                           releases.Candidate
	proof                               releases.VerifiedRelease
}

func cliDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// These real binaries retain the fixed GitHub URLs and immutable publisher root
// policy. Only a private build overlay supplies the synthetic public test root.
// A private TLS CONNECT proxy serves fixed-host fixture bytes; no production
// source/root override, real account, installation or external request is used.
func TestNativeCLIUpdate(t *testing.T) {
	if os.Getenv("SIDRAVIA_CLI_UPDATE_FIXTURE") != "1" {
		t.Skip("explicit private CLI fixture required")
	}
	nativeCLIUpdate(t, false)
}

func TestNativeCLIXZUpdate(t *testing.T) {
	if os.Getenv("SIDRAVIA_CLI_XZ_FIXTURE") != "1" {
		t.Skip("explicit private XZ CLI fixture required")
	}
	nativeCLIUpdate(t, true)
}

func nativeCLIUpdate(t *testing.T, useXZ bool) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	owned, err := os.MkdirTemp(filepath.Join(repo, ".local"), "cli-update-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("owned evidence", owned)
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	pub := key.Public().(ed25519.PublicKey)
	keys, err := releases.NewPublisherKeys([]ed25519.PublicKey{pub})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := testfixture.WritePublisherOverlay(t, repo, owned, pub)
	target := filepath.Join(owned, "portable with spaces")
	nextDir := filepath.Join(owned, "new-products")
	baseEnv := os.Environ()
	products := []struct{ dir, version, build string }{{target, "1.0.0", "cli-old"}, {nextDir, "1.1.0", "cli-new"}}
	thirdDir := filepath.Join(owned, "third-products")
	if useXZ {
		products = append(products, struct{ dir, version, build string }{thirdDir, "1.2.0", "cli-third"})
	}
	for _, item := range products {
		if err := os.MkdirAll(item.dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"sidraviad", "sidraviactl", "sidraviaupdater"} {
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-overlay", overlayPath, "-ldflags", "-X main.ProductVersion="+item.version+" -X main.BuildID="+item.build, "-o", filepath.Join(item.dir, role), "./cmd/"+role)
			cmd.Dir = repo
			cmd.Env = append(baseEnv, "GOTOOLCHAIN=local", "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build %s: %s %v", role, out, err)
			}
		}
	}
	old := makeCLIImage(t, target, "1.0.0", "cli-old", "1:11", key, keys)
	next := makeCLIImage(t, nextDir, "1.1.0", "cli-new", "2:22", key, keys)
	nextGzip := next
	if useXZ {
		next = cliXZImage(t, next, key, keys)
	}
	type encodingPair struct{ legacy, selected cliImage }
	published := map[string]encodingPair{"1.0.0": {old, old}, "1.1.0": {nextGzip, next}}
	var third cliImage
	if useXZ {
		third = makeCLIImage(t, thirdDir, "1.2.0", "cli-third", "3:33", key, keys)
	}
	releaseID := 2
	thirdPhase := false
	postXZSourceHits := 0

	if err := os.WriteFile(filepath.Join(target, packagefiles.Name), old.index, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sidravia.portable"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	oldMode := "valid"
	newLimited := false
	hits := 0
	candidateID := 22
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hits++
		if r.Host == "api.github.com" && r.URL.Path == "/repos/CaelestisAstesia/sidravia/releases" {
			json.NewEncoder(w).Encode([]any{map[string]any{"id": releaseID, "tag_name": "v" + next.candidate.Version, "draft": false, "prerelease": false, "assets": []any{map[string]any{"id": candidateID, "name": next.candidate.Name, "size": next.candidate.Size, "digest": "sha256:" + next.candidate.SHA256, "browser_download_url": next.candidate.DownloadURL, "state": "uploaded"}}}})
			return
		}
		image, legacy := next, nextGzip
		prefix := "/CaelestisAstesia/sidravia/releases/download/v" + next.candidate.Version + "/"
		for version, pair := range published {
			at := "/CaelestisAstesia/sidravia/releases/download/v" + version + "/"
			if strings.HasPrefix(r.URL.Path, at) {
				image, legacy, prefix = pair.selected, pair.legacy, at
				break
			}
		}
		if strings.HasPrefix(r.URL.Path, "/CaelestisAstesia/sidravia/releases/download/v1.0.0/") {
			image = old
			prefix = "/CaelestisAstesia/sidravia/releases/download/v1.0.0/"
			switch oldMode {
			case "missing":
				http.NotFound(w, r)
				return
			case "wrong-build":
				var altered releases.ReleaseManifest
				if err := json.Unmarshal(image.manifest, &altered); err != nil {
					http.Error(w, "fixture invalid", 500)
					return
				}
				altered.BuildID = "other-build"
				image.manifest, _ = json.Marshal(altered)
				image.signature = ed25519.Sign(key, image.manifest)
			case "limited":
				w.Header().Set("Retry-After", "600")
				w.WriteHeader(429)
				return
			}
		}
		if r.Host != "github.com" || !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		if image.candidate.Version == "1.0.0" {
			legacy = image
		}
		if newLimited && image.candidate.Version == "1.1.0" {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(429)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		switch name {
		case releases.ReleaseManifestName:
			if thirdPhase && image.candidate.Version == "1.1.0" {
				postXZSourceHits++
			}
			w.Write(legacy.manifest)
		case releases.ReleaseSignatureName:
			w.Write(legacy.signature)
		case releases.XZReleaseManifestName:
			w.Write(image.manifest)
		case releases.XZReleaseSignatureName:
			w.Write(image.signature)
		case image.candidate.Name:
			w.Write(image.archive)
		default:
			http.NotFound(w, r)
		}
	})
	proxy, ca := cliTLSProxy(t, handler)
	caPath := filepath.Join(owned, "transport-ca.pem")
	if err := os.WriteFile(caPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"HOME": filepath.Join(owned, "home"), "XDG_CACHE_HOME": filepath.Join(owned, "cache"), "XDG_CONFIG_HOME": filepath.Join(owned, "config"), "XDG_DATA_HOME": filepath.Join(owned, "data"), "XDG_STATE_HOME": filepath.Join(owned, "state"), "HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "NO_PROXY": "", "SSL_CERT_FILE": caPath, launchcontract.EnvNamespace: "cli-fixture-" + filepath.Base(owned), launchcontract.EnvSkipAutoLogin: "1"} {
		t.Setenv(name, value)
	}
	if err := os.MkdirAll(os.Getenv("HOME"), 0700); err != nil {
		t.Fatal(err)
	}
	count := 0
	run := func(want string, args ...string) json.RawMessage {
		t.Helper()
		count++
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(target, "sidraviactl"), append([]string{"update"}, args...)...)
		cmd.Env = os.Environ()
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if e := os.WriteFile(filepath.Join(owned, fmt.Sprintf("%02d-%s.json", count, args[0])), stdout.Bytes(), 0600); e != nil {
			t.Fatal(e)
		}
		var got struct {
			SchemaVersion int             `json:"schemaVersion"`
			OK            bool            `json:"ok"`
			Code          string          `json:"code"`
			Result        json.RawMessage `json:"result"`
		}
		if e := json.Unmarshal(stdout.Bytes(), &got); e != nil || got.SchemaVersion != 1 || got.Code != want || (want == "ok") != got.OK || (err == nil) != got.OK {
			t.Fatalf("%v code=%s want=%s err=%v decode=%v stdout=%s stderr=%s", args, got.Code, want, err, e, stdout.String(), stderr.String())
		}
		return got.Result
	}
	run("confirmation_required", "apply", "--candidate", "2:22")
	checked := run("ok", "check")
	var state cached
	if err := json.Unmarshal(checked, &state); err != nil {
		t.Fatal(err)
	}
	if state.Check.State != "available" || state.Check.Candidate == nil || !state.Check.Candidate.CanApply || state.Check.Candidate.ID != "2:22" {
		t.Fatalf("bad check %s", checked)
	}
	downloaded := run("ok", "download", "--candidate", "2:22")
	if err := json.Unmarshal(downloaded, &state); err != nil {
		t.Fatal(err)
	}
	if state.Downloaded == "" {
		t.Fatal("no downloaded path")
	}
	// Candidate identity is fresh at apply, even when the archive bytes match.
	mu.Lock()
	candidateID = 23
	mu.Unlock()
	run("candidate_changed", "apply", "--candidate", "2:22", "--confirm")
	mu.Lock()
	candidateID = 22
	oldMode = "missing"
	mu.Unlock()
	run("source_proof_unavailable", "apply", "--candidate", "2:22", "--confirm")
	mu.Lock()
	oldMode = "wrong-build"
	mu.Unlock()
	run("source_proof_unavailable", "apply", "--candidate", "2:22", "--confirm")
	mu.Lock()
	oldMode = "limited"
	mu.Unlock()
	run("rate_limited", "apply", "--candidate", "2:22", "--confirm")
	mu.Lock()
	before := hits
	oldMode = "valid"
	mu.Unlock()
	run("rate_limited", "apply", "--candidate", "2:22", "--confirm")
	mu.Lock()
	if hits != before {
		t.Error("backoff did not suppress transport requests")
	}
	mu.Unlock()
	// Only this fixture owns its private backoff and removes it to continue.
	if err := os.Remove(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "Sidravia/updates/github-backoff.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.Downloaded, bytes.Repeat([]byte{'x'}, len(next.archive)), 0600); err != nil {
		t.Fatal(err)
	}
	run("cache_invalid", "apply", "--candidate", "2:22", "--confirm")
	if err := os.WriteFile(state.Downloaded, next.archive, 0600); err != nil {
		t.Fatal(err)
	}
	// Explicitly reconcile an unstarted verified operation through the actual CLI.
	opRoot, err := productlayout.UpdateOperationRoot()
	if err != nil {
		t.Fatal(err)
	}
	store, err := operations.Open(opRoot, target, keys)
	if err != nil {
		t.Fatal(err)
	}
	record, err := operations.New(strings.Repeat("a", 32), update.Target{ID: target, Version: "1.0.0", BuildID: "cli-old", Layout: "portable"}, next.candidate, next.proof, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(context.Background(), record.ID, operations.Created, operations.Verified, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	status := run("ok", "status")
	if !bytes.Contains(status, []byte(`"phase":"verified"`)) {
		t.Fatalf("no verified status: %s", status)
	}
	run("confirmation_required", "recover", "--operation", record.ID)
	reconciled := run("ok", "recover", "--operation", record.ID, "--confirm")
	if !bytes.Contains(reconciled, []byte(`"phase":"aborted"`)) {
		t.Fatalf("no abort %s", reconciled)
	}
	run("ok", "recover", "--operation", record.ID, "--confirm")
	applied := run("ok", "apply", "--candidate", "2:22", "--confirm")
	var result applyResult
	if err := json.Unmarshal(applied, &result); err != nil {
		t.Fatal(err)
	}
	if !result.FilesApplied || result.Operation == nil || result.Operation.Phase != operations.Completed || result.ApplicationRecovery != "not_requested" || result.Authentication != "not_evaluated" {
		t.Fatalf("bad apply: %s", applied)
	}
	installed := run("ok", "version")
	if !bytes.Contains(installed, []byte(`"productVersion":"1.1.0"`)) {
		t.Fatalf("old binary %s", installed)
	}
	final := run("ok", "status")
	var statusView statusResult
	if err := json.Unmarshal(final, &statusView); err != nil {
		t.Fatal(err)
	}
	if statusView.Check.State != "up_to_date" || statusView.Check.Candidate == nil || statusView.Check.Candidate.CanApply || statusView.Operation == nil || statusView.Operation.Phase != operations.Completed {
		t.Fatalf("stale status %s", final)
	}
	run("not_newer", "apply", "--candidate", "2:22", "--confirm")
	// Simulate the exact Applied crash window using this fixture's completed
	// new tree and a real, now-dead coordinator generation. CLI status/recover
	// must remain reachable through startup protection and never launch anything.
	dead := exec.Command("/usr/bin/sleep", "30")
	if err := dead.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dead.Process.Kill(); dead.Wait() })
	peer, err := linuxplatform.Observe(dead.Process.Pid)
	if err != nil {
		dead.Process.Kill()
		dead.Wait()
		t.Fatal(err)
	}
	interrupted, err := operations.New(strings.Repeat("b", 32), record.Source, next.candidate, next.proof, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), interrupted); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(context.Background(), interrupted.ID, operations.Created, operations.Verified, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	rec := operations.Recovery{SourceFilesSHA256: cliDigest(old.index), Intent: update.RestoreIntent{Mode: "headless"}, CoordinatorPID: peer.PID, CoordinatorStart: peer.Start, Nonce: strings.Repeat("c", 32), ExpiresAt: time.Now().Add(20 * time.Minute), LogLevel: "info"}
	if err := store.SetRecovery(context.Background(), interrupted.ID, rec, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase := operations.Verified
	for _, to := range []operations.Phase{operations.Preparing, operations.Quiescent, operations.Applying, operations.Applied} {
		if _, _, err := store.Advance(context.Background(), interrupted.ID, phase, to, "", time.Now()); err != nil {
			t.Fatal(err)
		}
		phase = to
	}
	run("ok", "status")
	mu.Lock()
	newLimited = true
	mu.Unlock()
	run("rate_limited", "recover", "--operation", interrupted.ID, "--confirm")
	mu.Lock()
	before = hits
	newLimited = false
	mu.Unlock()
	run("rate_limited", "recover", "--operation", interrupted.ID, "--confirm")
	mu.Lock()
	if hits != before {
		t.Error("recover ignored backoff")
	}
	mu.Unlock()
	if err := os.Remove(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "Sidravia/updates/github-backoff.json")); err != nil {
		t.Fatal(err)
	}
	run("update_busy", "recover", "--operation", interrupted.ID, "--confirm")
	dead.Process.Signal(os.Interrupt)
	dead.Wait()
	peer.Close()
	recovered := run("ok", "recover", "--operation", interrupted.ID, "--confirm")
	if !bytes.Contains(recovered, []byte(`"phase":"resume_failed"`)) {
		t.Fatalf("Applied crash not reconciled %s", recovered)
	}
	run("ok", "recover", "--operation", interrupted.ID, "--confirm")
	// A second target exercises real CLI normal exit and cold restoration of a
	// running disconnected headless daemon, using the same compiled fixture bytes.
	target = filepath.Join(owned, "live portable")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(old.archive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		name := filepath.Base(h.Name)
		if name != "sidraviactl" && name != "sidraviad" && name != "sidraviaupdater" && name != packagefiles.Name && name != "sidravia.portable" {
			t.Fatal("unexpected fixture leaf", name)
		}
		b, e := io.ReadAll(tr)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(target, name), b, os.FileMode(h.Mode)); e != nil {
			t.Fatal(e)
		}
	}
	gz.Close()
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		t.Fatal(err)
	}
	layout, err := productlayout.ResolveTarget(target, ns)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.InstitutionProfilesDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile(filepath.Join(repo, "internal/daemon/configuration/profiles/jlu.json"))
	if err != nil {
		t.Fatal(err)
	}
	var profileData map[string]any
	if err := json.Unmarshal(profile, &profileData); err != nil {
		t.Fatal(err)
	}
	profileData["institutionProfileId"] = "update-loopback"
	delete(profileData, "announcement")
	protocol := profileData["institutionProtocolConfiguration"].(map[string]any)
	protocol["serverAddress"] = "127.0.0.1"
	protocol["serverPort"] = 9
	protocol["localPort"] = map[string]any{"mode": "system_assigned"}
	encoded, _ := json.Marshal(profileData)
	if err := os.WriteFile(filepath.Join(layout.InstitutionProfilesDirectory, "update-loopback.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	life := func(action string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(target, "sidraviactl"), "daemon", action)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("daemon %s: %s %v", action, out, err)
		}
	}
	life("start")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(target, "sidraviactl"), "daemon", "stop")
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("owned fixture stop: %s %v", out, err)
		}
	})
	run("ok", "check")
	run("ok", "download", "--candidate", "2:22")
	live := run("ok", "apply", "--candidate", "2:22", "--confirm")
	if err := json.Unmarshal(live, &result); err != nil {
		t.Fatal(err)
	}
	if !result.FilesApplied || result.Operation == nil || result.Operation.Phase != operations.Completed || result.ApplicationRecovery != "ready" {
		t.Fatalf("live update %s", live)
	}
	raw, err := os.ReadFile(layout.RuntimeInfoPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := contract.DecodeRuntimeInfo(raw)
	if err != nil || info.ProductVersion != "1.1.0" || info.BuildID != "cli-new" {
		t.Fatalf("new daemon identity: %+v %v", info, err)
	}
	life("status")
	run("ok", "status")
	if useXZ {
		mu.Lock()
		published["1.2.0"] = encodingPair{third, third}
		next, nextGzip, releaseID, candidateID, thirdPhase = third, third, 3, 33, true
		mu.Unlock()
		run("ok", "check")
		run("ok", "download", "--candidate", "3:33")
		again := run("ok", "apply", "--candidate", "3:33", "--confirm")
		if err := json.Unmarshal(again, &result); err != nil || !result.FilesApplied || result.Operation.Phase != operations.Completed || result.ApplicationRecovery != "ready" {
			t.Fatalf("post-XZ upgrade: %s %v", again, err)
		}
		raw, err := os.ReadFile(layout.RuntimeInfoPath)
		if err != nil {
			t.Fatal(err)
		}
		current, err := contract.DecodeRuntimeInfo(raw)
		if err != nil || current.ProductVersion != "1.2.0" || current.BuildID != "cli-third" {
			t.Fatalf("post-XZ runtime identity: %+v %v", current, err)
		}
		mu.Lock()
		sourceHits := postXZSourceHits
		mu.Unlock()
		if sourceHits == 0 {
			t.Fatal("XZ installed source was not authenticated through legacy metadata")
		}
		run("ok", "version")
		life("status")
		if err := os.WriteFile(filepath.Join(owned, "xz-passed"), []byte("test-key gzip A -> XZ B -> gzip C; signed source/index verification, live daemon normal exit and cold restoration; loopback-only\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(owned, "passed"), []byte("real CLI check/download/confirmed exec apply/status + source/cache/candidate/backoff/recover guards\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func makeCLIImage(t *testing.T, dir, version, build, id string, key ed25519.PrivateKey, keys *releases.PublisherKeys) cliImage {
	t.Helper()
	files := map[string][]byte{}
	m := packagefiles.Manifest{SchemaVersion: 1, Version: version, BuildID: build, OS: "linux", Arch: runtime.GOARCH, PayloadKind: "core"}
	for _, name := range []string{"sidraviactl", "sidraviad", "sidraviaupdater"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
		m.Files = append(m.Files, packagefiles.File{Path: name, Size: int64(len(b)), SHA256: cliDigest(b), Mode: 0755})
	}
	index, err := packagefiles.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	files[packagefiles.Name] = index
	files["sidravia.portable"] = nil
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		mode := int64(0755)
		if name == packagefiles.Name || name == "sidravia.portable" {
			mode = 0644
		}
		data := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: "sidravia-v" + version + "-linux-" + runtime.GOARCH + "/" + name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	c := releases.Candidate{ID: id, Version: version, Name: "sidravia-v" + version + "-linux-" + runtime.GOARCH + "-core-portable.tar.gz", Size: int64(b.Len()), SHA256: cliDigest(b.Bytes())}
	c.DownloadURL = "https://github.com/CaelestisAstesia/sidravia/releases/download/v" + version + "/" + c.Name
	keyID, _ := releases.PublisherKeyID(key.Public().(ed25519.PublicKey))
	manifest, err := json.Marshal(releases.ReleaseManifest{SchemaVersion: 2, KeyID: keyID, Version: version, BuildID: build, MinimumProtocol: 1, Assets: []releases.ReleaseAsset{{ManifestAsset: releases.ManifestAsset{OS: "linux", Arch: runtime.GOARCH, Kind: "core-portable", Name: c.Name, Size: c.Size, SHA256: c.SHA256}, FilesSHA256: cliDigest(index), PayloadKind: "core"}}})
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(key, manifest)
	proof, err := keys.Verify(manifest, sig, version)
	if err != nil {
		t.Fatal(err)
	}
	return cliImage{archive: b.Bytes(), manifest: manifest, signature: sig, index: index, candidate: c, proof: proof}
}

func cliTLSProxy(t *testing.T, handler http.Handler) (string, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Sidravia private fixture transport"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true, DNSNames: []string{"api.github.com", "github.com"}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewUnstartedServer(handler)
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	upstream.StartTLS()
	t.Cleanup(upstream.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || (r.Host != "api.github.com:443" && r.Host != "github.com:443") {
			http.Error(w, "fixture only", 403)
			return
		}
		remote, err := net.DialTimeout("tcp", upstream.Listener.Addr().String(), time.Second)
		if err != nil {
			http.Error(w, "local upstream unavailable", 502)
			return
		}
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			remote.Close()
			return
		}
		// Hijacked tunnels are owned and forcibly closed at fixture cleanup only.
		t.Cleanup(func() { client.Close(); remote.Close() })
		if _, err := rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			client.Close()
			remote.Close()
			return
		}
		if err := rw.Flush(); err != nil {
			client.Close()
			remote.Close()
			return
		}
		go func() { io.Copy(remote, rw); remote.Close() }()
		io.Copy(client, remote)
		client.Close()
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL, certPEM
}

// cliXZImage changes only the archive encoding and signed transport identity;
// the installed ownership bytes remain identical to the gzip counterpart.
func cliXZImage(t *testing.T, image cliImage, key ed25519.PrivateKey, keys *releases.PublisherKeys) cliImage {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(image.archive))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	gz.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("xz", "--threads=1", "--check=crc64", "-6", "--lzma2=dict=8MiB", "--stdout")
	cmd.Stdin = bytes.NewReader(raw)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if name != "XZ_OPT" && name != "XZ_DEFAULTS" {
			cmd.Env = append(cmd.Env, value)
		}
	}
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	image.archive = compressed
	image.candidate.Name = strings.TrimSuffix(image.candidate.Name, ".gz") + ".xz"
	image.candidate.Size = int64(len(compressed))
	image.candidate.SHA256 = cliDigest(compressed)
	image.candidate.DownloadURL = strings.TrimSuffix(image.candidate.DownloadURL, ".gz") + ".xz"
	var manifest releases.ReleaseManifest
	if err := json.Unmarshal(image.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Assets[0].Name = image.candidate.Name
	manifest.Assets[0].Size = image.candidate.Size
	manifest.Assets[0].SHA256 = image.candidate.SHA256
	image.manifest, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	image.signature = ed25519.Sign(key, image.manifest)
	image.proof, err = keys.Verify(image.manifest, image.signature, image.candidate.Version)
	if err != nil {
		t.Fatal(err)
	}
	return image
}
