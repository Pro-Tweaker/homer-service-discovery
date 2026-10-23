package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/config"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/consul"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/docker"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/entry"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/logger"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/pkg/homer"
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
	logger.Info("Building Homer config from base config")

	ctx := context.Background()
	err = generateConfig(ctx, conf)
	if err != nil {
		logger.Fatal("Error generating Homer config", err)
	}

	if conf.HomerBaseConfigWatchInterval > 0 {
		logger.Info(fmt.Sprintf("Start watching base config %s for changes every %s", conf.HomerBaseConfigPath, conf.HomerBaseConfigWatchInterval))
		go watchBaseConfig(ctx, conf)
	}

	switch conf.ServiceDiscovery {
	case config.Docker:
		logger.Info("Start watching for container creations and deletions")
		if conf.HomerDockerSwarmMode {
			logger.Info("Docker Swarm mode enabled")
			logger.Info("Start watching for service creations, updates, and removal")
		}
		eventsc, errc := docker.WatchEvents(ctx, conf.Docker, conf.HomerDockerSwarmMode)
		for {
			select {
			case event, ok := <-eventsc:
				if !ok {
					logger.Fatal("Error from Docker events", nil)
				}

				if event.Action == "start" || event.Action == "die" || strings.HasPrefix(string(event.Action), "health_status") || (event.Type == "service" && (event.Action == "create" || event.Action == "update" || event.Action == "remove")) {
					logger.Trace(fmt.Sprintf("%+v", event))
					logger.Debug("A " + string(event.Action) + " event occurred")
					logger.Info(fmt.Sprintf("Event '%s' received from %s. Generating Homer config...", string(event.Action), event.Actor.Attributes["name"]))
					time.Sleep(1 * time.Second)
					err = generateConfig(ctx, conf)
					if err != nil {
						logger.Fatal("Error generating Homer config", err)
					}
				}
			case err := <-errc:
				if errors.Is(err, io.EOF) {
					logger.Debug("Provider event stream closed")
				} else {
					logger.Fatal("Error watching Docker events", err)
				}
			case <-ctx.Done():
				return
			}
		}
	case config.Consul:
		logger.Info("Start watching for Consul services change")
		hcLogger := hclog.New(&hclog.LoggerOptions{
			Name:       "consulcatalog",
			Level:      hclog.LevelFromString(conf.LogLevel),
			JSONFormat: true,
		})
		for {
			watcher := consul.WatchServices(conf.Consul)
			watcher.HybridHandler = func(_ watch.BlockingParamVal, _ interface{}) {
				logger.Debug("Consul handler fired")
				err = generateConfig(ctx, conf)
			}
			err = watcher.RunWithClientAndHclog(conf.Consul, hcLogger)
			time.Sleep(1 * time.Second)
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
			if err := generateConfig(ctx, conf); err != nil {
				logger.Error("Error generating Homer config", err)
			}
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

func generateConfig(ctx context.Context, conf config.Config) error {
	generateMu.Lock()
	defer generateMu.Unlock()

	var parsedEntry []entry.RawEntry
	switch conf.ServiceDiscovery {
	case config.Docker:
		logger.Debug("Getting Docker containers")
		containers, err := docker.ListRunningContainers(ctx, conf.Docker)
		if err != nil {
			logger.Fatal("Failed to list containers for Docker", err)
		}
		for _, container := range containers {
			parsedContainer, err := docker.ParseContainer(ctx, conf.Docker, container)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to inspect container %s", container.Names), err)
			}
			logger.Debug(fmt.Sprintf("Inspected container %s", parsedContainer.Name))
			parsedEntry = append(parsedEntry, parsedContainer)
		}

		if conf.HomerDockerSwarmMode {
			logger.Debug("Getting Docker services")
			services, err := docker.ListRunningServices(ctx, conf.Docker)
			if err != nil {
				logger.Fatal("Failed to list swarm services for Docker", err)
			}
			for _, service := range services {
				parsedService, err := docker.ParseService(ctx, conf.Docker, service)
				if err != nil {
					logger.Error(fmt.Sprintf("Failed to inspect service %s", service.Spec.Name), err)
				}
				parsedEntry = append(parsedEntry, parsedService)
			}
		}
	case config.Consul:
		logger.Debug("Getting Consul service")
		services := consul.ListServices(conf.Consul)
		for name, label := range services {

			parsedService := consul.ParseService(name, label)
			parsedEntry = append(parsedEntry, parsedService)
		}
	}

	logger.Debug("Loading base config")
	baseConfig, err := homer.GetConfig(conf.HomerBaseConfigPath)
	if err != nil {
		logger.Fatal("Error getting base config", err)
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
			base = loadPageBase(conf, baseConfig, page)
			output = filepath.Join(filepath.Dir(conf.HomerConfigPath), page+".yml")
			knownPages[page] = true
		}

		logger.Debug(fmt.Sprintf("Generating config for page %q", page))
		generatedConfig, err := homer.BuildConfig(base, entries, opts)
		if err != nil {
			logger.Fatal("Error building Homer config", err)
		}

		logger.Debug(fmt.Sprintf("Updating Homer config file %s", output))
		err = homer.PutConfig(generatedConfig, output)
		if err != nil {
			logger.Fatal("Error updating Homer config file", err)
		}
	}

	logger.Info("Homer config was successfully generated and updated")

	return nil
}

// loadPageBase returns the base config of an extra page: <base dir>/<page>.yml
// when it exists, otherwise the main base config without its services so the
// page shares title, theme and navigation links with the main page.
func loadPageBase(conf config.Config, mainBase homer.Config, page string) homer.Config {
	path := filepath.Join(filepath.Dir(conf.HomerBaseConfigPath), page+".yml")
	if _, err := os.Stat(path); err == nil {
		base, err := homer.GetConfig(path)
		if err != nil {
			logger.Fatal(fmt.Sprintf("Error getting base config for page %q", page), err)
		}
		return base
	}
	logger.Debug(fmt.Sprintf("No base config at %s, deriving page %q from the main base config", path, page))
	mainBase.Services = nil
	return mainBase
}
