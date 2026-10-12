package installationmanager

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
func (m *memoryPath) write(v pathValue) error {
	m.writes++
	if m.writeErr != nil {
		return m.writeErr
	}
	m.value = v
	return nil
}
func (m *memoryPath) notify() error { m.notifications++; return m.notifyErr }

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
		{"remove duplicates", `;C:\Apps\Sidravia;C:\Other;;c:\apps\sidravia\;`, `;C:\Other;;`, DisablePath, false, true},
		{"missing", `C:\Other;;`, `C:\Other;;`, DisablePath, false, false},
		{"trailing separator", `C:\Other;`, `C:\Other;C:\Apps\Sidravia`, EnablePath, true, true},
		{"status", `C:\Apps\Sidravia`, `C:\Apps\Sidravia`, Status, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &memoryPath{value: pathValue{text: tc.before, kind: 1, exists: true}}
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
