package json

import (
	"errors"
	"fmt"
	"time"

	"github.com/grafana/alloy/internal/component"
	"github.com/grafana/alloy/internal/component/discovery"
	"github.com/grafana/alloy/internal/component/prometheus/exporter"
	"github.com/grafana/alloy/internal/featuregate"
	"github.com/grafana/alloy/internal/static/integrations"
	"github.com/grafana/alloy/internal/static/integrations/json_exporter"
	"github.com/grafana/alloy/internal/util"
	"github.com/grafana/alloy/syntax/alloytypes"
	json_config "github.com/prometheus-community/json_exporter/config"
	"gopkg.in/yaml.v2"
)

func init() {
	component.Register(component.Registration{
		Name:      "prometheus.exporter.json",
		Stability: featuregate.StabilityExperimental,
		Args:      Arguments{},
		Exports:   exporter.Exports{},

		Build: exporter.NewWithTargetBuilder(createExporter, "json", buildJSONTargets),
	})
}

func createExporter(opts component.Options, args component.Arguments) (integrations.Integration, string, error) {
	a := args.(Arguments)
	defaultInstanceKey := opts.ID // if cannot resolve instance key, use the component ID
	return integrations.NewIntegrationWithInstanceKey(opts.Logger, a.Convert(), defaultInstanceKey)
}

// buildJSONTargets creates the exporter's discovery targets based on the defined json targets.
func buildJSONTargets(baseTarget discovery.Target, args component.Arguments) []discovery.Target {
	var targets []discovery.Target

	jsonTargets := args.(Arguments).Targets
	if len(jsonTargets) == 0 {
		// Converting to JSONTarget to avoid duplicating logic
		jsonTargets = args.(Arguments).TargetsList.convertInternal()
	}

	for _, tgt := range jsonTargets {
		target := make(map[string]string, len(tgt.Labels)+baseTarget.Len())
		// Set extra labels first, meaning that any other labels will override
		for k, v := range tgt.Labels {
			target[k] = v
		}
		baseTarget.ForEachLabel(func(key string, value string) bool {
			target[key] = value
			return true
		})

		target["job"] = target["job"] + "/" + tgt.Name
		target["__param_target"] = tgt.Target
		if tgt.Module != "" {
			target["__param_module"] = tgt.Module
		}

		targets = append(targets, discovery.NewTargetFromMap(target))
	}

	return targets
}

// DefaultArguments holds non-zero default options for Arguments when it is
// unmarshaled from Alloy.
var DefaultArguments = Arguments{
	ProbeTimeoutOffset: 500 * time.Millisecond,
}

// JSONTarget defines a target to be used by the exporter.
type JSONTarget struct {
	Name   string            `alloy:"name,attr"`
	Target string            `alloy:"address,attr"`
	Module string            `alloy:"module,attr,optional"`
	Labels map[string]string `alloy:"labels,attr,optional"`
}

type TargetBlock []JSONTarget

// Convert converts the component's TargetBlock to a slice of integration's JSONTarget.
func (t TargetBlock) Convert() []json_exporter.JSONTarget {
	targets := make([]json_exporter.JSONTarget, 0, len(t))
	for _, target := range t {
		targets = append(targets, json_exporter.JSONTarget{
			Name:   target.Name,
			Target: target.Target,
			Module: target.Module,
			Labels: target.Labels,
		})
	}
	return targets
}

type Arguments struct {
	ConfigFile         string                    `alloy:"config_file,attr,optional"`
	Config             alloytypes.OptionalSecret `alloy:"config,attr,optional"`
	Targets            TargetBlock               `alloy:"target,block,optional"`
	ProbeTimeoutOffset time.Duration             `alloy:"probe_timeout_offset,attr,optional"`

	// New way of passing targets. This allows the component to receive targets from other components.
	TargetsList TargetsList `alloy:"targets,attr,optional"`
}

type TargetsList []map[string]string

func (t TargetsList) Convert() []json_exporter.JSONTarget {
	targets := make([]json_exporter.JSONTarget, 0, len(t))
	for _, target := range t {
		address, _ := getAddress(target)
		labels := make(map[string]string)
		for key, value := range target {
			if key != "name" && key != "__address__" && key != "address" && key != "module" {
				labels[key] = value
			}
		}

		targets = append(targets, json_exporter.JSONTarget{
			Name:   target["name"],
			Target: address,
			Module: target["module"],
			Labels: labels,
		})
	}
	return targets
}

func (t TargetsList) convertInternal() []JSONTarget {
	targets := make([]JSONTarget, 0, len(t))
	for _, target := range t {
		// extract the extra labels
		labels := make(map[string]string)
		for key, value := range target {
			if key != "name" && key != "__address__" && key != "address" && key != "module" {
				labels[key] = value
			}
		}

		address, _ := getAddress(target)
		targets = append(targets, JSONTarget{
			Name:   target["name"],
			Target: address,
			Module: target["module"],
			Labels: labels,
		})
	}
	return targets
}

// SetToDefault implements syntax.Defaulter.
func (a *Arguments) SetToDefault() {
	*a = DefaultArguments
}

// Validate implements syntax.Validator.
func (a *Arguments) Validate() error {
	if a.ConfigFile != "" && a.Config.Value != "" {
		return errors.New("config and config_file are mutually exclusive")
	}

	if a.ConfigFile == "" && a.Config.Value == "" {
		return errors.New("config or config_file must be set")
	}

	var jsonConfig json_config.Config
	err := yaml.UnmarshalStrict([]byte(a.Config.Value), &jsonConfig)
	if err != nil {
		return fmt.Errorf("invalid json_exporter config: %s", err)
	}

	if len(a.Targets) != 0 && len(a.TargetsList) != 0 {
		return fmt.Errorf("the block `target` and the attribute `targets` are mutually exclusive")
	}
	for _, target := range a.TargetsList {
		if _, hasName := target["name"]; !hasName {
			return fmt.Errorf("all targets must have a `name`")
		}
		if _, hasAddress := getAddress(target); !hasAddress {
			return fmt.Errorf("all targets must have an `address` or an `__address__` label")
		}
	}
	return nil
}

// Convert converts the component's Arguments to the integration's Config.
func (a *Arguments) Convert() *json_exporter.Config {
	var targets []json_exporter.JSONTarget
	if len(a.Targets) != 0 {
		targets = a.Targets.Convert()
	} else {
		targets = a.TargetsList.Convert()
	}
	return &json_exporter.Config{
		JSONConfigFile:     a.ConfigFile,
		JSONConfig:         util.RawYAML(a.Config.Value),
		JSONTargets:        targets,
		ProbeTimeoutOffset: a.ProbeTimeoutOffset.Seconds(),
	}
}

func getAddress(data map[string]string) (string, bool) {
	if value, ok := data["address"]; ok {
		return value, true
	}
	if value, ok := data["__address__"]; ok {
		return value, true
	}
	return "", false
}
