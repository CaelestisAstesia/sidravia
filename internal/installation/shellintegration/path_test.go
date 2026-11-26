package shellintegration

import (
	"errors"
	"testing"
)

type memoryPath struct {
	value                        pathValue
	reads, writes, notifications int
	readErr, writeErr, notifyErr error
	verifyErr                    bool
	tamper                       bool
	owned                        pathOwnership
	beforeWrite                  *pathValue
}

func (m *memoryPath) read() (pathValue, error) {
	m.reads++
	if m.readErr != nil {
		return pathValue{}, m.readErr
	}
	if m.reads > 1 && m.verifyErr {
		return pathValue{}, errors.New("read after write")
	}
	if m.reads > 1 && m.tamper {
		return pathValue{text: "changed externally", kind: 1, exists: true}, nil
	}
	return m.value, nil
}
func (m *memoryPath) replace(_ string, before, after pathValue, old, next pathOwnership) error {
	if m.beforeWrite != nil {
		m.value = *m.beforeWrite
	}
	if m.value != before || m.owned != old {
		return errPathChanged
	}
	m.writes++
	if m.writeErr != nil {
		return m.writeErr
	}
	m.value = after
	m.owned = next
	return nil
}
func (m *memoryPath) notify() error                           { m.notifications++; return m.notifyErr }
func (m *memoryPath) ownership(string) (pathOwnership, error) { return m.owned, nil }

func TestExactPATHTransitions(t *testing.T) {
	dir := `C:\Apps\Sidravia`
	for _, tc := range []struct {
		name, before, after string
		op                  Operation
		enabled, changed    bool
	}{
		{"append", `%SystemRoot%;C:\Other`, `%SystemRoot%;C:\Other;C:\Apps\Sidravia`, EnablePath, true, true},
		{"existing", `c:\apps\sidravia\;C:\Other`, `c:\apps\sidravia\;C:\Other`, EnablePath, true, false},
		{"prefix", `C:\Apps\SidraviaOther`, `C:\Apps\SidraviaOther;C:\Apps\Sidravia`, EnablePath, true, true},
		{"keep equivalent user entry", `;C:\Apps\Sidravia;C:\Other;;c:\apps\sidravia\;`, `;C:\Other;;c:\apps\sidravia\;`, DisablePath, true, true},
		{"missing", `C:\Other;;`, `C:\Other;;`, DisablePath, false, false},
		{"trailing separator", `C:\Other;`, `C:\Other;C:\Apps\Sidravia;`, EnablePath, true, true},
		{"status", `C:\Apps\Sidravia`, `C:\Apps\Sidravia`, Status, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &memoryPath{value: pathValue{text: tc.before, kind: 1, exists: true}}
			if tc.op == DisablePath {
				m.owned = pathAppended
			}
			r, err := operate(m, dir, tc.op)
			if err != nil || !r.OK || r.PathEnabled != tc.enabled || r.Changed != tc.changed || m.value.text != tc.after || m.value.kind != 1 {
				t.Fatalf("result=%+v value=%+v err=%v", r, m.value, err)
			}
			writes := m.writes
			if _, err := operate(m, dir, tc.op); err != nil || m.writes != writes {
				t.Fatalf("not idempotent: %v", err)
			}
		})
	}
}

func TestPathDisablePreservesExistingAndLaterEdits(t *testing.T) {
	dir := `C:\Apps\Sidravia`
	for _, initial := range []string{"", ";", ";;", `C:\Other`, `C:\Other;`, `%SystemRoot%;;`} {
		if got := edit(edit(initial, dir, true), dir, false); got != initial {
			t.Fatalf("PATH round trip for %q returned %q", initial, got)
		}
	}
	for _, kind := range []uint32{1, 2} {
		m := &memoryPath{value: pathValue{text: `C:\Other;` + dir + `;;C:\Added;`, kind: kind, exists: true}, owned: pathAppended}
		if _, err := operate(m, dir, DisablePath); err != nil || m.value.text != `C:\Other;;C:\Added;` || m.value.kind != kind || m.owned != pathUnowned {
			t.Fatalf("disable changed a later PATH edit or lost type: %+v, %v", m, err)
		}
	}
	m := &memoryPath{value: pathValue{text: dir + `;C:\Other;`, kind: 2, exists: true}}
	if _, err := operate(m, dir, EnablePath); err != nil || m.owned != pathUnowned {
		t.Fatalf("pre-existing matching entry was adopted: %+v, %v", m, err)
	}
	r, err := operate(m, dir, DisablePath)
	if err != nil || !r.PathEnabled || r.Changed || r.Warning != "path_entry_not_owned" || m.writes != 0 {
		t.Fatalf("pre-existing matching entry was removed: %+v, %v", r, err)
	}
	late := pathValue{text: `C:\Other;` + dir + `;C:\Added;`, kind: 2, exists: true}
	m = &memoryPath{value: pathValue{text: `C:\Other;` + dir + `;`, kind: 2, exists: true}, owned: pathAppended, beforeWrite: &late}
	if _, err := operate(m, dir, DisablePath); Code(err) != "path_verify_failed" || m.value != late || m.owned != pathAppended || m.writes != 0 {
		t.Fatalf("concurrent PATH append was lost: %+v, %v", m, err)
	}
}

