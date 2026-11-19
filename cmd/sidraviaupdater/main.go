package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"

	"sidravia/internal/installationgate"
	"sidravia/internal/update/command"
	"sidravia/internal/update/entry"
)

var ProductVersion = "0.5.0-alpha.1"
var BuildID = "dev"

func main() {
	if handled, code := portableExecute(os.Args[1:]); handled {
		os.Exit(code)
	}
	if handled, code := setupFinish(os.Args[1:]); handled {
		os.Exit(code)
	}
	release := func() {}
	var err error
	if !entry.ControlEntry(os.Args[1:]) {
		release, err = installationgate.AcquireRunIdentity(ProductVersion, BuildID)
	}
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion": 1, "ok": false, "code": "maintenance_active", "message": "安装或更新正在进行，请稍后重试。"})
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	code := command.RunWithAdmission(ctx, os.Args[1:], ProductVersion, BuildID, os.Stdout, release)
	release()
	os.Exit(code)
}
