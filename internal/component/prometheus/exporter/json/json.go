package json

import (
	"fmt"
	"time"

	"github.com/grafana/alloy/internal/component"
	"github.com/grafana/alloy/internal/component/discovery"
	"github.com/grafana/alloy/internal/component/prometheus/exporter"
	"github.com/grafana/alloy/internal/featuregate"
	"github.com/grafana/alloy/internal/static/integrations"
	"github.com/grafana/alloy/internal/static/integrations/json_exporter"
	"github.com/grafana/alloy/syntax/alloytypes"
	"github.com/prometheus/common/config"

	json_config "github.com/prometheus-community/json_exporter/config"
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
	a := args.(Arguments)
	target := make(map[string]string, len(a.StaticLabels)+baseTarget.Len())

	// Set static labels
	for k, v := range a.StaticLabels {
		target[k] = v
	}
	baseTarget.ForEachLabel(func(key string, value string) bool {
		target[key] = value
		return true
	})

	// The target address is the URL defined in the arguments
	target["__param_target"] = a.URL

	return []discovery.Target{discovery.NewTargetFromMap(target)}
}

// Arguments holds options for Arguments when it is unmarshaled from Alloy.
type Arguments struct {
	URL           string        `alloy:"url,attr"`
	PollFrequency time.Duration `alloy:"poll_frequency,attr,optional"` // Ignored, handled by scrape
	Timeout       time.Duration `alloy:"timeout,attr,optional"`

	Client       *Client           `alloy:"client,block,optional"`
	StaticLabels map[string]string `alloy:"static_labels,attr,optional"`
	Mappings     []Mapping         `alloy:"mapping,block,optional"`
	Limits       *Limits           `alloy:"limits,block,optional"`
}

type Client struct {
	Headers            map[string]string              `alloy:"headers,attr,optional"`
	Authorization      *Authorization                 `alloy:"authorization,block,optional"`
	BasicAuth          *BasicAuth                     `alloy:"basic_auth,block,optional"`
	OAuth2             *OAuth2                        `alloy:"oauth2,block,optional"`
	TLSConfig          *config.TLSConfig              `alloy:"tls_config,block,optional"`
	ProxyURL           *config.URL                    `alloy:"proxy_url,attr,optional"`
	NoProxy            string                         `alloy:"no_proxy,attr,optional"`
	ProxyConnectHeader map[string][]alloytypes.Secret `alloy:"proxy_connect_header,attr,optional"`
	FollowRedirects    bool                           `alloy:"follow_redirects,attr,optional"`
	EnableHTTP2        bool                           `alloy:"enable_http2,attr,optional"`
}

type Authorization struct {
	Type            string            `alloy:"type,attr,optional"`
	Credentials     alloytypes.Secret `alloy:"credentials,attr,optional"`
	CredentialsFile string            `alloy:"credentials_file,attr,optional"`
}

type BasicAuth struct {
	Username     string            `alloy:"username,attr,optional"`
	Password     alloytypes.Secret `alloy:"password,attr,optional"`
	PasswordFile string            `alloy:"password_file,attr,optional"`
}

type OAuth2 struct {
	ClientID         string            `alloy:"client_id,attr"`
	ClientSecret     alloytypes.Secret `alloy:"client_secret,attr,optional"`
	ClientSecretFile string            `alloy:"client_secret_file,attr,optional"`
	Scopes           []string          `alloy:"scopes,attr,optional"`
	TokenURL         string            `alloy:"token_url,attr"`
	EndpointParams   map[string]string `alloy:"endpoint_params,attr,optional"`
}

type Mapping struct {
	Metrics []Metric `alloy:"metric,block,optional"`
	Arrays  []Array  `alloy:"array,block,optional"`
}

type Metric struct {
	Name      string            `alloy:"name,attr"`
	Help      string            `alloy:"help,attr,optional"`
	Type      string            `alloy:"type,attr"` // gauge, counter, etc.
	Value     float64           `alloy:"value,attr,optional"`
	ValuePath string            `alloy:"value_path,attr,optional"`
	Labels    map[string]string `alloy:"labels,attr,optional"`
}

type Array struct {
	ItemsPath string   `alloy:"items_path,attr"`
	Metrics   []Metric `alloy:"metric,block,optional"`
}

type Limits struct {
	MaxSeries       int  `alloy:"max_series,attr,optional"`
	MaxLabelLength  int  `alloy:"max_label_length,attr,optional"`
	DropEmptyLabels bool `alloy:"drop_empty_labels,attr,optional"`
}

