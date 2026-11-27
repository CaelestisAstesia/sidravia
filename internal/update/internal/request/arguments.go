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
	manager, core := productlayout.SystemPackage(layout)
	if manager != "" {
		label := "RPM"
		if manager == "deb" {
			label = "DEB"
		}
		if !check {
			return layout, "", true, &Failure{"system_package_managed", "此安装由 " + label + " 管理，请使用系统包管理器更新。", nil}
		}
		if kind != "" {
			return layout, "", true, &Failure{"invalid_arguments", label + " 的更新包类型由当前安装确定。", nil}
		}
		kind = "gui-" + manager
		if core {
			kind = "core-" + manager
		}
	} else {
		if layout.Mode == productlayout.ModePortable && !check {
			return layout, "", false, PortableDownloadOnly()
		}
		if kind == "" {
			kind = DefaultPackageKind(runtime.GOOS, runtime.GOARCH, layout)
		}
		if !check && (runtime.GOOS != "windows" || kind != "setup") {
			return layout, "", false, &Failure{"apply_unavailable", "当前安装不支持应用内安装更新。", nil}
		}
		if kind != "setup" && kind != "gui-portable" && kind != "core-portable" {
			return layout, "", false, &Failure{"invalid_arguments", "更新包类型无效。", nil}
		}
	}
	return layout, kind, manager != "", nil
}

func PortableDownloadOnly() *Failure {
	return &Failure{"portable_download_only", "便携版请从发行页下载新版，并解包到新目录。", nil}
}

func ApplyPlatformSupported() bool {
	return release.DesktopArchitecture(runtime.GOARCH) && runtime.GOOS == "windows"
}

func ValidApplyTarget(layout productlayout.Layout, kind string) bool {
	return runtime.GOOS == "windows" && layout.Mode == productlayout.ModeInstalled && kind == "setup"
}
