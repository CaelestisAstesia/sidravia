//go:build windows

package shellintegration

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestNativePathTransactionRollsBackOtherKeyAndRetries(t *testing.T) {
	prefix := fmt.Sprintf("Software\\Sidravia-Path-Transaction-Test-%d-%d", os.Getpid(), time.Now().UnixNano())
	environmentName := prefix + "-environment"
	ownershipName := prefix + "-ownership"
	t.Cleanup(func() {
		for _, name := range []string{environmentName, ownershipName} {
			if err := registry.DeleteKey(registry.CURRENT_USER, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Error(err)
			}
		}
	})
	const directory = `C:\Sidravia-Test-Only`
	value := pathValue{text: directory, kind: registry.EXPAND_SZ, exists: true}
	for _, retry := range []bool{false, true} {
		func() {
			transaction, err := startPathTransaction()
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(transaction)
			environment, err := transactedPathKey(transaction, environmentName, true)
			if err != nil {
				t.Fatal(err)
			}
			defer environment.Close()
			ownership, err := transactedPathKey(transaction, ownershipName, true)
			if err != nil {
				t.Fatal(err)
			}
			defer ownership.Close()
			if err = writeTransactedPath(environment, value); err != nil {
				t.Fatal(err)
			}
			if !retry {
				if err = writeTransactedOwnership(ownership, directory, pathUnowned); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("expected the second registry operation to fail, got %v", err)
				}
				return
			}
			if err = writeTransactedOwnership(ownership, directory, pathCreated); err != nil {
				t.Fatal(err)
			}
			if err = finishPathTransaction(transaction); err != nil {
				t.Fatal(err)
			}
		}()
		for _, name := range []string{environmentName, ownershipName} {
			key, err := registry.OpenKey(registry.CURRENT_USER, name, registry.QUERY_VALUE)
			if !retry && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			defer key.Close()
			if !retry {
				t.Fatalf("failed transaction left a registry key: %s", name)
			}
			if name == environmentName {
				if got, err := readPathValue(key); err != nil || got != value {
					t.Fatalf("PATH retry changed expected value: %#v, %v", got, err)
				}
			} else if got, err := readPathOwnership(key, directory); err != nil || got != pathCreated {
				t.Fatalf("PATH retry lost ownership: %v, %v", got, err)
			}
		}
	}
}
