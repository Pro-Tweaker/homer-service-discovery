package consul

import (
	"strings"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/entry"
	"github.com/hashicorp/consul/api"
)

func ListServices(consul *api.Client) map[string][]string {
	catalog := consul.Catalog()
	q := &api.QueryOptions{}
	services, _, _ := catalog.Services(q)
	return services
}

// ParseService turns Consul service tags of the form "key=value" into labels.
// Only the first "=" separates key from value so that values containing "="
// (URLs with query strings, API tokens, ...) are kept intact.
func ParseService(name string, tags []string) entry.RawEntry {
	s := entry.RawEntry{
		Name:   name,
		Labels: make(map[string]string, len(tags)),
	}
	for _, tag := range tags {
		key, value, _ := strings.Cut(tag, "=")
		s.Labels[key] = value
	}
	return s
}
