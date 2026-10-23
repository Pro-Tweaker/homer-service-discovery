package config

import (
	"fmt"
	"time"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/consul"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/docker"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/logger"
	"github.com/docker/docker/client"
	"github.com/hashicorp/consul/api"

	"github.com/caarlos0/env/v6"
)

const (
	Docker = "Docker"
	Consul = "Consul"
)

type Config struct {
	Docker *client.Client

	Consul *api.Client

	ServiceDiscovery string `env:"SERVICE_DISCOVERY" envDefault:"Docker"`

	ConsulHost string `env:"CONSUL_HOST" envDefault:"127.0.0.1:8500"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"Info"`

	HomerBaseConfigPath string `env:"HOMER_BASE_CONFIG" envDefault:"/base.yml"`

	HomerConfigPath string `env:"HOMER_CONFIG" envDefault:"/config.yml"`

	HomerDockerSwarmMode bool `env:"HOMER_DOCKER_SWARM" envDefault:"false"`

	// HomerCreateMissingServices creates a group for a homer.service value that
	// is not in the base config instead of skipping the item.
	HomerCreateMissingServices bool `env:"HOMER_CREATE_MISSING_SERVICES" envDefault:"true"`

	// HomerDefaultType is the smart card type given to items without homer.type.
	HomerDefaultType string `env:"HOMER_DEFAULT_TYPE" envDefault:""`

	// HomerBaseConfigWatchInterval is how often the base config is checked for
	// changes. Zero disables the watcher.
	HomerBaseConfigWatchInterval time.Duration `env:"HOMER_BASE_CONFIG_WATCH_INTERVAL" envDefault:"10s"`
}

func New() (Config, error) {
	var err error
	conf := Config{}

	if err := env.Parse(&conf); err != nil {
		return Config{}, fmt.Errorf("error parsing config from env: %w", err)
	}

	switch conf.ServiceDiscovery {
	case Docker:
		conf.Docker, err = docker.CreateClient()
		if err != nil {
			return Config{}, fmt.Errorf("error creating Docker client: %w", err)
		}
	case Consul:
		conf.Consul, err = consul.CreateClient(conf.ConsulHost)
		if err != nil {
			return Config{}, fmt.Errorf("error creating Consul client: %w", err)
		}
	default:
		return Config{}, fmt.Errorf("unknown service discovery %q in configuration", conf.ServiceDiscovery)
	}
	err = logger.SetLevel(conf.LogLevel)
	if err != nil {
		return Config{}, fmt.Errorf("error setting log level: %w", err)
	}

	logger.Debug(fmt.Sprintf("%+v", conf))

	return conf, nil
}
