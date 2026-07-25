# Changelog

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
