package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/document/securefile"
	"sidravia/internal/ipc/contract"
)

// configurationPage uses the ordered catalog snapshot and its opaque edit
// versions. A changed catalog invalidates the cursor rather than returning a
// mixed generation. No per-client snapshot or cursor is retained by the daemon.
func configurationPage(values []contract.ConfigurationResult, protection securefile.ProtectionStatus, cursor string) (json.RawMessage, error) {
	digest := sha256.New()
	fmt.Fprintln(digest, protection)
	for _, value := range values {
		fmt.Fprintln(digest, value.ConfigurationID, value.Version)
	}
	snapshot := hex.EncodeToString(digest.Sum(nil))
	start := 0
	if cursor != "" {
		version, offset, ok := strings.Cut(cursor, ":")
		index, err := strconv.Atoi(offset)
		if !ok || err != nil || index <= 0 {
			return nil, NewResolutionFailure(InvalidConfiguration, nil)
		}
		if version != snapshot || index >= len(values) {
			return nil, config.ErrVersionConflict
		}
		start = index
	}
	// Leave room for the result and response envelopes. A single oversized
	// legacy row still gets its own bounded configuration response.
	const pageBudget = 48 * 1024
	end, size := start, 0
	for end < len(values) {
		row, err := json.Marshal(values[end])
		if err != nil {
			return nil, err
		}
		if end > start && size+len(row)+1 > pageBudget {
			break
		}
		size += len(row) + 1
		end++
	}
	result := contract.ConfigurationListResult{
		StorageProtection: string(protection), Configurations: values[start:end],
	}
	if end < len(values) {
		result.NextCursor = snapshot + ":" + strconv.Itoa(end)
	}
	return contract.MarshalConfigurationListResult(result)
}
