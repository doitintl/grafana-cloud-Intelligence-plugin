# Contributing

## Prerequisites

- Node.js 22+
- Go 1.24+
- [Mage](https://magefile.org/)
- Docker (for the local Grafana instance)

## Backend

Build plugin backend binaries for Linux, Windows and Darwin:

```bash
mage -v build:linux build:linuxARM64 build:darwin build:darwinARM64 build:windows
```

List all available Mage targets:

```bash
mage -l
```

Run backend tests:

```bash
go test ./...
```

## Frontend

```bash
npm install

# development build with watch
npm run dev

# production build
npm run build

# unit tests
npm run test:ci

# lint
npm run lint
```

## Local Grafana with the plugin

```bash
npm run server
```

This starts Grafana on `http://localhost:3002` via Docker Compose with the plugin mounted from `dist/` and the provisioned data source and sample dashboard from `provisioning/`. Set your DoiT API key through the environment:

```bash
DOIT_API_KEY=<your-key> npm run server
```

## E2E tests

```bash
npm run server
npm run e2e
```

## Releasing

The plugin is signed and packaged via the `@grafana/create-plugin` GitHub Actions release workflow. To cut a release:

1. Update `CHANGELOG.md`.
2. Run `npm version <major|minor|patch>`.
3. Run `git push origin main --follow-tags`.

Signing requires the `GRAFANA_API_KEY` repository secret (a Grafana Cloud API key with the `PluginPublisher` role). See [Sign a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin).
