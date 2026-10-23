package homer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/entry"
	"gopkg.in/yaml.v3"
)

func Test_buildItem(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   Item
	}{
		{
			name: "typed labels only",
			labels: map[string]string{
				EnableLabel:   "true",
				ServiceLabel:  "Apps",
				NameLabel:     "Echo",
				IconLabel:     "fas fa-comment",
				KeywordsLabel: "echo test",
				PriorityLabel: "7",
			},
			want: Item{Name: "Echo", Icon: "fas fa-comment", Keywords: "echo test", Priority: 7},
		},
		{
			name: "smart card options pass through with type coercion",
			labels: map[string]string{
				EnableLabel:                 "true",
				NameLabel:                   "Proxmox",
				TypeLabel:                   "Proxmox",
				PageLabel:                   "infra",
				"homer.node":                "pve",
				"homer.api_token":           "PVEAPIToken=root@pam!homer=abc",
				"homer.warning_value":       "50",
				"homer.hide_decimals":       "true",
				"homer.useCredentials":      "false",
				"homer.apikey":              "'0123'",
				"homer.headers.X-Api-Key":   "secret",
				"homer.headers.X-Other":     "\"42\"",
				"not.homer.label":           "ignored",
				ServiceIconLabel:            "belongs to the group",
				"homer.":                    "empty key ignored",
				"com.docker.compose.oneoff": "False",
			},
			want: Item{
				Name: "Proxmox",
				Type: "Proxmox",
				Extra: map[string]interface{}{
					"node":           "pve",
					"api_token":      "PVEAPIToken=root@pam!homer=abc",
					"warning_value":  50,
					"hide_decimals":  true,
					"useCredentials": false,
					"apikey":         "0123",
					"headers": map[string]interface{}{
						"X-Api-Key": "secret",
						"X-Other":   "42",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildItem(entry.RawEntry{Name: tt.name, Labels: tt.labels})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildItem() =\n\n%+v\n\nwant\n\n%+v", got, tt.want)
			}
		})
	}
}

func TestBuildConfig_sorting(t *testing.T) {
	base := Config{
		Services: []Service{
			{Name: "Low", Priority: 1, Items: []Item{{Name: "a", Priority: 1}}},
			{Name: "Default A"},
			{Name: "High", Priority: 10},
			{Name: "Default B"},
		},
	}
	entries := []entry.RawEntry{
		{Name: "c1", Labels: map[string]string{EnableLabel: "true", ServiceLabel: "High", NameLabel: "x", PriorityLabel: "2"}},
		{Name: "c2", Labels: map[string]string{EnableLabel: "true", ServiceLabel: "High", NameLabel: "y", PriorityLabel: "5"}},
		{Name: "c3", Labels: map[string]string{EnableLabel: "true", ServiceLabel: "Low", NameLabel: "b", PriorityLabel: "3"}},
		{Name: "c4", Labels: map[string]string{EnableLabel: "true", ServiceLabel: "Missing", NameLabel: "skipped"}},
		{Name: "c5", Labels: map[string]string{EnableLabel: "false", ServiceLabel: "High", NameLabel: "disabled"}},
	}

	got, err := BuildConfig(base, entries, Options{})
	if err != nil {
		t.Fatalf("BuildConfig() error = %v", err)
	}

	if order := serviceNames(got); !reflect.DeepEqual(order, []string{"High", "Low", "Default A", "Default B"}) {
		t.Errorf("service order = %v", order)
	}
	if names := itemNames(got.Services[0]); !reflect.DeepEqual(names, []string{"y", "x"}) {
		t.Errorf("High items = %v", names)
	}
	if names := itemNames(got.Services[1]); !reflect.DeepEqual(names, []string{"b", "a"}) {
		t.Errorf("Low items = %v", names)
	}
}

