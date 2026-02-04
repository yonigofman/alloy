package json

import (
	"testing"
	"time"

	"github.com/grafana/alloy/syntax"
	"github.com/stretchr/testify/require"

	json_config "github.com/prometheus-community/json_exporter/config"
)

func TestAlloyUnmarshal(t *testing.T) {
	alloyConfig := `
		url = "http://api.example.com"
		timeout = "10s"
		
		client {
			headers = { "X-Test" = "test" }
			basic_auth {
				username = "user"
				password = "password"
			}
		}

		static_labels = { "env" = "prod" }

		mapping {
			metric {
				name = "simple_metric"
				type = "gauge"
				value_path = ".value"
			}
			array {
				items_path = ".items[*]"
				metric {
					name = "item_info"
					type = "gauge"
					value = 1
					labels = { "name" = ".name" }
				}
			}
		}
	`
	var args Arguments
	err := syntax.Unmarshal([]byte(alloyConfig), &args)
	require.NoError(t, err)

	expected := Arguments{
		URL:          "http://api.example.com",
		Timeout:      10 * time.Second,
		StaticLabels: map[string]string{"env": "prod"},
		Client: &Client{
			Headers: map[string]string{"X-Test": "test"},
			BasicAuth: &BasicAuth{
				Username: "user",
				Password: "password",
			},
		},
		Mappings: []Mapping{
			{
				Metrics: []Metric{
					{
						Name:      "simple_metric",
						Type:      "gauge",
						ValuePath: ".value",
					},
				},
				Arrays: []Array{
					{
						ItemsPath: ".items[*]",
						Metrics: []Metric{
							{
								Name:   "item_info",
								Type:   "gauge",
								Value:  1,
								Labels: map[string]string{"name": ".name"},
							},
						},
					},
				},
			},
		},
	}
	require.Equal(t, expected, args)
}

func TestAlloyConvert(t *testing.T) {
	args := Arguments{
		URL: "http://api.example.com",
		Mappings: []Mapping{
			{
				Metrics: []Metric{
					{
						Name:      "simple",
						Type:      "gauge",
						ValuePath: ".val",
					},
				},
				Arrays: []Array{
					{
						ItemsPath: ".list",
						Metrics: []Metric{
							{
								Name:  "list_item",
								Type:  "counter",
								Value: 1,
							},
						},
					},
				},
			},
		},
	}

	converted := args.Convert()

	// Check struct directly through JSONModules
	require.NotNil(t, converted.JSONModules)
	cfg := converted.JSONModules

	module, ok := cfg.Modules["default"]
	require.True(t, ok)

	require.Len(t, module.Metrics, 2)

	// Check simple metric
	require.Equal(t, "simple", module.Metrics[0].Name)
	require.Equal(t, ".val", module.Metrics[0].Path)
	require.Equal(t, json_config.ValueScrape, module.Metrics[0].Type)
	require.Equal(t, json_config.ValueTypeGauge, module.Metrics[0].ValueType)

	// Check array metric
	require.Equal(t, "list_item", module.Metrics[1].Name)
	require.Equal(t, ".list", module.Metrics[1].Path)
	require.Equal(t, json_config.ObjectScrape, module.Metrics[1].Type)
	require.Equal(t, map[string]string{"value": "{1}"}, module.Metrics[1].Values)
}

func TestConvertClient(t *testing.T) {
	args := Arguments{
		URL: "http://test",
		Client: &Client{
			Headers: map[string]string{"Foo": "Bar"},
			BasicAuth: &BasicAuth{
				Username: "u",
				Password: "p",
			},
		},
	}

	converted := args.Convert()
	require.NotNil(t, converted.JSONModules)
	cfg := converted.JSONModules

	module := cfg.Modules["default"]
	require.Equal(t, "Bar", module.Headers["Foo"])
	require.Equal(t, "u", module.HTTPClientConfig.BasicAuth.Username)
	require.Equal(t, "p", string(module.HTTPClientConfig.BasicAuth.Password))
}
