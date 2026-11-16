package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"sidravia/internal/installationgate"

	"sidravia/internal/update/command"
)

var ProductVersion = "0.4.0-alpha.1"
var BuildID = "dev"

func main() {
	release, err := installationgate.AcquireRun()
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion": 1, "ok": false, "code": "maintenance_active", "message": "安装或更新正在进行，请稍后重试。"})
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	code := command.Run(ctx, os.Args[1:], ProductVersion, BuildID, os.Stdout)
	release()
	os.Exit(code)
}
