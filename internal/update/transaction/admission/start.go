// Package admission grants narrowly scoped startup permission after the
// authoritative execution record has been verified.
package admission

import (
	"os"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/transaction"
)

type Recovery struct {
	operation, token, version, build, namespace, configuration string
}

func RecoveryFromEnvironment(version, build string) Recovery {
	return Recovery{
		operation: os.Getenv(launchcontract.EnvRecoveryOperation),
		token:     os.Getenv(launchcontract.EnvRecoveryToken),
		version:   version, build: build,
		namespace:     os.Getenv(launchcontract.EnvNamespace),
		configuration: os.Getenv(launchcontract.EnvRecoveryConfiguration),
	}
}

func open(target string) (*transaction.Store, error) {
	root, err := productlayout.OperationRoot()
	if err != nil {
		return nil, err
	}
	return transaction.Open(root, target, release.PublisherTrust())
}
