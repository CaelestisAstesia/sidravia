package candidatecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"sidravia/internal/persistence/jsonfile"
	"sidravia/internal/productlayout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/internal/request"
)

type Store struct {
	root, base string
	secure     *jsonfile.SecureStore
}

func Open(directory string, selection release.Selection) (*Store, *request.Failure) {
	base, err := productlayout.UpdateCacheRoot()
	if err != nil {
		return nil, &request.Failure{Code: "cache_unavailable", Message: "更新缓存不可用。"}
	}
	identity := sha256.Sum256([]byte(directory + "\x00" + selection.Kind + "\x00" + selection.Channel))
	secure, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		return nil, &request.Failure{Code: "cache_unavailable", Message: "更新缓存保护不可用。"}
	}
	return &Store{root: filepath.Join(base, hex.EncodeToString(identity[:16])), base: base, secure: secure}, nil
}

func (s *Store) Read(ctx context.Context) (State, bool, *request.Failure) {
	data, exists, err := s.secure.Read(ctx, filepath.Join(s.root, "state.json"), 2<<20)
	if err != nil {
		return State{}, false, &request.Failure{Code: "cache_unavailable", Message: "无法安全读取更新缓存。"}
	}
	var state State
	if exists && (jsonfile.DecodeStrict(data, &state) != nil || state.SchemaVersion != 1) {
		return State{}, false, &request.Failure{Code: "cache_invalid", Message: "更新缓存无效。"}
	}
	return state, exists, nil
}

type Mutation struct {
	store       *Store
	previous    State
	backoffPath string
}

// WithMutation keeps selection, download and synchronous application or
// handoff under the same cache lock. The caller must consume the selection
// inside the callback. Installation still authenticates the bytes it reads.
func (s *Store) WithMutation(ctx context.Context, run func(Mutation) request.Outcome) request.Outcome {
	backoffPath := filepath.Join(s.base, "github-backoff.json")
	if _, _, err := s.secure.Read(ctx, backoffPath, 1024); err != nil {
		return request.Fail("cache_unavailable", "更新来源缓存不可用。", nil)
	}
	releaseLock, err := lockCache(filepath.Join(s.base, "github-operation.lock"))
	if err != nil {
		return request.Fail("update_busy", "另一项更新操作正在进行。", nil)
	}
	defer releaseLock()
	// The pre-lock read serves status and protects the directory. It cannot
	// authorize a mutation because another host may have changed the state.
	previous, _, failure := s.Read(ctx)
	if failure != nil {
		return request.Fail(failure.Code, failure.Message, failure.Result)
	}
	if previous.Retired != nil {
		if previous.Retired.Path == previous.Downloaded {
			return request.Fail("cache_invalid", "更新缓存无效。", nil)
		}
		if err := removeCached(s.root, previous.Retired.Path, previous.Retired.Candidate); err != nil {
			return request.Fail("cache_cleanup_failed", "旧更新缓存仍无法清理，请检查缓存。", nil)
		}
	}
	if data, exists, err := s.secure.Read(ctx, backoffPath, 1024); err != nil {
		return request.Fail("cache_unavailable", "更新限流记录不可用。", nil)
	} else if exists {
		var wait backoff
		if jsonfile.DecodeStrict(data, &wait) != nil || wait.SchemaVersion != 1 {
			return request.Fail("cache_invalid", "更新限流记录无效。", nil)
		}
		if time.Now().Before(wait.Until) {
			return request.Fail("rate_limited", "GitHub 请求受限，请稍后重试。", wait)
		}
	}
	return run(Mutation{store: s, previous: previous, backoffPath: backoffPath})
}

func (m Mutation) Root() string { return m.store.root }
func (m Mutation) Downloaded() (string, *release.Candidate) {
	return m.previous.Downloaded, m.previous.Check.Candidate
}

func (m Mutation) PreserveDownload(state *State) *request.Failure {
	if m.previous.Downloaded == "" {
		return nil
	}
	valid, err := Inspect(m.store.root, m.previous.Downloaded, m.previous.Check.Candidate)
	if err != nil {
		return &request.Failure{Code: "cache_invalid", Message: "已下载缓存已变化，请先检查并修复缓存。"}
	}
	if valid && SamePackage(m.previous.Check.Candidate, state.Check.Candidate) {
		state.Downloaded = m.previous.Downloaded
	}
	return nil
}

func (m Mutation) Commit(ctx context.Context, state State, newDownload string) request.Outcome {
	if m.previous.Downloaded != "" && m.previous.Downloaded != state.Downloaded {
		state.Retired = &retiredPackage{Path: m.previous.Downloaded, Candidate: m.previous.Check.Candidate}
	}
	data, err := json.Marshal(state)
	if err != nil {
		return request.Fail("update_failed", "更新操作失败。", nil)
	}
	if err := m.store.secure.Replace(ctx, filepath.Join(m.store.root, "state.json"), data); err != nil {
		if newDownload != "" {
			if cleanupErr := removeCached(m.store.root, newDownload, state.Check.Candidate); cleanupErr != nil {
				return request.Fail("cache_cleanup_failed", "更新结果无法保存且缓存无法清理，请检查缓存。", nil)
			}
		}
		return request.Fail("cache_write_failed", "更新结果无法保存；不会启动安装。", nil)
	}
	if state.Retired != nil {
		if err := removeCached(m.store.root, state.Retired.Path, state.Retired.Candidate); err != nil {
			return request.Fail("cache_cleanup_failed", "更新状态已保存，但旧更新缓存无法清理。", state)
		}
		state.Retired = nil
	}
	return request.Success(state)
}

// SourceFailure saves source backoff for every network stage, while allowing
// callers to retain the appropriate presentation for the operation.
func (m Mutation) SourceFailure(ctx context.Context, err error, code, message string, result any) request.Outcome {
	var limited *github.RateLimitError
	if errors.As(err, &limited) {
		data, _ := json.Marshal(backoff{SchemaVersion: 1, Until: limited.RetryAt})
		if err := m.store.secure.Replace(context.WithoutCancel(ctx), m.backoffPath, data); err != nil {
			return request.Fail("cache_write_failed", "限流记录无法保存，请稍后重试", result)
		}
	}
	return request.Fail(code, message, result)
}
