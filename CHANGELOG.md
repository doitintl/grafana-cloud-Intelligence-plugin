# Changelog

## 1.1.2 (2026-09-25)

**Fixes:**

- Return sanitized messages from the data source health check and resource endpoints instead of raw upstream errors; the underlying error is logged for operators
- Align the README usage instructions with the query editor controls

**Security:**

- Build the backend with Go 1.26.6 and update `golang.org/x/net`, `golang.org/x/text`, and `google.golang.org/grpc` to versions without known vulnerabilities

**Improvements:**

- Update Playwright E2E tooling to `@grafana/plugin-e2e` 3.14 for Grafana 13.2 and later

## 1.1.1 (2026-07-25)

**Improvements:**

- Build release artifacts with the plugin's required Go and Node.js toolchains
- Restore Grafana E2E coverage across supported and nightly versions

## 1.1.0 (2026-07-25)

**Features:**

- Cache successful report and ad-hoc query results for six hours
- Deduplicate identical in-flight requests and run API queries sequentially
- Return hierarchy frames for treemap dashboards exported from DoiT Console

**Improvements:**

- Show actionable authentication, throttling, timeout, network, and upstream service errors
- Allow queued queries to use their full execution timeout

## 1.0.0 (2026-07-24)

Initial release.

**Features:**

- Query saved DoiT Cloud Analytics reports as Grafana time series or tables
- Ad-hoc cost queries: metric, time interval, aggregation, group-by dimensions, and filters
- Optional Grafana time range override for report queries
- Grafana Alerting support (backend data source)
- Health check validating API connectivity and credentials
- Provisioned test environment with sample dashboard and data source
