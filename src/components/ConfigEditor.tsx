import React, { ChangeEvent } from 'react';
import { InlineField, Input, SecretInput } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { DoitDataSourceOptions, DoitSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<DoitDataSourceOptions, DoitSecureJsonData> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const onApiUrlChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        apiUrl: event.target.value,
      },
    });
  };

  const onAPIKeyChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        apiKey: event.target.value,
      },
    });
  };

  const onResetAPIKey = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...options.secureJsonFields,
        apiKey: false,
      },
      secureJsonData: {
        ...options.secureJsonData,
        apiKey: '',
      },
    });
  };

  return (
    <>
      <InlineField
        label="API URL"
        labelWidth={16}
        interactive
        tooltip={'DoiT API base URL. Leave the default unless instructed otherwise.'}
      >
        <Input
          id="config-editor-api-url"
          onChange={onApiUrlChange}
          value={jsonData.apiUrl}
          placeholder="https://api.doit.com"
          width={40}
        />
      </InlineField>
      <InlineField
        label="API Key"
        labelWidth={16}
        interactive
        tooltip={'DoiT API key. Generate one in the DoiT console under Profile > API.'}
      >
        <SecretInput
          required
          id="config-editor-api-key"
          isConfigured={secureJsonFields.apiKey}
          value={secureJsonData?.apiKey}
          placeholder="Enter your DoiT API key"
          width={40}
          onReset={onResetAPIKey}
          onChange={onAPIKeyChange}
        />
      </InlineField>
    </>
  );
}
