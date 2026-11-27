package release

import (
	"sidravia/internal/document/jsondoc"
	packagefiles "sidravia/internal/release/files"
)

const (
	QtCoreComponent = "org.sidravia.core"
	QtGUIComponent  = "org.sidravia.gui"
)

// QtRepositoryIndex is signed as a whole. Both the release client and the
// maintenance tool check its identity before handing files to Qt.
type QtRepositoryIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	Product       string            `json:"product"`
	Target        string            `json:"target"`
	Profile       string            `json:"profile"`
	Version       string            `json:"version"`
	Files         map[string]string `json:"files"`
}

func DecodeQtRepositoryIndex(data []byte) (QtRepositoryIndex, error) {
	var index QtRepositoryIndex
	if len(data) > ReleaseManifestLimit || jsondoc.DecodeStrict(data, &index) != nil ||
		index.SchemaVersion != 1 || index.Product != "Sidravia" ||
		(index.Target != "windows-amd64" && index.Target != "windows-arm64") ||
		(index.Profile != "core" && index.Profile != "gui") ||
		!packagefiles.ValidSHA256(index.Files["Updates.xml"]) {
		return QtRepositoryIndex{}, ErrMetadata
	}
	if _, err := ParseVersion(index.Version); err != nil {
		return QtRepositoryIndex{}, ErrMetadata
	}
	for name, hash := range index.Files {
		if !packagefiles.SafePath(name) || !packagefiles.ValidSHA256(hash) {
			return QtRepositoryIndex{}, ErrMetadata
		}
	}
	return index, nil
}
