//go:build !linux

package main

import launchcontract "sidravia/internal/background/launch"

func acquireUserInstance() (func() error, error) {
	return func() error { return nil }, nil
}

func configureServiceSwitch(*launchcontract.Options, defaultPaths) error { return nil }
