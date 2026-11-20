//go:build !linux

package main

import "sidravia/internal/launchcontract"

func acquireUserInstance() (func() error, error) {
	return func() error { return nil }, nil
}

func configureServiceSwitch(*launchcontract.Options, defaultPaths) error { return nil }
