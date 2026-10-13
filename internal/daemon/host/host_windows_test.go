//go:build windows

package host

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"sidravia/internal/productlayout"
	"testing"
	"time"
)

func TestWindowsSingletonContentionIsTyped(t *testing.T) {
	token := windows.GetCurrentProcessToken()
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		t.Fatal(e)
	}
	ns, e := productlayout.NewNamespace(fmt.Sprintf("review-singleton-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if e != nil {
		t.Fatal(e)
	}
	name, e := windows.UTF16PtrFromString(daemonMutexName(user.User.Sid.String(), ns))
	if e != nil {
		t.Fatal(e)
	}
	held, e := windows.CreateMutex(nil, false, name)
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(held)
	ready := false
	e = Run(context.Background(), Config{Namespace: ns, RuntimeInfoPath: t.TempDir() + "/unused-runtime.json", OnReady: func() { ready = true }})
	if ready {
		t.Fatal("nonowner signaled readiness")
	}
	if !errors.Is(e, ErrAlreadyRunning) {
		t.Fatalf("singleton contention=%v", e)
	}
}
