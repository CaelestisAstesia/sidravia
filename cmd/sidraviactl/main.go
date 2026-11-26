package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli"
	"sidravia/internal/installation/shellintegration"
	"sidravia/internal/installation/startgate"
	"sidravia/internal/update/invocation"
)

var (
	ProductVersion = "0.5.0-alpha.1"
	BuildID        = "dev"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "rpm-erase-user-registration" || os.Args[1] == "deb-erase-user-registration") {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		err := cleanupErasedPackage(ctx, strings.TrimSuffix(os.Args[1], "-erase-user-registration"))
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "无法清理已卸载软件包 的用户服务登记。")
			os.Exit(1)
		}
		return
	}
	releaseRun := func() {}
	if (len(os.Args) < 2 || os.Args[1] != "update-internal") && !(len(os.Args) > 2 && os.Args[1] == "update" && invocation.ControlEntry(os.Args[2:])) {
		release, err := startgate.AcquireRunIdentity(ProductVersion, BuildID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "安装或更新正在进行，请完成或取消后再运行命令。")
			os.Exit(1)
		}
		// An updater child takes over coordination; retaining this caller's
		// lease would prevent that child from ever obtaining execution admission.
		if len(os.Args) > 1 && os.Args[1] == "update" && !invocation.CheckInCTL(os.Args[2:]) {
			release()
		} else {
			releaseRun = release
		}
	}
	code := run(os.Args[1:], os.Stderr)
	releaseRun()
	os.Exit(code)
}

func run(args []string, stderr io.Writer) int {
	if len(args) == 2 && (args[0] == "rpm-user-service-ready" || args[0] == "deb-user-service-ready") {
		pid, err := strconv.Atoi(args[1])
		if err != nil || pid <= 0 {
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := daemonctl.CheckPackageServiceIPC(ctx, daemonctl.Identity{ProductVersion: ProductVersion, BuildID: BuildID}, pid, strings.TrimSuffix(args[0], "-user-service-ready")); err != nil {
			return 1
		}
		return 0
	}
	if len(args) == 3 && args[0] == "linux-recover-autostart-stage" && args[1] == "--" {
		if err := shellintegration.RecoverAutostartStaging(args[2]); err != nil {
			fmt.Fprintln(stderr, "无法清理桌面自启暂存目录。")
			return 1
		}
		return 0
	}
	if len(args) == 3 && (args[0] == "rpm-track-autostart" || args[0] == "rpm-untrack-autostart" || args[0] == "deb-track-autostart" || args[0] == "deb-untrack-autostart") && args[1] == "--" {
		manager, operation, _ := strings.Cut(args[0], "-")
		if err := shellintegration.TrackPackageAutostart(args[2], operation == "track-autostart", manager); err != nil {
			fmt.Fprintln(stderr, "无法登记软件包 桌面自启目录。")
			return 1
		}
		return 0
	}
	err := cli.RunWithIdentity(args, ProductVersion, BuildID)
	if err != nil {
		_ = cli.WriteError(stderr, err)
	}
	return cli.ExitCode(err)
}
