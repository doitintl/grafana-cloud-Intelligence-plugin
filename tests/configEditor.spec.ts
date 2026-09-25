import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render config editor', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByRole('textbox', { name: 'API URL' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'API Key' })).toBeVisible();
});

test('"Save & test" should fail when the API key is missing', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('textbox', { name: 'API URL' }).fill('https://api.doit.com');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'API key is missing' });
});

test('"Save & test" should not expose connection details when the API is unreachable', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  // Port 9 (discard) is closed in the Grafana container, so the backend gets a connection error.
  await page.getByRole('textbox', { name: 'API URL' }).fill('http://127.0.0.1:9');
  await page.getByRole('textbox', { name: 'API Key' }).fill('not-a-real-key');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'Could not reach the DoiT API' });
  await expect(page.getByText(/dial tcp|connection refused|127\.0\.0\.1:9/)).toHaveCount(0);
});
