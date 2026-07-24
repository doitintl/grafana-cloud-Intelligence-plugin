import React, { useEffect, useMemo, useState } from 'react';
import { InlineField, InlineFieldRow, InlineSwitch, MultiSelect, RadioButtonGroup, Select } from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import { AdHocConfig, Dimension, DoitDataSourceOptions, DoitQuery, DoitQueryType, ReportListItem } from '../types';

type Props = QueryEditorProps<DataSource, DoitQuery, DoitDataSourceOptions>;

const QUERY_TYPE_OPTIONS: Array<SelectableValue<DoitQueryType>> = [
  { label: 'Saved report', value: 'report' },
  { label: 'Ad-hoc query', value: 'query' },
];

const METRIC_OPTIONS: Array<SelectableValue<string>> = [
  { label: 'Cost', value: 'cost' },
  { label: 'Usage', value: 'usage' },
  { label: 'Savings', value: 'savings' },
];

const TIME_INTERVAL_OPTIONS: Array<SelectableValue<string>> = [
  { label: 'Hour', value: 'hour' },
  { label: 'Day', value: 'day' },
  { label: 'Week', value: 'week' },
  { label: 'Month', value: 'month' },
];

const DEFAULT_ADHOC_CONFIG: AdHocConfig = {
  metric: { type: 'basic', value: 'cost' },
  timeInterval: 'day',
  timeRange: { mode: 'last', amount: 30, unit: 'day', includeCurrent: true },
  aggregation: 'total',
  dimensions: [
    { id: 'year', type: 'datetime' },
    { id: 'month', type: 'datetime' },
    { id: 'day', type: 'datetime' },
  ],
};

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const [reports, setReports] = useState<ReportListItem[]>([]);
  const [dimensions, setDimensions] = useState<Dimension[]>([]);

  const queryType: DoitQueryType = query.doitQueryType ?? 'report';

  useEffect(() => {
    datasource.listReports().then(setReports).catch(() => setReports([]));
  }, [datasource]);

  useEffect(() => {
    if (queryType === 'query') {
      datasource.listDimensions().then(setDimensions).catch(() => setDimensions([]));
    }
  }, [datasource, queryType]);

  const reportOptions = useMemo(
    () => reports.map((report) => ({ label: report.reportName, value: report.id, description: report.type })),
    [reports]
  );

  const dimensionOptions = useMemo(
    () => dimensions.map((dimension) => ({ label: dimension.label, value: `${dimension.type}:${dimension.id}` })),
    [dimensions]
  );

  const config = query.config ?? DEFAULT_ADHOC_CONFIG;

  const onQueryTypeChange = (value: DoitQueryType) => {
    onChange({ ...query, doitQueryType: value, config: value === 'query' ? config : query.config });
  };

  const onReportChange = (option: SelectableValue<string>) => {
    onChange({ ...query, reportId: option.value, reportName: option.label });
    onRunQuery();
  };

  const onUseGrafanaTimeChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, useGrafanaTimeRange: event.currentTarget.checked });
    onRunQuery();
  };

  const onMetricChange = (option: SelectableValue<string>) => {
    onChange({ ...query, config: { ...config, metric: { type: 'basic', value: option.value ?? 'cost' } } });
    onRunQuery();
  };

  const onTimeIntervalChange = (option: SelectableValue<string>) => {
    onChange({ ...query, config: { ...config, timeInterval: option.value } });
    onRunQuery();
  };

  const onGroupByChange = (options: Array<SelectableValue<string>>) => {
    const group = options
      .filter((option) => option.value)
      .map((option) => {
        const [type, ...idParts] = option.value!.split(':');
        return { type, id: idParts.join(':') };
      });

    onChange({ ...query, config: { ...config, group } });
    onRunQuery();
  };

  const groupByValue = useMemo(
    () => (config.group ?? []).map((group) => `${group.type}:${group.id}`),
    [config.group]
  );

  return (
    <>
      <InlineFieldRow>
        <InlineField label="Query type" labelWidth={16}>
          <RadioButtonGroup options={QUERY_TYPE_OPTIONS} value={queryType} onChange={onQueryTypeChange} />
        </InlineField>
      </InlineFieldRow>

      {queryType === 'report' && (
        <InlineFieldRow>
          <InlineField label="Report" labelWidth={16} grow tooltip="Saved Cloud Analytics report to run">
            <Select
              inputId="query-editor-report"
              options={reportOptions}
              value={query.reportId}
              onChange={onReportChange}
              placeholder="Select a report"
              isLoading={reports.length === 0}
              allowCustomValue
            />
          </InlineField>
          <InlineField
            label="Use dashboard time"
            labelWidth={20}
            tooltip="Override the report time settings with the Grafana time range"
          >
            <InlineSwitch
              id="query-editor-use-grafana-time"
              value={query.useGrafanaTimeRange ?? true}
              onChange={onUseGrafanaTimeChange}
            />
          </InlineField>
        </InlineFieldRow>
      )}

      {queryType === 'query' && (
        <>
          <InlineFieldRow>
            <InlineField label="Metric" labelWidth={16}>
              <Select
                inputId="query-editor-metric"
                options={METRIC_OPTIONS}
                value={config.metric?.value ?? 'cost'}
                onChange={onMetricChange}
                width={24}
              />
            </InlineField>
            <InlineField label="Time interval" labelWidth={16}>
              <Select
                inputId="query-editor-time-interval"
                options={TIME_INTERVAL_OPTIONS}
                value={config.timeInterval ?? 'day'}
                onChange={onTimeIntervalChange}
                width={24}
              />
            </InlineField>
          </InlineFieldRow>
          <InlineFieldRow>
            <InlineField label="Group by" labelWidth={16} grow tooltip="Dimensions to group results by">
              <MultiSelect
                inputId="query-editor-group-by"
                options={dimensionOptions}
                value={groupByValue}
                onChange={onGroupByChange}
                placeholder="Select dimensions"
              />
            </InlineField>
          </InlineFieldRow>
        </>
      )}
    </>
  );
}
