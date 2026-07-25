package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"sidravia/internal/daemon/app"
	"sidravia/internal/daemon/host"
	"sidravia/internal/ipc/server"
)

// Set via ldflags.
var (
	ProductVersion = "0.1.0-dev"
	BuildID        = "dev"
)

func main() {
	token, err := host.GenerateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sidraviad: failed to generate token: %v\n", err)
		os.Exit(1)
	}

	handler := app.StatusHandler(ProductVersion, BuildID)
	srv := server.NewServer(token, BuildID, handler)

	infoPath, err := host.DefaultRuntimeInfoPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sidraviad: failed to get runtime info path: %v\n", err)
		os.Exit(1)
	}

	cfg := host.Config{
		ProductVersion:  ProductVersion,
		BuildID:         BuildID,
		Token:           token,
		RuntimeInfoPath: infoPath,
		Handler:         http.HandlerFunc(srv.ServeHTTP),
	}

	if err := host.Run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "sidraviad: %v\n", err)
		os.Exit(1)
	}
}
