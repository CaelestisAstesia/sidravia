package main

import (
	"context"
	"fmt"
	"os"
)

// Set via ldflags.
var (
	ProductVersion = "0.1.0-dev"
	BuildID        = "dev"
)

func main() {
	rt, err := constructProductionSystem(context.Background())
	if err != nil {
		productionExit(err)
	}

	if err := rt.run(context.Background()); err != nil {
		productionExit(err)
	}
}

func productionExit(err error) {
	fmt.Fprintf(os.Stderr, "sidraviad: %v\n", err)
	os.Exit(1)
}
