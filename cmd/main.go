package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/config"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/consul"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/docker"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/entry"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/logger"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/pkg/homer"
	"github.com/docker/docker/api/types/events"
	"github.com/hashicorp/consul/api/watch"
	"github.com/hashicorp/go-hclog"
)

// generateMu serialises config generation between the provider event loop and
// the base config watcher.
var generateMu sync.Mutex

// knownPages remembers every extra page written so far, so a page whose last
// container disappears is regenerated (emptied) instead of going stale.
var knownPages = map[string]bool{}

func main() {
	logger.Init()

	logger.Debug("Loading internal config")
	conf, err := config.New()
	if err != nil {
		logger.Fatal("Error parsing config", err)
	}

	if conf.ServiceDiscovery == config.Docker {
		logger.Debug("Retrieving Docker server version")
		serverVersion, err := docker.ServerVersion(conf.Docker)
		if err != nil {
			logger.Fatal("Failed to retrieve information about the Docker client and server host", err)
		}
		logger.Trace(fmt.Sprintf("Provider connection established with Docker %s (API %s)", serverVersion.Version, serverVersion.APIVersion))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The first generation must succeed: a wrong path or an invalid base config
	// is a configuration error, not a transient one.
	logger.Info("Building Homer config from base config")
	if err := generateConfig(ctx, conf); err != nil {
		logger.Fatal("Error generating Homer config", err)
	}

	if conf.HomerBaseConfigWatchInterval > 0 {
		logger.Info(fmt.Sprintf("Start watching base config %s for changes every %s", conf.HomerBaseConfigPath, conf.HomerBaseConfigWatchInterval))
		go watchBaseConfig(ctx, conf)
	}

	switch conf.ServiceDiscovery {
	case config.Docker:
		watchDocker(ctx, conf)
	case config.Consul:
		watchConsul(ctx, conf)
	}

	logger.Info("Shutting down")
}

// regenerate runs a generation triggered after start-up. Failures are logged
// and the previous config stays in place; the next event tries again.
func regenerate(ctx context.Context, conf config.Config) {
	if err := generateConfig(ctx, conf); err != nil {
		logger.Error("Error generating Homer config, keeping the previous one", err)
	}
}

// watchDocker follows the Docker event stream, regenerating the config once a
// burst of relevant events has settled. A failed stream is reopened after
// ReconnectInterval, with a regeneration to catch up on anything missed.
func watchDocker(ctx context.Context, conf config.Config) {
	logger.Info("Start watching for container creations and deletions")
	if conf.HomerDockerSwarmMode {
		logger.Info("Docker Swarm mode enabled")
		logger.Info("Start watching for service creations, updates, and removal")
	}

	for {
		streamErr := consumeDockerEvents(ctx, conf)
		if ctx.Err() != nil {
			return
		}
		logger.Warn(fmt.Sprintf("Docker event stream ended, reconnecting in %s", conf.ReconnectInterval), streamErr)
		select {
		case <-ctx.Done():
			return
		case <-time.After(conf.ReconnectInterval):
		}
		regenerate(ctx, conf)
	}
}

// consumeDockerEvents reads one event stream until it ends and returns why.
func consumeDockerEvents(ctx context.Context, conf config.Config) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	eventsc, errc := docker.WatchEvents(streamCtx, conf.Docker, conf.HomerDockerSwarmMode)

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	pending := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-eventsc:
			if !ok {
				return errors.New("event channel closed")
			}
			if !isRelevantEvent(event) {
				continue
			}
			logger.Trace(fmt.Sprintf("%+v", event))
			logger.Info(fmt.Sprintf("Event '%s' received from %s", string(event.Action), event.Actor.Attributes["name"]))
			if pending {
				if !debounce.Stop() {
					<-debounce.C
				}
			}
			debounce.Reset(conf.DebounceInterval)
			pending = true
		case <-debounce.C:
			pending = false
			logger.Info("Generating Homer config...")
			regenerate(ctx, conf)
		case err := <-errc:
			if errors.Is(err, io.EOF) {
				return errors.New("stream closed")
			}
			return err
		}
	}
}

func isRelevantEvent(event events.Message) bool {
	switch {
	case event.Action == "start", event.Action == "die":
		return true
	case strings.HasPrefix(string(event.Action), "health_status"):
		return true
	case event.Type == "service":
		return event.Action == "create" || event.Action == "update" || event.Action == "remove"
	}
	return false
}

// watchConsul blocks on a Consul services watch, restarting it when it fails.
func watchConsul(ctx context.Context, conf config.Config) {
	logger.Info("Start watching for Consul services change")
	hcLogger := hclog.New(&hclog.LoggerOptions{
		Name:       "consulcatalog",
		Level:      hclog.LevelFromString(conf.LogLevel),
		JSONFormat: true,
	})
	for ctx.Err() == nil {
		watcher := consul.WatchServices(conf.Consul)
		watcher.HybridHandler = func(_ watch.BlockingParamVal, _ interface{}) {
			logger.Debug("Consul handler fired")
			regenerate(ctx, conf)
		}
		go func() {
			<-ctx.Done()
			watcher.Stop()
		}()
		if err := watcher.RunWithClientAndHclog(conf.Consul, hcLogger); err != nil && ctx.Err() == nil {
			logger.Warn(fmt.Sprintf("Consul watch ended, restarting in %s", conf.ReconnectInterval), err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(conf.ReconnectInterval):
		}
	}
}

// watchBaseConfig polls the base config file and regenerates the Homer config
// whenever its modification time or size changes. Polling (rather than inotify)
// works reliably with Docker bind mounts and with editors that replace the file.
func watchBaseConfig(ctx context.Context, conf config.Config) {
	last, _ := fileSignature(conf.HomerBaseConfigPath)
	ticker := time.NewTicker(conf.HomerBaseConfigWatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, err := fileSignature(conf.HomerBaseConfigPath)
			if err != nil {
				logger.Warn("Failed to stat base config", err)
				continue
			}
			if current == last {
				continue
			}
			last = current
			logger.Info("Base config changed. Generating Homer config...")
			regenerate(ctx, conf)
		}
	}
}

