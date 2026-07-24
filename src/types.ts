import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type DoitQueryType = 'report' | 'query';

export interface AdHocFilter {
  id: string;
  type: string;
  values: string[];
  inverse?: boolean;
}

export interface AdHocGroup {
  id: string;
  type: string;
}

export interface AdHocConfig {
  metric?: { type: string; value: string };
  timeRange?: { mode: string; amount: number; unit: string; includeCurrent: boolean };
  customTimeRange?: { from: string; to: string };
  timeInterval?: string;
  aggregation?: string;
  group?: AdHocGroup[];
  filters?: AdHocFilter[];
  dimensions?: Array<{ id: string; type: string }>;
}

export interface DoitQuery extends DataQuery {
  doitQueryType?: DoitQueryType;
  reportId?: string;
  reportName?: string;
  useGrafanaTimeRange?: boolean;
  config?: AdHocConfig;
}

export const DEFAULT_QUERY: Partial<DoitQuery> = {
  doitQueryType: 'report',
  useGrafanaTimeRange: true,
};

export interface ReportListItem {
  id: string;
  reportName: string;
  owner: string;
  type: string;
}

export interface Dimension {
  id: string;
  label: string;
  type: string;
}

export interface DimensionValue {
  value: string;
  cloud: string;
}

export interface DimensionWithValues extends Dimension {
  values: DimensionValue[];
}

export interface DoitDataSourceOptions extends DataSourceJsonData {
  apiUrl?: string;
}

export interface DoitSecureJsonData {
  apiKey?: string;
}
