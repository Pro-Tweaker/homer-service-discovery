package homer

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// newFileMode is used when the generated config does not exist yet (extra pages).
const newFileMode = 0o644

func GetConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	config, err := unmarshalConfig(b)
	if err != nil {
		return Config{}, fmt.Errorf("error unmarshalling config file: %w", err)
	}

	return config, nil
}

func unmarshalConfig(contents []byte) (Config, error) {
	config := Config{}

	if len(contents) == 0 {
		return config, fmt.Errorf("homer config is empty")
	}

	if err := yaml.Unmarshal(contents, &config); err != nil {
		return config, err
	}

	// the footer can be disabled by changing it from a 'string' to a 'bool'
	if config.Footer == "false" {
		config.Footer = false
	}

	return config, nil
}

// PutConfig writes the config to path. An existing file keeps its mode and
// owner; a missing one is created.
func PutConfig(config Config, path string) error {
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshalling data into YAML: %w", err)
	}

	fileStats, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, b, newFileMode); err != nil {
			return fmt.Errorf("error creating config file '%s': %w", path, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("error getting stats for config file '%s': %w", path, err)
	}

	err = os.WriteFile(path, b, fileStats.Mode())
	if err != nil {
		return fmt.Errorf("error writing data to config file '%s': %w", path, err)
	}

	return preserveOwner(path, fileStats)
}
