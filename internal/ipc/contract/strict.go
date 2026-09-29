package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// rejectDuplicateJSONKeys scans one complete JSON value and rejects repeated
// member names in every object, including nested objects and array elements.
// json.Decoder.Token returns decoded strings, so escaped-equivalent names are
// compared by their actual key values. The scanner intentionally does not
// include key or value bytes in its diagnostic.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("duplicate key scan: object member name is not a string")
			}
			if _, exists := seen[name]; exists {
				return fmt.Errorf("duplicate object key")
			}
			seen[name] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("duplicate key scan: unexpected delimiter")
	}
}

// decodeStrict decodes exactly one JSON value into target while rejecting
// unknown fields, null or empty input, trailing JSON values and trailing
// garbage. Duplicate validation runs after those existing diagnostics.
func decodeStrict(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return fmt.Errorf("decode payload: missing payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("decode payload: trailing data")
	} else if err != io.EOF {
		return fmt.Errorf("decode payload: trailing garbage")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return nil
}
