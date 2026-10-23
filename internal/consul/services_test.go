package consul

import (
	"reflect"
	"testing"
)

func TestParseService(t *testing.T) {
	got := ParseService("web", []string{
		"homer.enable=true",
		"homer.url=https://example.com/?a=1&b=2",
		"homer.api_token=PVEAPIToken=root@pam!homer=abc",
		"flagonly",
	})
	want := map[string]string{
		"homer.enable":    "true",
		"homer.url":       "https://example.com/?a=1&b=2",
		"homer.api_token": "PVEAPIToken=root@pam!homer=abc",
		"flagonly":        "",
	}
	if got.Name != "web" {
		t.Errorf("name = %q", got.Name)
	}
	if !reflect.DeepEqual(got.Labels, want) {
		t.Errorf("labels = %v, want %v", got.Labels, want)
	}
}
