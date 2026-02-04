package json_exporter

import (
	"github.com/go-kit/log"
	"github.com/grafana/alloy/internal/static/integrations"
	"github.com/grafana/alloy/internal/util"
	json_config "github.com/prometheus-community/json_exporter/config"
	"gopkg.in/yaml.v2"
)

// DefaultConfig holds default settings for the json_exporter integration.
var DefaultConfig = Config{
	ProbeTimeoutOffset: 500.0, // Default in ms, though float64 suggests seconds? blackbox uses seconds. json_exporter typically uses ms for timeout?
}

// JSONTarget defines a target to be used by the integration.
type JSONTarget struct {
	Name   string            `yaml:"name"`
	Target string            `yaml:"address"`
	Module string            `yaml:"module,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

// Config configures the JSON integration.
type Config struct {
	JSONConfigFile     string              `yaml:"config_file,omitempty"`
	JSONTargets        []JSONTarget        `yaml:"json_targets"`
	JSONConfig         util.RawYAML        `yaml:"json_config,omitempty"`
	JSONModules        *json_config.Config `yaml:"-"`                              // Used for struct-based config to avoid secret marshaling issues
	ProbeTimeoutOffset float64             `yaml:"probe_timeout_offset,omitempty"` // in seconds
}

// UnmarshalYAML implements yaml.Unmarshaler for Config.
func (c *Config) UnmarshalYAML(unmarshal func(any) error) error {
	*c = DefaultConfig

	type plain Config
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}

	var jsonConfig json_config.Config
	return yaml.Unmarshal(c.JSONConfig, &jsonConfig)
}

// Name returns the name of the integration.
func (c *Config) Name() string {
	return "json"
}

// InstanceKey returns the instance key of the integration.
func (c *Config) InstanceKey(defaultKey string) (string, error) {
	return defaultKey, nil
}

// NewIntegration creates a new json integration.
func (c *Config) NewIntegration(l log.Logger) (integrations.Integration, error) {
	return New(l, c)
}

func init() {
	integrations.RegisterIntegration(&Config{})
}