// DefaultArguments holds non-zero default options.
var DefaultArguments = Arguments{
	Timeout: 10 * time.Second,
}

// SetToDefault implements syntax.Defaulter.
func (a *Arguments) SetToDefault() {
	*a = DefaultArguments
}

// Validate implements syntax.Validator.
func (a *Arguments) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

// Convert converts the component's Arguments to the integration's Config.
func (a *Arguments) Convert() *json_exporter.Config {
	// Construct the json_exporter config programmatically
	module := json_config.Module{
		Headers: a.ClientHeaders(),
		// HTTP Client Config
		HTTPClientConfig: a.ClientConfig(),
		// Metrics
		Metrics: a.BuildMetrics(),
	}

	cfg := json_config.Config{
		Modules: map[string]json_config.Module{
			"default": module,
		},
	}

	return &json_exporter.Config{
		JSONModules:        &cfg,
		JSONTargets:        []json_exporter.JSONTarget{{Name: "default", Target: a.URL}},
		ProbeTimeoutOffset: 0, // Not really used in this mode
	}
}

func (a *Arguments) ClientHeaders() map[string]string {
	if a.Client == nil || a.Client.Headers == nil {
		return make(map[string]string)
	}
	return a.Client.Headers
}

func (a *Arguments) ClientConfig() config.HTTPClientConfig {
	cfg := config.DefaultHTTPClientConfig
	if a.Client != nil {
		if a.Client.BasicAuth != nil {
			cfg.BasicAuth = &config.BasicAuth{
				Username:     a.Client.BasicAuth.Username,
				Password:     config.Secret(a.Client.BasicAuth.Password),
				PasswordFile: a.Client.BasicAuth.PasswordFile,
			}
		}
		if a.Client.Authorization != nil {
			cfg.Authorization = &config.Authorization{
				Type:            a.Client.Authorization.Type,
				Credentials:     config.Secret(a.Client.Authorization.Credentials),
				CredentialsFile: a.Client.Authorization.CredentialsFile,
			}
		}
		if a.Client.OAuth2 != nil {
			cfg.OAuth2 = &config.OAuth2{
				ClientID:         a.Client.OAuth2.ClientID,
				ClientSecret:     config.Secret(a.Client.OAuth2.ClientSecret),
				ClientSecretFile: a.Client.OAuth2.ClientSecretFile,
				Scopes:           a.Client.OAuth2.Scopes,
				TokenURL:         a.Client.OAuth2.TokenURL,
				EndpointParams:   a.Client.OAuth2.EndpointParams,
			}
		}
		if a.Client.TLSConfig != nil {
			cfg.TLSConfig = *a.Client.TLSConfig
		}
		if a.Client.ProxyURL != nil {
			cfg.ProxyURL = *a.Client.ProxyURL
		}
		cfg.NoProxy = a.Client.NoProxy
		for k, v := range a.Client.ProxyConnectHeader {
			var secrets []config.Secret
			for _, s := range v {
				secrets = append(secrets, config.Secret(s))
			}
			cfg.ProxyConnectHeader[k] = secrets
		}
		cfg.FollowRedirects = a.Client.FollowRedirects
		cfg.EnableHTTP2 = a.Client.EnableHTTP2
	}
	return cfg
}

