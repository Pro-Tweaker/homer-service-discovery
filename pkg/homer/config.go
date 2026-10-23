package homer

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/entry"
	"github.com/Pro-Tweaker/homer-docker-service-discovery/internal/logger"
)

// LabelPrefix is the namespace of every label this tool reads.
const LabelPrefix = "homer."

const (
	EnableLabel     = LabelPrefix + "enable"
	ServiceLabel    = LabelPrefix + "service"
	PageLabel       = LabelPrefix + "page"
	NameLabel       = LabelPrefix + "name"
	LogoLabel       = LabelPrefix + "logo"
	IconLabel       = LabelPrefix + "icon"
	SubtitleLabel   = LabelPrefix + "subtitle"
	TagLabel        = LabelPrefix + "tag"
	KeywordsLabel   = LabelPrefix + "keywords"
	UrlLabel        = LabelPrefix + "url"
	TargetLabel     = LabelPrefix + "target"
	TagstyleLabel   = LabelPrefix + "tagstyle"
	TypeLabel       = LabelPrefix + "type"
	ClassLabel      = LabelPrefix + "class"
	BackgroundLabel = LabelPrefix + "background"
	PriorityLabel   = LabelPrefix + "priority"

	// Options of the group (service) the item belongs to. They are used when the
	// group is created on the fly and fill in blank fields of existing groups.
	ServiceIconLabel     = ServiceLabel + ".icon"
	ServiceLogoLabel     = ServiceLabel + ".logo"
	ServiceClassLabel    = ServiceLabel + ".class"
	ServiceTagstyleLabel = ServiceLabel + ".tagstyle"
	ServicePriorityLabel = ServiceLabel + ".priority"
)

// reservedLabels are consumed by this tool and never copied onto the item.
var reservedLabels = map[string]bool{
	EnableLabel:   true,
	ServiceLabel:  true,
	PageLabel:     true,
	PriorityLabel: true,
}

// typedLabels are mapped onto a typed Item field in buildItem.
var typedLabels = map[string]bool{
	NameLabel:       true,
	LogoLabel:       true,
	IconLabel:       true,
	SubtitleLabel:   true,
	TagLabel:        true,
	KeywordsLabel:   true,
	UrlLabel:        true,
	TargetLabel:     true,
	TagstyleLabel:   true,
	TypeLabel:       true,
	ClassLabel:      true,
	BackgroundLabel: true,
}

// Options tunes how discovered entries are merged into the base config.
type Options struct {
	// CreateMissingServices adds a group when homer.service names one that is
	// not in the base config instead of skipping the item.
	CreateMissingServices bool
	// DefaultType is applied to every item that has no homer.type label, e.g.
	// "Ping" to get a reachability indicator on every discovered service.
	DefaultType string
}

// pageNamePattern restricts page names to something that is safe to use as a
// file name next to the generated config.
var pageNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// MainPage is the key of the entries that belong to the main config file.
const MainPage = ""

// GroupByPage splits entries by their homer.page label. Entries without the
// label, or with an invalid page name, go to MainPage. The result always
// contains MainPage so the main config is regenerated even when it is empty.
func GroupByPage(entries []entry.RawEntry) map[string][]entry.RawEntry {
	pages := map[string][]entry.RawEntry{MainPage: nil}
	for _, e := range entries {
		page := e.GetLabelValueOrEmpty(PageLabel)
		if page != MainPage && !pageNamePattern.MatchString(page) {
			logger.Warn(fmt.Sprintf("Container %s: invalid page name %q, using the main page instead", e.Name, page), nil)
			page = MainPage
		}
		pages[page] = append(pages[page], e)
	}
	return pages
}

func BuildConfig(config Config, containers []entry.RawEntry, opts Options) (Config, error) {
	logger.Debug("Checking all container and their labels")
	for _, container := range containers {
		logger.Debug(fmt.Sprintf("Start checking Container %s", container.Name))

		logger.Debug(fmt.Sprintf("Container %s: Checking if label %s exists and is true", container.Name, EnableLabel))
		if !homerEnabled(container) {
			logger.Debug(fmt.Sprintf("Container %s: Label %s does not exist or is false", container.Name, EnableLabel))
			continue
		}
		logger.Debug(fmt.Sprintf("Container %s: Label %s exists and is true", container.Name, EnableLabel))

		logger.Debug(fmt.Sprintf("Container %s: Finding Homer Service from label %s", container.Name, ServiceLabel))
		i, found := findServiceFromLabel(container, config.Services)
		if !found {
			name := container.GetLabelValueOrEmpty(ServiceLabel)
			if name == "" || !opts.CreateMissingServices {
				logger.Warn(fmt.Sprintf("Container %s: No matching service found or label does not exist. Skipping", container.Name), nil)
				continue
			}
			logger.Info(fmt.Sprintf("Container %s: Creating Homer Service %s", container.Name, name))
			config.Services = append(config.Services, Service{Name: name})
			i = len(config.Services) - 1
		}
		logger.Debug(fmt.Sprintf("Container %s: Found matching Homer Service %s", container.Name, config.Services[i].Name))

		applyServiceOptions(&config.Services[i], container)

		item := buildItem(container)
		if item.Type == "" {
			item.Type = opts.DefaultType
		}

		logger.Debug(fmt.Sprintf("Container %s: Appending to Homer Service %s", container.Name, config.Services[i].Name))
		config.Services[i].Items = append(config.Services[i].Items, item)
	}

	logger.Debug("Sorting all services based on priority")
	for i := range config.Services {
		logger.Debug(fmt.Sprintf("Service %s: Sorting items", config.Services[i].Name))

		items := config.Services[i].Items
		sort.SliceStable(items, func(j, k int) bool {
			return items[j].Priority > items[k].Priority
		})
	}

	// Stable so that services with equal priority keep the order of the base config.
	sort.SliceStable(config.Services, func(j, k int) bool {
		return config.Services[j].Priority > config.Services[k].Priority
	})

	return config, nil
}

