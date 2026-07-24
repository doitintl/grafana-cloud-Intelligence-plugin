# Test environment for reviewers

This directory provisions a ready-to-use test setup for the DoiT Cloud Intelligence data source, per the [Grafana test environment guidelines](https://grafana.com/developers/plugin-tools/publish-a-plugin/provide-test-environment).

## Quick start

```bash
npm install
npm run build          # build frontend into dist/
mage -v                # build backend binaries into dist/ (requires Go + Mage)
DOIT_API_KEY=<key> docker compose up
```

Grafana starts on `http://localhost:3002` with:

- **Data source** `DoiT Cloud Intelligence` (`datasources/datasources.yml`) — points at `https://api.doit.com`, API key taken from the `DOIT_API_KEY` environment variable.
- **Sample dashboard** "DoiT Cloud Intelligence — Sample" (`dashboards/sample-dashboard.json`) — two ad-hoc query panels (cost by service time series, cost by cloud provider pie chart) that work with any DoiT account, no saved reports required.

## Getting a test API key

The data source requires a DoiT Cloud Intelligence account API key. Reviewers can request a trial account and key at <https://www.doit.com/platform/> or contact the maintainers via GitHub issues, and we will provide test credentials for the review.

To generate a key on an existing account, see the [DoiT API docs](https://developer.doit.com/docs/start): Console → Profile → API → Generate key (Cloud Analytics permission required).

For more information about provisioning see [Provision dashboards and data sources](https://grafana.com/tutorials/provision-dashboards-and-data-sources/).
