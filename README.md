# Homer Service Discovery

This tool generates configuration files for [Homer](https://github.com/bastienwirtz/homer) using Docker container labels.

Using Docker container or Consul catalog labels for service discovery is inspired by the same approach used by [Traefik](https://doc.traefik.io/traefik/providers/docker/) and [Watchtower](https://github.com/containrrr/watchtower).
## Features

- Generates Homer config using container labels
- Watches for container changes or Consul changes
- Reloads when the base config changes
- Supports standalone Docker and Docker Swarm
- Creates groups (services) on the fly from labels
- Passes every Homer option through, including smart card options
- Generates extra Homer pages from a `homer.page` label
- Sort groups and items by priority
- Atomic config writes, debounced events and automatic reconnection to Docker / Consul
- Multi-arch

## Usage

Create a base configuration for this tool to build upon. This will contain your `services` sections, `title`, `logo`, etc. A value unique to this tool is `priority`. The higher the `priority` of an item, the higher on the list it will appear.

```yaml
title: My Self Hosted Kingdom
subtitle: Enter!

services:
  - name: Numbers
    icon: fa-solid fa-list-ol
    items:
      - name: 4
        icon: fa-solid fa-dice-four
        url: https://fontawesome.com/v5.15/icons/dice-four?style=solid
        priority: 4
      - name: 3
        icon: fa-brands fa-html5
        url: https://fontawesome.com/v5.15/icons/html5?style=brands
        priority: 3
      - name: 1
        icon: fa-regular fa-money-bill-alt
        url: https://fontawesome.com/v5.15/icons/money-bill-alt?style=regular
        priority: 1

  # Empty section
  - name: Letters
    icon: fa-solid fa-sun
    items: []
```

Icons use [Font Awesome 7](https://fontawesome.com/search) classes (`fa-solid`, `fa-regular`, `fa-brands`),
which Homer ships since v26.08. The older `fas`/`far`/`fab` prefixes still work as aliases.

Start Homer following [its instructions](https://github.com/bastienwirtz/homer/blob/main/README.md) to mount the `/www/assets` directory. This is where the `config.yaml` file resides.

```bash
docker run -d \
  -p 8080:8080 \
  -v /folder1/homer/:/www/assets \
  b4bz/homer:latest
```

Start this tool while:
- mounting the base config
- mounting homer's config
- mounting the Docker socket
- using environment variables to provide their mountpoints.

```bash
docker run -d \
  -v /folder1/homer/config.yml:/config.yml \
  -v /sd/base.yml:/base.yml \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/pro-tweaker/homer-service-discovery
```

You can visit your Homer Dashboard to see the initial generated config (remember to hard refresh).

![](docs/base.png)

Start adding labels to your containers to have this tool pick them up. For example, the following command will regenerate the config and add the `http-echo` service to the `Numbers` service.

```bash
docker run -d \
  -p 5678:5678 \
  -l homer.enable=true \
  -l homer.service=Numbers \
  -l homer.name='This is number 2!' \
  -l homer.subtitle='http-echo server' \
  -l homer.icon='fa-regular fa-hand-peace' \
  -l homer.priority=2 \
  -l homer.url='http://localhost:5678' \
  hashicorp/http-echo \
  --text="Be yourself; everyone else is already taken"
```

![](docs/base-added.png)

The full list of labels are:

| Label              | Description                                                                                    |
|--------------------|------------------------------------------------------------------------------------------------|
| `homer.enable`     | Set to `true` include in Homer's config                                                        |
| `homer.service`    | The name of Homer Service (group) to add this item to. Created if it is not in the base config |
| `homer.page`       | Write this item to the extra page `<page>.yml` instead of the main config (see below)          |
| `homer.priority`   | The priority level of the item. The higher the number, the higher it appears in the Service    |
| `homer.name`       | The item's name (See Homer's documentation)                                                    |
| `homer.logo`       | The item's logo (See Homer's documentation)                                                    |
| `homer.icon`       | The item's icon (See Homer's documentation)                                                    |
| `homer.subtitle`   | The item's subtitle (See Homer's documentation)                                                |
| `homer.tag`        | The item's tag (See Homer's documentation)                                                     |
| `homer.keywords`   | The item's search keywords (See Homer's documentation)                                         |
| `homer.url`        | The item's url (See Homer's documentation)                                                     |
| `homer.target`     | The item's target (See Homer's documentation)                                                  |
| `homer.tagstyle`   | The item's tagstyle (See Homer's documentation)                                                |
| `homer.type`       | The item's type (See Homer's documentation)                                                    |
| `homer.class`      | The item's class (See Homer's documentation)                                                   |
| `homer.background` | The item's background (See Homer's documentation)                                              |
| `homer.<anything>` | Any other key is copied to the item as-is. Use it for smart card options (see below)           |

Group options, applied when the group is created from labels (values set in the base config always win):

