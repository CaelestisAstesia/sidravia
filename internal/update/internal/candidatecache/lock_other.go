//go:build !linux && !darwin && !windows

package candidatecache

import "errors"

func lockCache(string) (func(), error) { return nil, errors.New("update locking unsupported") }
