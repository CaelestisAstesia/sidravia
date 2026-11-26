package request

import (
	"flag"
	"io"
	"runtime"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
)

type Options struct {
	Channel, Kind, CandidateID, OperationID string
	Confirmed                               bool
}

func Parse(args []string, version string) (Options, string) {
	flags := flag.NewFlagSet("updater", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	defaultChannel := "stable"
	if v, e := release.ParseVersion(version); e == nil && v.Prerelease() {
		defaultChannel = "preview"
	}
	channel := flags.String("channel", defaultChannel, "")
	kind := flags.String("package", "", "")
	candidateID := flags.String("candidate", "", "")
	confirmed := flags.Bool("confirm", false, "")
	operationID := flags.String("operation", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return Options{}, "更新参数无效。"
	}
	if *channel != "stable" && *channel != "preview" {
		return Options{}, "更新通道无效。"
	}

	return Options{Channel: *channel, Kind: *kind, CandidateID: *candidateID, OperationID: *operationID, Confirmed: *confirmed}, ""
}

// Resolve selects the package type for the calling installation.
func Resolve(kind string, check bool) (productlayout.Layout, string, bool, *Failure) {
	layout, err := productlayout.Resolve()
	if err != nil {
		return layout, "", false, &Failure{"invalid_target", "无法确认当前程序布局。", nil}
	}
	rpm := runtime.GOOS == "linux" && productlayout.RPMManaged(layout)
	if rpm {
		if !check {
			return layout, "", true, &Failure{"system_package_managed", "此安装由 RPM 管理，请使用系统包管理器更新。", nil}
		}
		if kind != "" {
			return layout, "", true, &Failure{"invalid_arguments", "RPM 的更新包类型由当前安装确定。", nil}
		}
		kind = "gui-rpm"
		if productlayout.RPMCore(layout) {
			kind = "core-rpm"
		}
	} else {
		if kind == "" {
			kind = DefaultPackageKind(runtime.GOOS, runtime.GOARCH, layout)
		}
		if kind != "setup" && kind != "gui-portable" && kind != "core-portable" {
			return layout, "", false, &Failure{"invalid_arguments", "更新包类型无效。", nil}
		}
	}
	return layout, kind, rpm, nil
}

func ApplyPlatformSupported() bool {
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") ||
		runtime.GOOS == "windows" && runtime.GOARCH == "amd64"
}

func ValidApplyTarget(layout productlayout.Layout, kind string) bool {
	switch runtime.GOOS {
	case "linux":
		return layout.Mode == productlayout.ModePortable && kind == DefaultPackageKind(runtime.GOOS, runtime.GOARCH, layout)
	case "windows":
		return layout.Mode == productlayout.ModeInstalled && kind == "setup" ||
			layout.Mode == productlayout.ModePortable && kind == DefaultPackageKind(runtime.GOOS, runtime.GOARCH, layout)
	default:
		return false
	}
}
