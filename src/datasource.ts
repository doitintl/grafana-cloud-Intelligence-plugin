import { CoreApp, DataSourceInstanceSettings, MetricFindValue, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { DEFAULT_QUERY, Dimension, DimensionWithValues, DoitDataSourceOptions, DoitQuery, ReportListItem } from './types';

const REPORTS_VARIABLE_QUERY = /^reports\(\)$/;
const DIMENSION_VALUES_VARIABLE_QUERY = /^dimension_values\(\s*([\w-]+)\s*,\s*([\w-]+)\s*\)$/;

export class DataSource extends DataSourceWithBackend<DoitQuery, DoitDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<DoitDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<DoitQuery> {
    return DEFAULT_QUERY;
  }

  applyTemplateVariables(query: DoitQuery, scopedVars: ScopedVars): DoitQuery {
    const templateSrv = getTemplateSrv();

    return {
      ...query,
      reportId: query.reportId ? templateSrv.replace(query.reportId, scopedVars) : query.reportId,
      config: query.config
        ? {
            ...query.config,
            filters: query.config.filters?.map((filter) => ({
              ...filter,
              values: filter.values.flatMap((value) =>
                templateSrv
                  .replace(value, scopedVars, 'pipe')
                  .split('|')
                  .filter((v) => v !== '')
              ),
            })),
          }
        : query.config,
    };
  }

  filterQuery(query: DoitQuery): boolean {
    if (query.doitQueryType === 'query') {
      return !!query.config?.metric;
    }

    return !!query.reportId;
  }

  async metricFindQuery(query: string): Promise<MetricFindValue[]> {
    const trimmed = query.trim();

    if (REPORTS_VARIABLE_QUERY.test(trimmed)) {
      const reports = await this.listReports();
      return reports.map((report) => ({ text: report.reportName, value: report.id }));
    }

    const dimensionValuesMatch = trimmed.match(DIMENSION_VALUES_VARIABLE_QUERY);
    if (dimensionValuesMatch) {
      const [, type, id] = dimensionValuesMatch;
      const dimension = await this.getDimensionValues(type, id);
      return dimension.values.map((value) => ({ text: value.value }));
    }

    throw new Error('Unsupported variable query. Use reports() or dimension_values(<type>, <id>).');
  }

  listReports(): Promise<ReportListItem[]> {
    return this.getResource('reports');
  }

  listDimensions(): Promise<Dimension[]> {
    return this.getResource('dimensions');
  }

  getDimensionValues(type: string, id: string): Promise<DimensionWithValues> {
    return this.getResource('dimension-values', { type, id });
  }
}
