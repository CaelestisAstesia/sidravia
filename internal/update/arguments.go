package update

import (
	"flag"
	"io"

	"sidravia/internal/release"
)

type arguments struct {
	channel, kind, candidateID, operationID string
	confirmed                               bool
}

func parseArguments(args []string, version string) (arguments, string) {
	flags := flag.NewFlagSet("updater", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	defaultChannel := "stable"
	if v, e := release.ParseVersion(version); e == nil && v.Prerelease() {
		defaultChannel = "preview"
	}
	channel := flags.String("channel", defaultChannel, "")
	kind := flags.String("package", "", "")
	candidateID := flags.String("candidate", "", "")
	confirmed := flags.Bool("confirm", false, "")
	operationID := flags.String("operation", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return arguments{}, "更新参数无效。"
	}
	if *channel != "stable" && *channel != "preview" {
		return arguments{}, "更新通道无效。"
	}

	return arguments{channel: *channel, kind: *kind, candidateID: *candidateID, operationID: *operationID, confirmed: *confirmed}, ""
}