// applyServiceOptions fills blank group fields from the homer.service.* labels
// of an entry. Values already set in the base config always win.
func applyServiceOptions(s *Service, c entry.RawEntry) {
	if s.Icon == "" {
		s.Icon = c.GetLabelValueOrEmpty(ServiceIconLabel)
	}
	if s.Logo == "" {
		s.Logo = c.GetLabelValueOrEmpty(ServiceLogoLabel)
	}
	if s.Class == "" {
		s.Class = c.GetLabelValueOrEmpty(ServiceClassLabel)
	}
	if s.Tagstyle == "" {
		s.Tagstyle = c.GetLabelValueOrEmpty(ServiceTagstyleLabel)
	}
	if s.Priority == 0 {
		s.Priority = toIntOrZero(c.GetLabelValueOrEmpty(ServicePriorityLabel))
	}
}

// buildItem converts the labels of an entry into a Homer item. Known labels
// fill the typed fields; every other label under the "homer." prefix is copied
// as-is into the item so that smart card options (apikey, endpoint, headers,
// ...) and future Homer keys work without a code change.
func buildItem(c entry.RawEntry) Item {
	item := Item{
		Name:       c.GetLabelValueOrEmpty(NameLabel),
		Logo:       c.GetLabelValueOrEmpty(LogoLabel),
		Icon:       c.GetLabelValueOrEmpty(IconLabel),
		Subtitle:   c.GetLabelValueOrEmpty(SubtitleLabel),
		Tag:        c.GetLabelValueOrEmpty(TagLabel),
		Keywords:   c.GetLabelValueOrEmpty(KeywordsLabel),
		Url:        c.GetLabelValueOrEmpty(UrlLabel),
		Target:     c.GetLabelValueOrEmpty(TargetLabel),
		Tagstyle:   c.GetLabelValueOrEmpty(TagstyleLabel),
		Type:       c.GetLabelValueOrEmpty(TypeLabel),
		Class:      c.GetLabelValueOrEmpty(ClassLabel),
		Background: c.GetLabelValueOrEmpty(BackgroundLabel),
		// docker labels are strings but we want an int
		Priority: toIntOrZero(c.GetLabelValueOrEmpty(PriorityLabel)),
	}

	// Iterate in a deterministic order so the generated YAML is stable.
	keys := make([]string, 0, len(c.Labels))
	for label := range c.Labels {
		keys = append(keys, label)
	}
	sort.Strings(keys)

	for _, label := range keys {
		if !strings.HasPrefix(label, LabelPrefix) || reservedLabels[label] || typedLabels[label] {
			continue
		}
		path := strings.Split(strings.TrimPrefix(label, LabelPrefix), ".")
		if path[0] == "" || reservedLabels[LabelPrefix+path[0]] {
			// e.g. "homer.service.icon" belongs to the group, not the item.
			continue
		}
		if item.Extra == nil {
			item.Extra = map[string]interface{}{}
		}
		setNested(item.Extra, path, parseLabelValue(c.Labels[label]))
	}

	return item
}

// setNested stores value at path inside m, creating intermediate maps. A dotted
// label such as "homer.headers.Authorization" becomes headers: {Authorization: ...}.
func setNested(m map[string]interface{}, path []string, value interface{}) {
	for _, key := range path[:len(path)-1] {
		next, ok := m[key].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			m[key] = next
		}
		m = next
	}
	m[path[len(path)-1]] = value
}

// parseLabelValue converts a label string into the YAML type Homer expects:
// "true"/"false" become booleans and integers become numbers. Wrapping the
// value in single or double quotes forces a string (e.g. an all-digit API key).
func parseLabelValue(v string) interface{} {
	if len(v) >= 2 {
		first, last := v[0], v[len(v)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return v[1 : len(v)-1]
		}
	}
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.Atoi(v); err == nil {
		return i
	}
	return v
}

// homerEnabled checks if the container has the enabled label and it is true
func homerEnabled(c entry.RawEntry) bool {
	value, ok := c.Labels[EnableLabel]
	return ok && value == "true"
}

// findServiceFromLabel finds the service specified on the container's labels and returns its position
func findServiceFromLabel(c entry.RawEntry, services []Service) (int, bool) {
	if value, found := c.GetLabelValue(ServiceLabel); found {
		for index, service := range services {
			if service.Name == value {
				return index, true
			}
		}
	}

	return 0, false
}

func toIntOrZero(v string) int {
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return i
}
