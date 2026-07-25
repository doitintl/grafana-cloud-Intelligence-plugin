import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const queryEditor = panelEditPage.getQueryEditorRow('A');
  await expect(queryEditor.getByRole('combobox', { name: 'Report' })).toBeVisible();
  await expect(queryEditor.getByText('Use dashboard time')).toBeVisible();
});
