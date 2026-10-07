package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

func newDiagnosticsExportCommand(operation func(string) error) *cobra.Command {
	var outputPath string
	command := &cobra.Command{
		Use:   "export",
		Short: "写入新的诊断文件",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("output") || !validDiagnosticsExportOutputPath(outputPath) {
				return errCommandUsage
			}
			return wrapCommandOperation(operation(outputPath))
		},
	}
	command.Flags().StringVar(&outputPath, "output", "", "新诊断文件路径")
	return command
}

func runDiagnosticsExport(deps listDependencies, path string) error {
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		response, err := callList(connection, deps.connection.callTimeout, contract.MethodDiagnosticsExport)
		if err != nil {
			return wrapSafeOperation("导出诊断信息失败", err)
		}
		result, err := contract.DecodeDiagnosticsExportResult(response)
		if err != nil {
			return wrapSafeOperation("解码诊断信息失败", err)
		}
		artifact, err := contract.MarshalDiagnosticsExportResult(result)
		if err != nil {
			return wrapSafeOperation("编码诊断信息失败", err)
		}
		if len(artifact) > contract.MaximumDiagnosticExportBytes {
			return wrapSafeOperation("诊断文件超过大小限制", errors.New("diagnostic export exceeds size limit"))
		}
		artifact = append(artifact, '\n')

		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return wrapSafeOperation("无法创建诊断文件", err)
		}
		if err := writeOwnedArtifact(file, artifact); err != nil {
			return wrapSafeOperation("诊断文件写入失败，文件可能不完整", err)
		}

		message := "诊断信息已写入：" + sanitizeDynamicText(path) + "\n"
		if err := newPresentation(deps.stdout).complete(message); err != nil {
			return wrapSafeOperation("文件已写入，但提示输出失败", err)
		}
		return nil
	})
}

func diagnosticsExport(identity clientbootstrap.Identity, path string) error {
	return runDiagnosticsExport(defaultListDependencies(identity), path)
}

func writeOwnedArtifact(file io.WriteCloser, artifact []byte) error {
	written, writeErr := file.Write(artifact)
	if writeErr == nil && (written < 0 || written != len(artifact)) {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func validDiagnosticsExportOutputPath(path string) bool {
	return path != "" && strings.IndexByte(path, 0) < 0
}
