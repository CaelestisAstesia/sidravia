package configcmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

const protocolOverrideFileError = "协议上下文 Override 文件无效或无法读取"

// loadProtocolContextOverride reads and validates one user-owned override file.
// The returned error has a fixed public message while retaining private causes.
func loadProtocolContextOverride(path string, supplied bool) (json.RawMessage, error) {
	if !supplied {
		return nil, nil
	}
	if path == "" || strings.ContainsRune(path, 0) {
		return nil, terminal.Failure(protocolOverrideFileError, errors.New("invalid override file path"))
	}
	file, openErr := os.Open(path)
	if openErr != nil {
		return nil, terminal.Failure(protocolOverrideFileError, openErr)
	}
	return readProtocolContextOverride(file)
}

// readProtocolContextOverride owns one bounded read and exactly one Close.
// It is private to the file-loading operation, not a production injection seam.
func readProtocolContextOverride(file io.ReadCloser) (json.RawMessage, error) {
	data, readErr := io.ReadAll(io.LimitReader(file, contract.MaximumProtocolContextOverrideBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, terminal.Failure(protocolOverrideFileError, errors.Join(readErr, closeErr))
	}
	validated, validateErr := contract.DecodeProtocolContextOverride(data)
	if validateErr != nil {
		return nil, terminal.Failure(protocolOverrideFileError, validateErr)
	}
	return json.RawMessage(validated), nil
}
