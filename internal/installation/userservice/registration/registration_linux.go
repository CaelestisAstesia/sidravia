//go:build linux

package registration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	productlayout "sidravia/internal/installation/layout"
)

const registrationPrefix = "# Sidravia registration v1 "

// UnitPath returns the account-owned user unit path.
func UnitPath() (string, error) {
	home, err := productlayout.AccountHome(os.Geteuid())
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) || os.Geteuid() == 0 {
		return "", ErrUnavailable
	}
	return filepath.Join(home, ".config", "systemd", "user", UnitName), nil
}

// Encode renders the canonical registration header and systemd unit together.
func Encode(layout productlayout.Layout) ([]byte, error) {
	body, err := renderUserUnit(layout)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(layout)
	if err != nil {
		return nil, err
	}
	return append([]byte(registrationPrefix+base64.RawStdEncoding.EncodeToString(data)+"\n"), body...), nil
}

// Read checks ownership and canonical bytes before returning a registration.
func Read(path string) (productlayout.Layout, []byte, error) {
	var layout productlayout.Layout
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return layout, nil, nil
	}
	if err != nil {
		return layout, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return layout, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > 16384 {
		return layout, nil, ErrConflict
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return layout, nil, err
	}
	line, _, _ := bytes.Cut(data, []byte("\n"))
	encoded, ok := strings.CutPrefix(string(line), registrationPrefix)
	if !ok {
		return layout, nil, ErrConflict
	}
	fields, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(fields, &layout) != nil || !filepath.IsAbs(layout.ExecutableDirectory) {
		return productlayout.Layout{}, nil, ErrConflict
	}
	expected, err := Encode(layout)
	if err != nil || !bytes.Equal(data, expected) {
		return productlayout.Layout{}, nil, ErrConflict
	}
	return layout, data, nil
}

// Lookup reads this account’s registration without contacting systemd.
func Lookup() (productlayout.Layout, bool, error) {
	if os.Geteuid() == 0 {
		return productlayout.Layout{}, false, nil
	}
	path, err := UnitPath()
	if err != nil {
		return productlayout.Layout{}, false, err
	}
	layout, data, err := Read(path)
	return layout, data != nil, err
}
