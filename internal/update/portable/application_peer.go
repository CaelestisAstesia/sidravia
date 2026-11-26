//go:build linux || windows

package portable

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"sidravia/internal/installation/change"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/productlayout"
)

type applicationPeer struct {
	connection  *client.Client
	daemon, gui *applicationProcess
	info        contract.RuntimeInfo
	status      contract.StatusResult
}

func (p *applicationPeer) close() {
	if p == nil {
		return
	}
	if p.connection != nil {
		p.connection.Close()
	}
	if p.daemon != nil {
		p.daemon.Close()
	}
	if p.gui != nil {
		p.gui.Close()
	}
}
func call(ctx context.Context, c *client.Client, method string, input any, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := c.Call(bounded, method, data)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return &peerFailure{response.Error}
	}
	if output != nil {
		return json.Unmarshal(response.Result, output)
	}
	return nil
}
func probeApplication(ctx context.Context, layout productlayout.Layout, identity change.Target) (peer *applicationPeer, err error) {
	file, err := os.Open(layout.RuntimeInfoPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	closeErr := file.Close()
	err = errors.Join(err, closeErr)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, ErrInvalid
	}
	info, err := contract.DecodeRuntimeInfo(data)
	if err != nil {
		return nil, err
	}
	if info.ProductVersion != identity.Version || info.BuildID != identity.BuildID {
		return nil, ErrChanged
	}
	p := &applicationPeer{info: info}
	defer func() {
		if err != nil {
			p.close()
		}
	}()
	p.daemon, err = observeExecutable(info.PID, filepath.Join(identity.ID, daemonName()))
	if err != nil {
		return nil, err
	}
	p.connection, err = client.Connect(ctx, info.Endpoint, info.Token, identity.BuildID)
	if err != nil {
		return nil, err
	}
	if err = call(ctx, p.connection, contract.MethodDaemonStatus, struct{}{}, &p.status); err != nil {
		return nil, err
	}
	if p.status.PID != info.PID || p.status.ProductVersion != identity.Version || p.status.BuildID != identity.BuildID {
		return nil, ErrChanged
	}
	if p.status.Mode == "desktop" {
		if p.status.DesktopOwnerPID == nil {
			return nil, ErrChanged
		}
		p.gui, err = observeExecutable(*p.status.DesktopOwnerPID, guiRuntime(identity.ID))
		if err != nil {
			return nil, err
		}
	} else if (p.status.Mode != "headless" && p.status.Mode != "systemd") || p.status.DesktopOwnerPID != nil {
		return nil, ErrChanged
	}
	return p, nil
}
func waitPeers(ctx context.Context, p *applicationPeer) error {
	if p == nil {
		return nil
	}
	if p.gui != nil {
		if err := p.gui.Wait(ctx); err != nil {
			return err
		}
	}
	return p.daemon.Wait(ctx)
}

type peerFailure struct{ reply *contract.Error }

func (e *peerFailure) Error() string { return "update lifecycle peer rejected the request" }
