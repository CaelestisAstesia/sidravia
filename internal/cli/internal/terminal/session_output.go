package terminal

import (
	"io"
	"sidravia/internal/ipc/contract"
)

func WriteSessionRemoveResult(output io.Writer, result contract.SessionRemoveResult) error {
	p := NewPresentation(output)
	return Failure("写入 Session 删除响应", p.Complete(RenderSessionRemoved(&result)))
}

func validateSessionResult(value contract.SessionResult) error {
	_, err := contract.MarshalSessionResult(value)
	return err
}

func WriteSessionResult(output io.Writer, result contract.SessionResult) error {
	if err := validateSessionResult(result); err != nil {
		return Failure("渲染 Session 响应", err)
	}
	p := NewPresentation(output)
	return Failure("写入 Session 响应", p.Complete(RenderSessionDetail(p, &result)))
}

func WriteSessionStartResult(output io.Writer, result contract.SessionStartResult) error {
	if err := validateSessionResult(result.Session); err != nil {
		return Failure("渲染 Session 启动响应", err)
	}
	p := NewPresentation(output)
	return Failure("写入 Session 启动响应", p.Complete(RenderSessionStartResult(p, &result)))
}

func WriteSessionList(output io.Writer, result contract.SessionListResult) error {
	p := NewPresentation(output)
	return Failure("写入 Session 列表", p.Complete(RenderSessionList(p, &result)))
}

func WriteProfileList(output io.Writer, result contract.ProfileListResult) error {
	p := NewPresentation(output)
	return Failure("写入 Profile 列表", p.Complete(RenderProfileList(p, &result)))
}
