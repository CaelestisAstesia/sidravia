package jsonfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"sidravia/internal/daemon/persistence"
)

const (
	SchemaVersion1               uint64 = 1
	AuthenticationFileSizeLimit  int64  = 16 * 1024 * 1024
	ApplicationSettingsSizeLimit int64  = 1 * 1024 * 1024
	ProfileFileSizeLimit         int64  = 1 * 1024 * 1024
	RawRecordSizeLimit           int64  = 1 * 1024 * 1024
)

func ReadLimited(reader io.Reader, maximum int64) ([]byte, error) {
	if maximum <= 0 {
		return nil, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	if int64(len(data)) > maximum {
		return nil, persistence.NewFailure(persistence.FailureSizeLimitExceeded, nil)
	}
	return data, nil
}

func DecodeStrict(data []byte, destination any, opaqueJSONPointers ...string) error {
	if err := scanExactlyOne(data, opaqueJSONPointers); err != nil {
		return invalidDocument(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return invalidDocument(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return invalidDocument(fmt.Errorf("multiple JSON values"))
		}
		return invalidDocument(err)
	}
	return nil
}

func RequireObjectFields(data []byte, fieldNames ...string) error {
	if err := scanExactlyOne(data, nil); err != nil {
		return invalidDocument(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return invalidDocument(err)
	}
	for _, fieldName := range fieldNames {
		if _, exists := fields[fieldName]; !exists {
			return invalidDocument(fmt.Errorf("required field missing"))
		}
	}
	return nil
}

func RequireSchemaVersion(data []byte, wanted uint64) error {
	if err := scanExactlyOne(data, nil); err != nil {
		return invalidDocument(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return invalidDocument(err)
	}
	rawVersion, exists := fields["schemaVersion"]
	if !exists || bytes.Equal(bytes.TrimSpace(rawVersion), []byte("null")) {
		return invalidDocument(fmt.Errorf("schemaVersion missing or null"))
	}
	var actual uint64
	if err := json.Unmarshal(rawVersion, &actual); err != nil {
		return invalidDocument(err)
	}
	if actual != wanted {
		return persistence.NewFailure(persistence.FailureUnsupportedSchemaVersion, nil)
	}
	return nil
}

func ValidateOpaqueObjectOrNull(data json.RawMessage) error {
	if err := validateUTF8(data); err != nil {
		return invalidDocument(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return invalidDocument(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return invalidDocument(fmt.Errorf("multiple JSON values"))
		}
		return invalidDocument(err)
	}
	if value == nil {
		return nil
	}
	if _, isObject := value.(map[string]any); !isObject {
		return invalidDocument(fmt.Errorf("opaque value is not object or null"))
	}
	return nil
}

func MarshalDeterministic(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, invalidDocument(err)
	}
	if err := scanExactlyOne(data, nil); err != nil {
		return nil, invalidDocument(err)
	}
	return data, nil
}

func MarshalRawRecordEnvelope(schemaVersion uint64, records []json.RawMessage) ([]byte, error) {
	for _, record := range records {
		if int64(len(record)) > RawRecordSizeLimit {
			return nil, persistence.NewFailure(persistence.FailureSizeLimitExceeded, nil)
		}
		if err := validateOneValue(record); err != nil {
			return nil, invalidDocument(err)
		}
	}

	result := make([]byte, 0, 32)
	result = append(result, `{"schemaVersion":`...)
	result = strconv.AppendUint(result, schemaVersion, 10)
	result = append(result, `,"records":[`...)
	for index, record := range records {
		if index > 0 {
			result = append(result, ',')
		}
		result = append(result, record...)
	}
	result = append(result, ']', '}')
	return result, nil
}

func invalidDocument(cause error) error {
	return persistence.NewFailure(persistence.FailureInvalidDocument, cause)
}

func validateOneValue(data []byte) error {
	if err := validateUTF8(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanOpaqueValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func scanExactlyOne(data []byte, opaqueJSONPointers []string) error {
	if err := validateUTF8(data); err != nil {
		return err
	}
	skipped := make(map[string]struct{}, len(opaqueJSONPointers))
	for _, pointer := range opaqueJSONPointers {
		skipped[pointer] = struct{}{}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanOwnedValue(decoder, "", skipped); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func scanOwnedValue(decoder *json.Decoder, pointer string, skipped map[string]struct{}) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			return scanOwnedObject(decoder, pointer, skipped)
		case '[':
			return scanOwnedArray(decoder, pointer, skipped)
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
	default:
		return nil
	}
}

func scanOwnedObject(decoder *json.Decoder, pointer string, skipped map[string]struct{}) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("object key is not a string")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate object key")
		}
		seen[key] = struct{}{}
		childPointer := pointer + "/" + escapeJSONPointerToken(key)
		if _, opaque := skipped[childPointer]; opaque {
			if err := scanOpaqueValue(decoder); err != nil {
				return err
			}
			continue
		}
		if err := scanOwnedValue(decoder, childPointer, skipped); err != nil {
			return err
		}
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('}') {
		return fmt.Errorf("object not terminated")
	}
	return nil
}

func scanOwnedArray(decoder *json.Decoder, pointer string, skipped map[string]struct{}) error {
	for index := 0; decoder.More(); index++ {
		childPointer := pointer + "/" + strconv.Itoa(index)
		if _, opaque := skipped[childPointer]; opaque {
			if err := scanOpaqueValue(decoder); err != nil {
				return err
			}
			continue
		}
		if err := scanOwnedValue(decoder, childPointer, skipped); err != nil {
			return err
		}
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim(']') {
		return fmt.Errorf("array not terminated")
	}
	return nil
}

func scanOpaqueValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{', '[':
		for decoder.More() {
			if delimiter == '{' {
				if _, err := decoder.Token(); err != nil {
					return err
				}
			}
			if err := scanOpaqueValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if (delimiter == '{' && closing != json.Delim('}')) || (delimiter == '[' && closing != json.Delim(']')) {
			return fmt.Errorf("opaque value not terminated")
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

func escapeJSONPointerToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}

func validateUTF8(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8")
	}
	return nil
}
