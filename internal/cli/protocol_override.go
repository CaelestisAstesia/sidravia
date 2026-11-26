package cli

import (
	"github.com/spf13/pflag"
)

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
