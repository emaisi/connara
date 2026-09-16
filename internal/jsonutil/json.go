// Package jsonutil preserves provider numbers instead of rounding them to float64.
package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Unmarshal(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("JSON must contain exactly one value")
	}
	return nil
}
