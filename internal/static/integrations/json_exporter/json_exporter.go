package json_exporter

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/alloy/internal/static/integrations"
	"github.com/grafana/alloy/internal/static/integrations/config"
	json_config "github.com/prometheus-community/json_exporter/config"
	"github.com/prometheus-community/json_exporter/exporter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v2"
)

// LoadConfig loads the json config from the given file or from embedded yaml block
// it also validates that targets are properly defined
func LoadConfig(log log.Logger, configFile string, targets []JSONTarget, modules *json_config.Config) (*json_config.Config, error) {
	var err error

	if configFile != "" {
		modules, err = loadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load json config from file %v: %w", configFile, err)
		}
	}

	// The `name` and `address` fields are mandatory for the JSON targets.
	for _, target := range targets {
		if target.Name == "" || target.Target == "" {
			return nil, fmt.Errorf("failed to load json_targets; the `name` and `address` fields are mandatory")
		}
	}

	// Apply defaults if modules were loaded from inline config or file
	if modules != nil {
		for _, module := range modules.Modules {
			for i := 0; i < len(module.Metrics); i++ {
				if module.Metrics[i].Type == "" {
					module.Metrics[i].Type = json_config.ValueScrape
				}
				if module.Metrics[i].Help == "" {
					module.Metrics[i].Help = module.Metrics[i].Name
				}
				if module.Metrics[i].ValueType == "" {
					module.Metrics[i].ValueType = json_config.ValueTypeUntyped
				}
			}
		}
	}

	return modules, nil
}

func loadFile(filename string) (*json_config.Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg json_config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Complete Defaults (logic taken from json_exporter/config/config.go)
	for _, module := range cfg.Modules {
		for i := 0; i < len(module.Metrics); i++ {
			if module.Metrics[i].Type == "" {
				module.Metrics[i].Type = json_config.ValueScrape
			}
			if module.Metrics[i].Help == "" {
				module.Metrics[i].Help = module.Metrics[i].Name
			}
			if module.Metrics[i].ValueType == "" {
				module.Metrics[i].ValueType = json_config.ValueTypeUntyped
			}
		}
	}

	return &cfg, nil
}

// New creates a new json_exporter integration
func New(log log.Logger, c *Config) (integrations.Integration, error) {
	if c.JSONConfigFile == "" && c.JSONConfig == nil {
		return nil, fmt.Errorf("failed to load json config; no config file or config block provided")
	}

	var modules json_config.Config
	if c.JSONConfig != nil {
		if err := yaml.Unmarshal(c.JSONConfig, &modules); err != nil {
			return nil, err
		}
	}

	loadedModules, err := LoadConfig(log, c.JSONConfigFile, c.JSONTargets, &modules)
	if err != nil {
		return nil, err
	}

	integration := &Integration{
		cfg:     c,
		modules: loadedModules,
		log:     log,
	}
	return integration, nil
}

// Integration is the json integration.
type Integration struct {
	cfg     *Config
	modules *json_config.Config
	log     log.Logger
}

// MetricsHandler implements Integration.
func (i *Integration) MetricsHandler() (http.Handler, error) {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slogLogger := slog.New(&slogHandler{l: i.log})
		handler(w, r, slogLogger, *i.modules)
	}), nil
}

// Run satisfies Integration.Run.
func (i *Integration) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// ScrapeConfigs satisfies Integration.ScrapeConfigs.
func (i *Integration) ScrapeConfigs() []config.ScrapeConfig {
	var res []config.ScrapeConfig
	for _, target := range i.cfg.JSONTargets {
		queryParams := url.Values{}
		queryParams.Add("target", target.Target)
		if target.Module != "" {
			queryParams.Add("module", target.Module)
		}
		// Add labels as query parameters? No, existing json_exporter logic doesn't take labels from query params.
		// However, existing json_exporter documentation says:
		// "The json_exporter creates metrics from JSON data. The json path to the data is defined in the configuration file."
		// It doesn't seem to support passing extra labels via query params to the probe endpoint usually.
		// But blackbox_exporter does.
		// If we want to support the labels defined in JSONTarget, we might need to use `relabel_configs` in the ScrapeConfig
		// or find a way to pass them.
		// For now, let's just stick to target and module.

		res = append(res, config.ScrapeConfig{
			JobName:     i.cfg.Name() + "/" + target.Name,
			MetricsPath: "/metrics",
			QueryParams: queryParams,
		})
	}
	return res
}

// Logic adapted from json_exporter/cmd/main.go
func handler(w http.ResponseWriter, r *http.Request, logger *slog.Logger, config json_config.Config) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(ctx)

	module := r.URL.Query().Get("module")
	if module == "" {
		module = "default"
	}
	if _, ok := config.Modules[module]; !ok {
		http.Error(w, fmt.Sprintf("Unknown module %q", module), http.StatusBadRequest)
		logger.Debug("Unknown module", "module", module)
		return
	}

	registry := prometheus.NewPedanticRegistry()

	metrics, err := exporter.CreateMetricsList(config.Modules[module])
	if err != nil {
		logger.Error("Failed to create metrics list from config", "err", err)
	}

	jsonMetricCollector := exporter.JSONMetricCollector{JSONMetrics: metrics}
	jsonMetricCollector.Logger = logger

	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "Target parameter is missing", http.StatusBadRequest)
		return
	}

	fetcher := exporter.NewJSONFetcher(ctx, logger, config.Modules[module], r.URL.Query())
	data, err := fetcher.FetchJSON(target)
	if err != nil {
		http.Error(w, "Failed to fetch JSON response. TARGET: "+target+", ERROR: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	jsonMetricCollector.Data = data

	registry.MustRegister(jsonMetricCollector)
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

// slogHandler adapts go-kit/log.Logger to valid slog.Handler
type slogHandler struct {
	l     log.Logger
	attrs []any
	group string
}

func (h *slogHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return true
}

func (h *slogHandler) Handle(ctx context.Context, r slog.Record) error {
	levelVal := level.InfoValue()
	switch r.Level {
	case slog.LevelDebug:
		levelVal = level.DebugValue()
	case slog.LevelInfo:
		levelVal = level.InfoValue()
	case slog.LevelWarn:
		levelVal = level.WarnValue()
	case slog.LevelError:
		levelVal = level.ErrorValue()
	}

	args := []any{level.Key(), levelVal, "msg", r.Message}

	// Add attributes from handler
	args = append(args, h.attrs...)

	// Add attributes from record
	r.Attrs(func(a slog.Attr) bool {
		args = append(args, a.Key, a.Value.Any())
		return true
	})

	return h.l.Log(args...)
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]any, 0, len(attrs)*2)
	for _, a := range attrs {
		newAttrs = append(newAttrs, a.Key, a.Value.Any())
	}
	return &slogHandler{
		l:     h.l,
		attrs: append(h.attrs, newAttrs...),
		group: h.group,
	}
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	return &slogHandler{
		l:     h.l,
		attrs: h.attrs,
		group: h.group + name + ".", // Simplified group handling
	}
}