func fileSignature(path string) (string, error) {
	stats, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", stats.ModTime().UnixNano(), stats.Size()), nil
}

// generateConfig discovers entries, merges them into the base config and
// writes every page. It returns an error instead of exiting so that callers
// decide whether a failure is fatal.
func generateConfig(ctx context.Context, conf config.Config) error {
	generateMu.Lock()
	defer generateMu.Unlock()

	parsedEntry, err := discoverEntries(ctx, conf)
	if err != nil {
		return err
	}

	logger.Debug("Loading base config")
	baseConfig, err := homer.GetConfig(conf.HomerBaseConfigPath)
	if err != nil {
		return fmt.Errorf("error getting base config: %w", err)
	}
	logger.Debug(fmt.Sprintf("Loaded base config: %+v", baseConfig))

	opts := homer.Options{
		CreateMissingServices: conf.HomerCreateMissingServices,
		DefaultType:           conf.HomerDefaultType,
	}

	pages := homer.GroupByPage(parsedEntry)
	for page := range knownPages {
		if _, ok := pages[page]; !ok {
			pages[page] = nil
		}
	}

	for page, entries := range pages {
		base := baseConfig
		output := conf.HomerConfigPath
		if page != homer.MainPage {
			base, err = loadPageBase(conf, baseConfig, page)
			if err != nil {
				return err
			}
			output = filepath.Join(filepath.Dir(conf.HomerConfigPath), page+".yml")
		}

		logger.Debug(fmt.Sprintf("Generating config for page %q", page))
		generatedConfig, err := homer.BuildConfig(base, entries, opts)
		if err != nil {
			return fmt.Errorf("error building Homer config for page %q: %w", page, err)
		}

		logger.Debug(fmt.Sprintf("Updating Homer config file %s", output))
		if err := homer.PutConfig(generatedConfig, output); err != nil {
			return fmt.Errorf("error updating Homer config file: %w", err)
		}
		if page != homer.MainPage {
			knownPages[page] = true
		}
	}

	logger.Info("Homer config was successfully generated and updated")

	return nil
}

// discoverEntries lists the provider's running containers/services with their
// labels. A single entry that cannot be inspected is skipped with an error log;
// a failure to list at all is returned so the previous config is kept.
func discoverEntries(ctx context.Context, conf config.Config) ([]entry.RawEntry, error) {
	var parsedEntry []entry.RawEntry
	switch conf.ServiceDiscovery {
	case config.Docker:
		logger.Debug("Getting Docker containers")
		containers, err := docker.ListRunningContainers(ctx, conf.Docker)
		if err != nil {
			return nil, fmt.Errorf("failed to list containers for Docker: %w", err)
		}
		for _, container := range containers {
			parsedContainer, err := docker.ParseContainer(ctx, conf.Docker, container)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to inspect container %s, skipping it", container.Names), err)
				continue
			}
			logger.Debug(fmt.Sprintf("Inspected container %s", parsedContainer.Name))
			parsedEntry = append(parsedEntry, parsedContainer)
		}

		if conf.HomerDockerSwarmMode {
			logger.Debug("Getting Docker services")
			services, err := docker.ListRunningServices(ctx, conf.Docker)
			if err != nil {
				return nil, fmt.Errorf("failed to list swarm services for Docker: %w", err)
			}
			for _, service := range services {
				parsedService, err := docker.ParseService(ctx, conf.Docker, service)
				if err != nil {
					logger.Error(fmt.Sprintf("Failed to inspect service %s, skipping it", service.Spec.Name), err)
					continue
				}
				parsedEntry = append(parsedEntry, parsedService)
			}
		}
	case config.Consul:
		logger.Debug("Getting Consul service")
		services, err := consul.ListServices(conf.Consul)
		if err != nil {
			return nil, fmt.Errorf("failed to list Consul services: %w", err)
		}
		for name, tags := range services {
			parsedEntry = append(parsedEntry, consul.ParseService(name, tags))
		}
	}
	return parsedEntry, nil
}

// loadPageBase returns the base config of an extra page: <base dir>/<page>.yml
// when it exists, otherwise the main base config without its services so the
// page shares title, theme and navigation links with the main page.
func loadPageBase(conf config.Config, mainBase homer.Config, page string) (homer.Config, error) {
	path := filepath.Join(filepath.Dir(conf.HomerBaseConfigPath), page+".yml")
	if _, err := os.Stat(path); err == nil {
		base, err := homer.GetConfig(path)
		if err != nil {
			return homer.Config{}, fmt.Errorf("error getting base config for page %q: %w", page, err)
		}
		return base, nil
	}
	logger.Debug(fmt.Sprintf("No base config at %s, deriving page %q from the main base config", path, page))
	mainBase.Services = nil
	return mainBase, nil
}
