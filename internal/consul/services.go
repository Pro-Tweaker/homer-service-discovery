package consul

import (
	"strings"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/entry"
	"github.com/hashicorp/consul/api"
)

func ListServices(consul *api.Client) (map[string][]string, error) {
	catalog := consul.Catalog()
	q := &api.QueryOptions{}
	services, _, err := catalog.Services(q)
	if err != nil {
		return nil, err
	}
	return services, nil
}

func ParseService(name string, labels []string) entry.RawEntry {
	s := entry.RawEntry{
		Name: name,
	}
	s.Labels = make(map[string]string)
	if len(labels) > 0 {
		for _, label := range labels {
			splitedLabel := strings.Split(label, "=")
			if len(splitedLabel) > 1 {
				s.Labels[splitedLabel[0]] = splitedLabel[1]
			} else {
				s.Labels[splitedLabel[0]] = ""
			}

		}
	}
	return s
}