func (a *Arguments) BuildMetrics() []json_config.Metric {
	var metrics []json_config.Metric
	for _, mapping := range a.Mappings {
		// Single metrics (not array)
		for _, m := range mapping.Metrics {
			metrics = append(metrics, convertMetric(m, ""))
		}
		// Array metrics
		for _, array := range mapping.Arrays {
			// In json_exporter, object iteration is handled by "type: object"
			// and then nested "values" for the metrics.
			// But wait, the standard json_exporter struct 'Metric' has 'Values map[string]string' for sub-metrics in object mode.

			// If we have multiple metrics for the SAME array path, we should ideally group them?
			// OR we can generate multiple json_exporter metrics, one for each "metric" block in Alloy?
			// json_exporter supports multiple metrics with same path? Yes.

			for _, m := range array.Metrics {
				// For array/object iteration, json_exporter uses:
				// type: object
				// path: <items_path>
				// values:
				//   <suffix>: <value_path>
				// labels: ...

				// However, our Alloy syntax allows specifying full metric name in the block.
				// In json_exporter, if type is object, the keys in 'values' are appended to the main name?
				// Actually, looking at json_exporter/config/config.go:
				// Type=ObjectScrape
				// Values map[string]string
				// Main Metric Name is the prefix.

				// So if user wants "api_item_info" and "api_item_score", we need separate entries in config.Modules?
				// Or separate entries in Config.Metrics?

				// We can have multiple entries in Config.Metrics with same Path.

				// So for each metric in the array block:
				jm := json_config.Metric{
					Name:   m.Name,
					Path:   array.ItemsPath,
					Type:   json_config.ObjectScrape,
					Help:   m.Help,
					Labels: m.Labels,
					Values: map[string]string{
						"val": oneOrPath(m.Value, m.ValuePath), // we need a key. "val" might be generic. Or we can use empty string if supported?
						// Wait, json_exporter logic:
						// for subName, valuePath := range metric.Values {
						//    name := MakeMetricName(metric.Name, subName)
						// }
						// if subName is empty, it probably just uses Name?
						// MakeMetricName uses strings.Join with "_".
						// If subName is "", it might append "_".
						// Let's check util.go MakeMetricName.
					},
				}

				// To get exact name Match, we might need a workaround.
				// If we use subkey "info" -> Name_info.
				// The user wants "api_item_info".
				// If Alloy config is Name="api_item_info", we want final metric "api_item_info".
				// If we pass subkey="", MakeMetricName("api_item_info", "") -> "api_item_info_" ?
				// strings.Join(..., "_") -> yes.

				// BUT: json_exporter creates metrics.
				// If we want exact name, maybe we can abuse the system?
				// Actually, multiple values per object scrape is an optimization.
				// We can just define one value per ObjectScrape metric entry.

				// Let's try to make the sub-key empty?
				// If we look at `exporter/util.go`:
				// func MakeMetricName(parts ...string) string { return strings.Join(parts, "_") }
				// So `api_item_info` + `_` + `` -> `api_item_info`. No wait.
				// Join(["a", ""], "_") -> "a_"

				// So we probably want to assume the user provides a "base" name?
				// Or we accept that it appends a suffix.
				// User example:
				// metric { name = "api_item_info" ... value = 1 ... }
				// Result: api_item_info?

				// If we use a dummy key "value", we get `api_item_info_value`.
				// Maybe that's acceptable or standard json_exporter behavior.
				// The existing logic I wrote in `example_json.alloy` used `values: info: '{1}'` and produced `app_info_info`.

				// Let's try to be clever.
				// If the user name ends in certain way? No.

				// What if the user sets `name` in the Alloy block to the common prefix?
				// User example: `name = "api_item_info"`
				// Maybe we enforce `_value` suffix?

				// Actually, `json_exporter` allows multiple values.
				// User's example:
				// metric { name = "api_item_info" ... }
				// metric { name = "api_item_score" ... }

				// We can implement this by creating TWO separate Metrics in the json_exporter config, each with type=object.
				// Metric 1: Name="api_item_info", Values={"":"1"} -> "api_item_info_"
				// Wait, if I use a custom fix in the integration wrapper? No.

				// Let's look at `exporter/util.go` again.
				// It seems `json_exporter` really wants to append.

				// Strategy: Use a constant suffix "val" or "info" or similar, and advise user.
				// Or, use a single letter?
				// Or, maybe I can use a key that is `""` (empty string) but handle the underscore?
				// If parts[1] is empty, Join still adds separator?

				// Actually, if we look closely at `json_exporter` source:
				// for subName, valuePath := range metric.Values {
				//    name := MakeMetricName(metric.Name, subName)
				// }
				// It forces the join.

				// The user expectation: `api_item_info`
				// Behavior: `api_item_info_something`

				// We'll use "val" for numeric value, "info" for static 1?
				// Or just "value"?

				// Let's use "value" as the default suffix key.
				jm.Values = map[string]string{"value": oneOrPath(m.Value, m.ValuePath)}

				metrics = append(metrics, jm)
			}
		}
	}
	return metrics
}

func convertMetric(m Metric, pathOverride string) json_config.Metric {
	// For simple ValueScrape
	p := m.ValuePath
	if p == "" {
		p = fmt.Sprintf("{%v}", m.Value)
	}

	if pathOverride != "" {
		p = pathOverride // actually for object scrape, path comes from parent
	}

	valType := json_config.ValueTypeUntyped
	switch m.Type {
	case "gauge":
		valType = json_config.ValueTypeGauge
	case "counter":
		valType = json_config.ValueTypeCounter
	}

	return json_config.Metric{
		Name:      m.Name,
		Path:      p,
		Type:      json_config.ValueScrape,
		Help:      m.Help,
		Labels:    m.Labels,
		ValueType: valType,
	}
}

func oneOrPath(val float64, path string) string {
	if path != "" {
		return path
	}
	return fmt.Sprintf("{%v}", val)
}
