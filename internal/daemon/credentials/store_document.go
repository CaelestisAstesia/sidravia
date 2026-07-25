package credentials

import (
	"fmt"
	"sort"

	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type storeDocumentInput struct {
	SchemaVersion *uint64                      `json:"schemaVersion"`
	Credentials   *[]persistentCredentialInput `json:"credentials"`
}

type persistentCredentialInput struct {
	CredentialID *CredentialID `json:"credentialId"`
	Username     *string       `json:"username"`
	Password     *string       `json:"password"`
}

type storeDocumentOutput struct {
	SchemaVersion uint64                       `json:"schemaVersion"`
	Credentials   []persistentCredentialOutput `json:"credentials"`
}

type persistentCredentialOutput struct {
	CredentialID CredentialID `json:"credentialId"`
	Username     string       `json:"username"`
	Password     string       `json:"password"`
}

func decodeStoreDocument(data []byte) (map[CredentialID]AuthenticationCredential, error) {
	var document storeDocumentInput
	if err := jsonfile.DecodeStrict(data, &document); err != nil {
		return nil, err
	}
	if document.SchemaVersion == nil || document.Credentials == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("required credentials document field missing"))
	}
	if *document.SchemaVersion != jsonfile.SchemaVersion1 {
		return nil, persistence.NewFailure(persistence.FailureUnsupportedSchemaVersion, nil)
	}
	credentials := make(map[CredentialID]AuthenticationCredential, len(*document.Credentials))
	for _, record := range *document.Credentials {
		if record.CredentialID == nil || record.Username == nil || record.Password == nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("required credential field missing"))
		}
		if *record.CredentialID == "" || *record.Username == "" {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("credential identity or username is empty"))
		}
		if _, exists := credentials[*record.CredentialID]; exists {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("duplicate credential identity"))
		}
		credentials[*record.CredentialID] = AuthenticationCredential{Username: *record.Username, Password: *record.Password}
	}
	return credentials, nil
}

func encodeStoreDocument(credentials map[CredentialID]AuthenticationCredential) ([]byte, error) {
	identifiers := make([]CredentialID, 0, len(credentials))
	for identifier := range credentials {
		identifiers = append(identifiers, identifier)
	}
	sort.Slice(identifiers, func(left, right int) bool { return identifiers[left] < identifiers[right] })
	records := make([]persistentCredentialOutput, 0, len(identifiers))
	for _, identifier := range identifiers {
		credential := credentials[identifier]
		if identifier == "" || credential.Username == "" {
			return nil, persistence.NewFailure(persistence.FailureInvalidArgument, fmt.Errorf("credential identity or username is empty"))
		}
		records = append(records, persistentCredentialOutput{
			CredentialID: identifier,
			Username:     credential.Username,
			Password:     credential.Password,
		})
	}
	return jsonfile.MarshalDeterministic(storeDocumentOutput{
		SchemaVersion: jsonfile.SchemaVersion1,
		Credentials:   records,
	})
}