| Label                     | Description                                                      |
|---------------------------|------------------------------------------------------------------|
| `homer.service.icon`      | The group's icon                                                 |
| `homer.service.logo`      | The group's logo                                                 |
| `homer.service.class`     | The group's class                                                |
| `homer.service.tagstyle`  | The group's tagstyle                                             |
| `homer.service.priority`  | The group's priority. Higher priority groups are listed first    |

### Smart cards and other item options

Any `homer.<key>` label that is not in the table above is written to the item unchanged, so every option of
Homer's [smart cards](https://github.com/bastienwirtz/homer/blob/main/docs/customservices.md) and any key added
in future Homer releases works without an update of this tool:

- `true` / `false` become booleans and whole numbers become integers. Wrap a value in quotes
  (`homer.apikey='0123'`) to force a string.
- A dotted key creates a nested map: `homer.headers.Authorization=Bearer x` becomes `headers: {Authorization: Bearer x}`.
- Labels starting with `homer.service.` belong to the group (see the table above), not the item.

```bash
docker run -d \
  -l homer.enable=true \
  -l homer.service=Infra \
  -l homer.name=Proxmox \
  -l homer.type=Proxmox \
  -l homer.url=https://pve.local:8006 \
  -l homer.node=pve \
  -l 'homer.api_token=PVEAPIToken=root@pam!homer=xxxx' \
  -l homer.warning_value=50 \
  -l homer.hide_decimals=true \
  ...
```

### Base config passthrough

Every key in the base config is preserved in the generated config, including keys this tool does not know
about (`defaults`, `proxy`, `hotkey`, `updateIntervalMs`, group `class`/`logo`, smart card options on static
items, ...). Only the `items` lists of the `services` are modified.

Services (groups) also accept a `priority` key. Groups with a higher priority are listed first; groups with
the same priority keep the order of the base config.

The base config is checked for changes every `HOMER_BASE_CONFIG_WATCH_INTERVAL` (10 seconds by default) and
the Homer config is regenerated when it changed, so edits no longer wait for the next container event.

### Groups created from labels

When `homer.service` names a group that is not in the base config, the group is created with the
`homer.service.*` options of its containers. Set `HOMER_CREATE_MISSING_SERVICES=false` to get the old
behaviour of skipping such items with a warning.

```bash
docker run -d \
  -l homer.enable=true \
  -l homer.service=Media \
  -l homer.service.icon='fa-solid fa-film' \
  -l homer.service.priority=10 \
  -l homer.name=Jellyfin \
  ...
```

### Default smart card type

Set `HOMER_DEFAULT_TYPE` to give every item without a `homer.type` label a smart card, for example
`HOMER_DEFAULT_TYPE=Ping` to show a reachability indicator on every discovered service.

### Multiple pages

Homer supports [extra pages](https://github.com/bastienwirtz/homer/blob/main/docs/configuration.md): a
navbar link with `url: "#media"` loads `media.yml` from the same directory as the main config. Add
`homer.page=media` to a container to write its item to that page instead of the main config.

- The page file is written next to `HOMER_CONFIG` (e.g. `/www/assets/media.yml`) and is created if missing.
- If `<page>.yml` exists next to `HOMER_BASE_CONFIG`, it is used as the base of that page. Otherwise the
  main base config is reused without its `services`, so the page keeps the same title, theme and links.
- Page names may only contain letters, digits, `-` and `_`.

```yaml
# base.yml
links:
  - name: Media
    icon: fa-solid fa-film
    url: "#media"
```

```bash
docker run -d \
  -l homer.enable=true \
  -l homer.page=media \
  -l homer.service=Streaming \
  -l homer.name=Jellyfin \
  ...
```

## Environment Variables

| Key                 | Description                                   | Default                      |
|---------------------|-----------------------------------------------|------------------------------|
| `LOG_LEVEL`         | The level of log verbosity                    | `Info`                       |
| `HOMER_BASE_CONFIG` | Where the base config is located              | `/base.yml`                  |
| `HOMER_CONFIG`      | Where the Homer config is located             | `/config.yml`                |
| `SERVICE_DISCOVERY` | Either `Docker` or `Consul`                   | `Docker`                     |
| `HOMER_DOCKER_SWARM`| Docker Swarm supprt. Either `true` or `false` | `false`                      |
| `CONSUL_HOST`       | Host for Consul connnection                   | `127.0.0.1:8500`             |
| `HOMER_CREATE_MISSING_SERVICES` | Create groups named by `homer.service` that are missing from the base config | `true` |
| `HOMER_DEFAULT_TYPE` | Smart card `type` for items without `homer.type`, e.g. `Ping` | *(none)*     |
| `HOMER_BASE_CONFIG_WATCH_INTERVAL` | How often the base config is checked for changes. `0` disables it | `10s` |
| `DEBOUNCE_INTERVAL`  | Wait after the last Docker event before regenerating, so a burst of events causes one regeneration | `1s` |
| `RECONNECT_INTERVAL` | Pause before reconnecting after the Docker event stream or Consul watch fails | `5s` |
