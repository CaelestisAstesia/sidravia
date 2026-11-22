package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"sidravia/internal/update/github"
	"sidravia/internal/update/releases"
)

type response struct {
	SchemaVersion int    `json:"schemaVersion"`
	OK            bool   `json:"ok"`
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
	Result        any    `json:"result,omitempty"`
}

func safeCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, github.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, github.ErrIncomplete):
		return "incomplete_query"
	case errors.Is(err, github.ErrIntegrity):
		return "integrity_failed"
	case errors.Is(err, releases.ErrUntrusted):
		return "publisher_untrusted"
	case errors.Is(err, releases.ErrMetadata):
		return "invalid_metadata"
	case errors.Is(err, releases.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, releases.ErrVersion):
		return "unknown_current_version"
	}
	return "source_unavailable"
}
func safeMessage(err error) string {
	switch safeCode(err) {
	case "cancelled":
		return "更新操作已取消。"
	case "timeout":
		return "更新操作超时。"
	case "rate_limited":
		return "GitHub 请求受限，请稍后重试。"
	case "incomplete_query":
		return "发行信息未完整获取，请稍后重试。"
	case "integrity_failed":
		return "下载文件校验失败，不能用于更新。"
	case "unsupported":
		return "当前平台尚无适用的更新能力。"
	case "unknown_current_version":
		return "当前构建版本无法用于更新比较。"
	case "publisher_untrusted":
		return "发行签名不可信，已停止操作。"
	case "invalid_metadata":
		return "发行信息无效，已停止操作。"
	}
	return "更新来源暂时不可用，请稍后重试。"
}

func responseWriter(out io.Writer) func(bool, string, string, any) int {
	return func(ok bool, code, message string, result any) int {
		err := json.NewEncoder(out).Encode(response{1, ok, code, message, result})
		if err != nil {
			return 1
		}
		if !ok {
			return 1
		}
		return 0
	}
}
