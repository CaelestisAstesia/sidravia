package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"sidravia/internal/installationgate"

	"sidravia/internal/update/command"
)

var ProductVersion = "0.1.0-dev"
var BuildID = "dev"

func main() {
	release, err := installationgate.AcquireStart()
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion": 1, "ok": false, "code": "maintenance_active", "message": "安装或更新正在进行，请稍后重试。"})
		os.Exit(1)
	}
	release()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(command.Run(ctx, os.Args[1:], ProductVersion, BuildID, os.Stdout))
}
