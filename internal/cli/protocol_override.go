package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/spf13/pflag"

	"sidravia/internal/ipc/contract"
)

const protocolOverrideFileError = "协议上下文 Override 文件无效或无法读取"

// loadProtocolContextOverride reads and validates one user-owned override file.
// The returned error has a fixed public message while retaining private causes.
func loadProtocolContextOverride(path string, supplied bool) (json.RawMessage, error) {
	if !supplied {
		return nil, nil
	}
	if path == "" || containsNUL(path) {
		return nil, wrapSafeOperation(protocolOverrideFileError, errors.New("invalid override file path"))
	}
	file, openErr := os.Open(path)
	if openErr != nil {
		return nil, wrapSafeOperation(protocolOverrideFileError, openErr)
	}
	return readProtocolContextOverride(file)
}

// readProtocolContextOverride owns one bounded read and exactly one Close.
// It is private to the file-loading operation, not a production injection seam.
func readProtocolContextOverride(file io.ReadCloser) (json.RawMessage, error) {
	data, readErr := io.ReadAll(io.LimitReader(file, contract.MaximumProtocolContextOverrideBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, wrapSafeOperation(protocolOverrideFileError, errors.Join(readErr, closeErr))
	}
	validated, validateErr := contract.DecodeProtocolContextOverride(data)
	if validateErr != nil {
		return nil, wrapSafeOperation(protocolOverrideFileError, validateErr)
	}
	return json.RawMessage(validated), nil
}

func containsNUL(value string) bool {
	for _, character := range value {
		if character == 0 {
			return true
		}
	}
	return false
}

type protocolOverridePathFlag struct {
	value    string
	set      bool
	target   *string
	supplied *bool
}

func (flag *protocolOverridePathFlag) Set(value string) error {
	if flag.set {
		return errCommandUsage
	}
	flag.value = value
	flag.set = true
	if flag.target != nil {
		*flag.target = value
	}
	if flag.supplied != nil {
		*flag.supplied = true
	}
	return nil
}

func (flag *protocolOverridePathFlag) String() string { return flag.value }
func (flag *protocolOverridePathFlag) Type() string   { return "string" }

func addProtocolOverrideFileFlag(flags *pflag.FlagSet, value *string, supplied *bool) {
	flag := &protocolOverridePathFlag{target: value, supplied: supplied}
	flags.Var(flag, "protocol-override-file", "从严格 JSON 对象文件读取协议上下文 Override；保存后结束旧 Session，不会自动连接")
	flags.Lookup("protocol-override-file").NoOptDefVal = ""
}
