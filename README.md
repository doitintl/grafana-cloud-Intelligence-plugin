# Cloud Intelligence™ data source for Grafana

Visualize your multicloud cost analytics from [Cloud Intelligence™](https://www.doit.com/platform/) directly in Grafana. The plugin queries the [DoiT API](https://developer.doit.com/) live — no data export or sync required — so your dashboards always reflect the latest Cloud Analytics data across AWS, Google Cloud, Azure, Oracle Cloud and over [40 additional integrations](https://www.doit.com/integrations).

![Cloud cost dashboard powered by DoiT reports](https://raw.githubusercontent.com/doitintl/grafana-cloud-intelligence-plugin/main/src/img/screenshot-dashboard.png)

## Features

- **Saved reports**: Run any Cloud Analytics report from your DoiT Console and render its results as Grafana time series or tables.
- **Ad-hoc queries**: Build cost queries in Grafana — pick a metric (cost, usage, savings), group by dimensions (service, project, SKU, labels, …), and apply filters, without creating a report in the DoiT Console first.
- **Grafana time range**: Report queries can follow the dashboard time picker instead of the report's own time settings.
- **Alerting**: The data source supports Grafana Alerting; build alert rules on top of any report or ad-hoc query.
- **Dashboard export from DoiT Console**: The DoiT Console can generate ready-made Grafana dashboard JSON from any Cloud Analytics dashboard or report for use with this data source.

## Requirements

- Grafana 12.3.0 or later.
- A Cloud Intelligence™ account and a [DoiT API key](https://developer.doit.com/docs/start) with Cloud Analytics access.

## Configuration

1. In Grafana, go to **Connections → Data sources → Add new data source** and select **DoiT Cloud Intelligence**.
2. Set the following options:

   | Option  | Description                                                            |
   | ------- | ---------------------------------------------------------------------- |
   | API URL | DoiT API base URL. Defaults to `https://api.doit.com`.                 |
   | API Key | Your DoiT API key (stored encrypted via Grafana secure JSON data).     |

3. Click **Save & test**. The health check verifies connectivity and the API key.

To generate an API key, see the [DoiT API documentation](https://developer.doit.com/docs/start).

### Provisioning example

```yaml
apiVersion: 1

datasources:
  - name: DoiT Cloud Intelligence
    type: doitintl-doitcloudintelligence-datasource
    access: proxy
    jsonData:
      apiUrl: https://api.doit.com
    secureJsonData:
      apiKey: $DOIT_API_KEY
```

## Usage

### Query a saved report

1. Add a panel and choose the **DoiT Cloud Intelligence** data source.
2. Set **Query type** to **Report**.
3. Select a report from the drop-down (populated from your DoiT account).
4. Optionally enable **Use Grafana time range** to override the report's time settings with the dashboard time picker.

### Ad-hoc query

1. Set **Query type** to **Query**.
2. Choose a metric, time interval, and aggregation.
3. Add group-by dimensions and filters as needed.

Results are returned as time series frames (one series per group) suitable for time series, bar chart, and stat panels, or as a table for tabular reports.

### Alerting

The data source supports Grafana Alerting. Create an alert rule, choose this data source in the query, and add expressions (reduce, threshold) as usual.

## Getting help

- [Open an issue](https://github.com/doitintl/grafana-cloud-intelligence-plugin/issues) for bugs or feature requests.
- [DoiT API reference](https://developer.doit.com/reference) for the underlying Cloud Analytics API.

## Development

See [CONTRIBUTING.md](https://github.com/doitintl/grafana-cloud-intelligence-plugin/blob/main/CONTRIBUTING.md) for local development, build, and test instructions.

## License

[Apache-2.0](https://github.com/doitintl/grafana-cloud-intelligence-plugin/blob/main/LICENSE)
