//go:build !linux

package main

import (
	"context"

	"sidravia/internal/installation/userservice/registration"
)

func cleanupErasedRPM(context.Context) error { return registration.ErrUnavailable }
