package json

import (
	"testing"
	"time"

	"github.com/grafana/alloy/internal/static/integrations/json_exporter"
	"github.com/grafana/alloy/internal/util"
	"github.com/grafana/alloy/syntax"
	"github.com/grafana/alloy/syntax/alloytypes"
	"github.com/stretchr/testify/require"
)

func TestAlloyUnmarshal(t *testing.T) {
	alloyConfig := `
		config = "modules:\n  default:\n    metrics:\n      - name: example\n        path: '{ .counter }'\n"
		target {
			name    = "example"
			address = "http://localhost:8080/metrics.json"
		}
		probe_timeout_offset = "1s"
	`
	var args Arguments
	err := syntax.Unmarshal([]byte(alloyConfig), &args)
	require.NoError(t, err)

	expected := Arguments{
		Config: alloytypes.OptionalSecret{
			Value: "modules:\n  default:\n    metrics:\n      - name: example\n        path: '{ .counter }'\n",
		},
		Targets: TargetBlock{
			{
				Name:   "example",
				Target: "http://localhost:8080/metrics.json",
			},
		},
		ProbeTimeoutOffset: 1 * time.Second,
	}
	require.Equal(t, expected, args)
}

func TestAlloyConvert(t *testing.T) {
	args := Arguments{
		Config: alloytypes.OptionalSecret{
			Value: "modules:\n  default:\n    metrics:\n      - name: example\n        path: '{ .counter }'\n",
		},
		Targets: TargetBlock{
			{
				Name:   "example",
				Target: "http://localhost:8080/metrics.json",
			},
		},
		ProbeTimeoutOffset: 500 * time.Millisecond,
	}

	converted := args.Convert()

	expected := &json_exporter.Config{
		JSONConfig: util.RawYAML("modules:\n  default:\n    metrics:\n      - name: example\n        path: '{ .counter }'\n"),
		JSONTargets: []json_exporter.JSONTarget{
			{
				Name:   "example",
				Target: "http://localhost:8080/metrics.json",
			},
		},
		ProbeTimeoutOffset: 0.5,
	}

	require.Equal(t, expected, converted)
}

func TestTargetsList(t *testing.T) {
	alloyConfig := `
		config = "modules:\n  default:\n    metrics:\n      - name: example\n        path: '{ .counter }'\n"
		targets = [
			{ "name" = "example1", "address" = "http://host1", "module" = "mod1", "extra" = "label" },
		]
	`
	var args Arguments
	err := syntax.Unmarshal([]byte(alloyConfig), &args)
	require.NoError(t, err)

	require.Len(t, args.TargetsList, 1)
	require.Equal(t, "example1", args.TargetsList[0]["name"])
	require.Equal(t, "http://host1", args.TargetsList[0]["address"])
	require.Equal(t, "mod1", args.TargetsList[0]["module"])
	require.Equal(t, "label", args.TargetsList[0]["extra"])

	converted := args.Convert()
	require.Len(t, converted.JSONTargets, 1)
	require.Equal(t, "example1", converted.JSONTargets[0].Name)
	require.Equal(t, "http://host1", converted.JSONTargets[0].Target)
	require.Equal(t, "mod1", converted.JSONTargets[0].Module)
	require.Equal(t, map[string]string{"extra": "label"}, converted.JSONTargets[0].Labels)
}