func TestPathAndOwnershipFailedWriteCanRetry(t *testing.T) {
	dir := `C:\Apps\Sidravia`
	failure := errors.New("registry transaction failed")
	m := &memoryPath{writeErr: failure}
	if _, err := operate(m, dir, EnablePath); !errors.Is(err, failure) || m.value.exists || m.owned != pathUnowned {
		t.Fatalf("failed enable left a partial registry change: %+v, %v", m, err)
	}
	m.writeErr = nil
	if _, err := operate(m, dir, EnablePath); err != nil || m.value.text != dir || m.owned != pathCreated {
		t.Fatalf("enable could not retry: %+v, %v", m, err)
	}
	m.writeErr = failure
	if _, err := operate(m, dir, DisablePath); !errors.Is(err, failure) || m.value.text != dir || m.owned != pathCreated {
		t.Fatalf("failed disable left a partial registry change: %+v, %v", m, err)
	}
	m.writeErr = nil
	if _, err := operate(m, dir, DisablePath); err != nil || m.value.exists || m.owned != pathUnowned {
		t.Fatalf("disable could not restore the originally absent PATH: %+v, %v", m, err)
	}
	m = &memoryPath{value: pathValue{text: dir, kind: pathExpandable, exists: true}, owned: pathCreated, writeErr: failure}
	if _, err := operate(m, dir, DisablePath); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	m.writeErr = nil
	m.value.text += `;C:\Later;;`
	if _, err := operate(m, dir, DisablePath); err != nil || !m.value.exists || m.value.text != `C:\Later;;` || m.owned != pathUnowned {
		t.Fatalf("disable retry clobbered a later user edit: %+v, %v", m, err)
	}
}

func TestRegistryTypesAndFailures(t *testing.T) {
	for _, v := range []pathValue{{}, {text: "", kind: 1, exists: true}, {text: "", kind: 2, exists: true}} {
		m := &memoryPath{value: v}
		r, err := operate(m, `C:\Sidravia`, EnablePath)
		wantKind := v.kind
		if !v.exists {
			wantKind = 2
		}
		if err != nil || !r.OK || m.value.kind != wantKind {
			t.Fatalf("kind=%d err=%v", m.value.kind, err)
		}
	}
	cause := errors.New("private native error")
	for _, tc := range []struct {
		name, code string
		m          memoryPath
	}{
		{"read", "path_read_failed", memoryPath{readErr: cause}},
		{"write", "path_write_failed", memoryPath{writeErr: cause}},
		{"verify read", "path_verify_failed", memoryPath{verifyErr: true}},
		{"verify concurrent write", "path_verify_failed", memoryPath{tamper: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := operate(&tc.m, `C:\Sidravia`, EnablePath)
			if err == nil || r.OK || Code(err) != tc.code {
				t.Fatalf("%+v %v", r, err)
			}
			if tc.m.readErr != nil || tc.m.writeErr != nil {
				if !errors.Is(err, cause) {
					t.Fatal("lost cause")
				}
			}
			if tc.m.writes > 1 || tc.m.notifications != 0 {
				t.Fatal("unexpected compensating write or notification")
			}
		})
	}
	m := &memoryPath{notifyErr: cause}
	r, err := operate(m, `C:\Sidravia`, EnablePath)
	if err != nil || !r.OK || !r.Changed || r.Warning != "environment_notification_failed" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestInvalidInputDoesNotRead(t *testing.T) {
	for _, tc := range []struct {
		dir string
		op  Operation
	}{{"", EnablePath}, {"C:\\bad;entry", EnablePath}, {"C:\\ok", Operation("repair")}} {
		m := &memoryPath{}
		r, err := operate(m, tc.dir, tc.op)
		if err == nil || r.OK || m.reads != 0 {
			t.Fatalf("%+v %v reads=%d", r, err, m.reads)
		}
	}
}
