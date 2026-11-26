package request

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
)

type response struct {
	SchemaVersion int    `json:"schemaVersion"`
	OK            bool   `json:"ok"`
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
	Result        any    `json:"result,omitempty"`
}

func SourceCode(err error) string {
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
	case errors.Is(err, release.ErrUntrusted):
		return "publisher_untrusted"
	case errors.Is(err, release.ErrMetadata):
		return "invalid_metadata"
	case errors.Is(err, release.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, release.ErrVersion):
		return "unknown_current_version"
	}
	return "source_unavailable"
}
func SourceMessage(err error) string {
	switch SourceCode(err) {
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

func Writer(out io.Writer) func(bool, string, string, any) int {
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

type Failure struct {
	Code    string
	Message string
	Result  any
}

func (f *Failure) Write(out io.Writer) int {
	return Writer(out)(false, f.Code, f.Message, f.Result)
}

type Outcome struct {
	OK      bool
	Code    string
	Message string
	Result  any
}

func (o Outcome) Write(out io.Writer) int {
	return Writer(out)(o.OK, o.Code, o.Message, o.Result)
}

func Fail(code, message string, result any) Outcome {
	return Outcome{false, code, message, result}
}

func Success(result any) Outcome {
	return Outcome{true, "ok", "", result}
}
