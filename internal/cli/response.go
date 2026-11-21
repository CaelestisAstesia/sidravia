package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func decodeStrictCLI(data []byte, destination any, trailingMessage string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New(trailingMessage)
	}
	return nil
}
