package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sidravia/internal/productlayout"
)

func TestGUILogDirectoryUsesNamespaceLayoutAndOnlyOpensDirectory(t *testing.T) {
	ns, err := productlayout.NewNamespace("manual-test")
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	opened := false
	deps := guiLogDirectoryDependencies{
		namespace: func() (productlayout.Namespace, error) { return ns, nil },
		resolve: func(got productlayout.Namespace) (productlayout.Layout, error) {
			if got != ns {
				t.Fatal("wrong namespace")
			}
			return productlayout.Layout{DaemonLogPath: filepath.Join(path, "sidraviad.log")}, nil
		},
		stat: os.Stat, open: func(got string) error {
			if got != path {
				t.Fatalf("directory=%q", got)
			}
			opened = true
			return nil
		},
	}
	if err := openGUILogDirectoryWith(deps); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("not opened")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("operation changed directory")
	}
}
func TestGUILogDirectoryFailuresPreserveCauseAndNeverLeakPath(t *testing.T) {
	cause := errors.New("secret raw path")
	for _, kind := range []string{"namespace", "layout", "relative", "missing", "file", "open"} {
		t.Run(kind, func(t *testing.T) {
			path := t.TempDir()
			opened := false
			deps := guiLogDirectoryDependencies{namespace: func() (productlayout.Namespace, error) { return productlayout.Namespace{}, nil }, resolve: func(productlayout.Namespace) (productlayout.Layout, error) {
				return productlayout.Layout{DaemonLogPath: filepath.Join(path, "sidraviad.log")}, nil
			}, stat: os.Stat, open: func(string) error { opened = true; return cause }}
			switch kind {
			case "namespace":
				deps.namespace = func() (productlayout.Namespace, error) { return productlayout.Namespace{}, cause }
				deps.resolve = func(productlayout.Namespace) (productlayout.Layout, error) {
					t.Fatal("invalid namespace resolved")
					return productlayout.Layout{}, nil
				}
			case "layout":
				deps.resolve = func(productlayout.Namespace) (productlayout.Layout, error) { return productlayout.Layout{}, cause }
			case "relative":
				deps.resolve = func(productlayout.Namespace) (productlayout.Layout, error) {
					return productlayout.Layout{DaemonLogPath: "logs/file"}, nil
				}
			case "missing":
				deps.stat = func(string) (os.FileInfo, error) { return nil, cause }
			case "file":
				file := filepath.Join(path, "file")
				if err := os.WriteFile(file, nil, 0600); err != nil {
					t.Fatal(err)
				}
				deps.stat = func(string) (os.FileInfo, error) { return os.Stat(file) }
			}
			err := openGUILogDirectoryWith(deps)
			if err == nil {
				t.Fatal("accepted failure")
			}
			if opened != (kind == "open") {
				t.Fatal("unexpected opener call")
			}
			if kind == "namespace" || kind == "layout" || kind == "missing" || kind == "open" {
				if !errors.Is(err, cause) {
					t.Fatal("lost cause")
				}
			}
			var out bytes.Buffer
			if err := WriteError(&out, err); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "secret raw path") || strings.Contains(out.String(), path) {
				t.Fatal("raw path leaked")
			}
		})
	}
}
func TestGUILogDirectoryRejectsArgumentsBeforeNativeOperation(t *testing.T) {
	c := newGUILogDirectoryCommand()
	if !c.Hidden {
		t.Fatal("command visible")
	}
	if err := c.Args(c, []string{"C:\\other"}); err == nil {
		t.Fatal("path accepted")
	}
	if runtime.GOOS != "windows" {
		if err := openGUILogDirectory(); !errors.Is(err, errLogDirectoryUnsupported) {
			t.Fatalf("unsupported=%v", err)
		}
	}
}