func TestBuildConfig_createMissingServices(t *testing.T) {
	base := Config{Services: []Service{{Name: "Existing", Icon: "fas fa-box"}}}
	entries := []entry.RawEntry{
		// Options arrive on a later container than the one that creates the group.
		{Name: "c1", Labels: map[string]string{EnableLabel: "true", ServiceLabel: "Media", NameLabel: "Plex"}},
		{Name: "c2", Labels: map[string]string{
			EnableLabel: "true", ServiceLabel: "Media", NameLabel: "Jellyfin",
			ServiceIconLabel: "fas fa-film", ServicePriorityLabel: "5", ServiceClassLabel: "wide",
		}},
		// Base config values are never overridden by labels.
		{Name: "c3", Labels: map[string]string{EnableLabel: "true", ServiceLabel: "Existing", NameLabel: "x", ServiceIconLabel: "fas fa-other"}},
		// A missing service name is still skipped.
		{Name: "c4", Labels: map[string]string{EnableLabel: "true", NameLabel: "no service"}},
	}

	got, err := BuildConfig(base, entries, Options{CreateMissingServices: true, DefaultType: "Ping"})
	if err != nil {
		t.Fatalf("BuildConfig() error = %v", err)
	}

	if order := serviceNames(got); !reflect.DeepEqual(order, []string{"Media", "Existing"}) {
		t.Fatalf("service order = %v", order)
	}
	media := got.Services[0]
	if media.Icon != "fas fa-film" || media.Priority != 5 || media.Class != "wide" {
		t.Errorf("Media group options not applied: %+v", media)
	}
	if names := itemNames(media); !reflect.DeepEqual(names, []string{"Plex", "Jellyfin"}) {
		t.Errorf("Media items = %v", names)
	}
	for _, item := range media.Items {
		if item.Type != "Ping" {
			t.Errorf("item %s type = %q, want Ping", item.Name, item.Type)
		}
	}
	if got.Services[1].Icon != "fas fa-box" {
		t.Errorf("base icon overridden: %q", got.Services[1].Icon)
	}
	if len(got.Services[1].Items) != 1 {
		t.Errorf("Existing items = %v", itemNames(got.Services[1]))
	}

	// Disabled: the unknown group is skipped as before.
	got, _ = BuildConfig(Config{}, entries[:1], Options{})
	if len(got.Services) != 0 {
		t.Errorf("service created while disabled: %v", serviceNames(got))
	}
}

func TestGroupByPage(t *testing.T) {
	entries := []entry.RawEntry{
		{Name: "a", Labels: map[string]string{}},
		{Name: "b", Labels: map[string]string{PageLabel: "media"}},
		{Name: "c", Labels: map[string]string{PageLabel: "../etc"}},
		{Name: "d", Labels: map[string]string{PageLabel: "media"}},
	}
	got := GroupByPage(entries)
	names := func(es []entry.RawEntry) []string {
		var n []string
		for _, e := range es {
			n = append(n, e.Name)
		}
		return n
	}
	if n := names(got[MainPage]); !reflect.DeepEqual(n, []string{"a", "c"}) {
		t.Errorf("main page = %v", n)
	}
	if n := names(got["media"]); !reflect.DeepEqual(n, []string{"b", "d"}) {
		t.Errorf("media page = %v", n)
	}
	if len(got) != 2 {
		t.Errorf("pages = %v", got)
	}
	if _, ok := GroupByPage(nil)[MainPage]; !ok {
		t.Error("main page missing for empty input")
	}
}

func TestPutConfig_createsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.yml")
	if err := PutConfig(Config{Title: "Page"}, path); err != nil {
		t.Fatalf("PutConfig() error = %v", err)
	}
	got, err := GetConfig(path)
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if got.Title != "Page" {
		t.Errorf("title = %q", got.Title)
	}
	// Existing file is overwritten.
	if err := PutConfig(Config{Title: "Again"}, path); err != nil {
		t.Fatalf("PutConfig() second error = %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "title: Again\nheader: false\n" {
		t.Errorf("content = %q", b)
	}
	// No temporary file is left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("leftover files: %v", names)
	}
}

// Unknown keys at every level must survive a load + generate + save cycle.
func Test_unknownKeysRoundTrip(t *testing.T) {
	in := `
title: Dashboard
documentTitle: Tab title
hotkey:
  search: "Shift"
updateIntervalMs: 5000
proxy:
  useCredentials: true
  headers:
    Authorization: Bearer x
defaults:
  layout: list
  colorTheme: dark
colors:
  light:
    link: "#123456"
  dark:
    something-new: value
message:
  title: Hi
  newMessageKey: yes
links:
  - name: Page 2
    url: "#page2"
    newLinkKey: 1
services:
  - name: Infra
    logo: group.png
    class: wide
    tagstyle: is-info
    newGroupKey: g
    items:
      - name: Proxmox
        type: Proxmox
        node: pve
        api_token: tok
        warning_value: 50
        hide_decimals: true
        headers:
          X-Api-Key: abc
`
	config, err := unmarshalConfig([]byte(in))
	if err != nil {
		t.Fatalf("unmarshalConfig() error = %v", err)
	}

	config, err = BuildConfig(config, nil, Options{})
	if err != nil {
		t.Fatalf("BuildConfig() error = %v", err)
	}

	out, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	var want, got map[string]interface{}
	if err := yaml.Unmarshal([]byte(in), &want); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	// "header" has no omitempty and is always written; it is not part of the input.
	delete(got, "header")

	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the config.\n\ngot:\n%s\n\nwant:\n%s", out, in)
	}
}

func serviceNames(c Config) []string {
	var names []string
	for _, s := range c.Services {
		names = append(names, s.Name)
	}
	return names
}

func itemNames(s Service) []string {
	var names []string
	for _, i := range s.Items {
		names = append(names, i.Name)
	}
	return names
}
