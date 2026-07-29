package jsonfile

import "errors"

type ProtectionStatus string

const (
	ProtectionProtected   ProtectionStatus = "protected"
	ProtectionUnprotected ProtectionStatus = "unprotected"
)

var (
	ProtectionUnsupported                  = errors.New("storage protection unsupported")
	ErrInsecureStorageConfirmationRequired = errors.New("insecure storage confirmation required")
)
