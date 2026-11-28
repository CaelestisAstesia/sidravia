package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"

	"sidravia/internal/installation/startgate"
	"sidravia/internal/update"
	"sidravia/internal/update/invocation"
)

var ProductVersion = "0.5.0-alpha.1"
var BuildID = "dev"

func main() {
	if handled, code := setupFinish(os.Args[1:]); handled {
		os.Exit(code)
	}
	release := func() {}
	var err error
	if !invocation.ReadOnly(os.Args[1:]) {
		release, err = startgate.AcquireRunIdentity(ProductVersion, BuildID)
	}
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion": 1, "ok": false, "code": "maintenance_active", "message": "安装或更新正在进行，请稍后重试。"})
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	code := update.Run(ctx, os.Args[1:], ProductVersion, BuildID, os.Stdout)
	release()
	os.Exit(code)
}
