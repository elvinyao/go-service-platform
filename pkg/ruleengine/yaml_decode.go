package ruleengine

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

func rejectAdditionalYAMLDocuments(decoder *yaml.Decoder) error {
	var additional interface{}
	err := decoder.Decode(&additional)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("multiple YAML documents are not supported")
}
