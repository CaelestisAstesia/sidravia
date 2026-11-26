//go:build !linux

package main

import (
	"context"

	"sidravia/internal/installation/userservice/registration"
)

func cleanupErasedPackage(context.Context, string) error { return registration.ErrUnavailable }
