import { chromium } from '@playwright/test';
const base = process.env.GRAFANA_URL || "http://localhost:3000";
const out = process.env.OUT || "img";
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1920, height: 1320 }, deviceScaleFactor: 2, colorScheme: 'dark' });
const page = await ctx.newPage();
const login = await ctx.request.post(`${base}/login`, { data: { user: 'admin', password: 'admin' } });
if (!login.ok()) {
  throw new Error('login failed: ' + login.status());
}
const installTs = Date.now() - 50 * 86400000;
const ann = await ctx.request.post(`${base}/api/annotations`, { data: { dashboardUID: 'ci-showback-no-tagging', time: installTs, tags: ['attribute'], text: 'Attribute sensor installed' } });
console.log('annotation', ann.status());

async function settle() {
  await page.waitForLoadState('networkidle');
  // wait until no panel is still loading
  await page.waitForFunction(() => document.querySelectorAll('[aria-label="Panel loading bar"], [data-testid="Spinner"]').length === 0, null, { timeout: 60000 }).catch(() => {});
  await page.waitForTimeout(1500);
}
async function shot(path, name, opts = {}) {
  await page.goto(`${base}${path}`);
  await settle();
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: false });
  console.log('saved', name);
}
await shot('/d/ci-showback-no-tagging/showback-without-tagging?orgId=1&from=now-90d&to=now', '01-showback-dashboard', { fullPage: true });
await shot('/d/ci-cost-per-customer/cost-per-customer-and-margin?orgId=1&from=now-6M&to=now', '02-cost-per-customer-dashboard', { fullPage: true });
await shot('/d/ci-showback-no-tagging/showback-without-tagging?orgId=1&from=now-90d&to=now&editPanel=5', '03-panel-editor-saved-report');
await shot('/connections/datasources/edit/doit-cloud-intelligence', '04-datasource-config');
await browser.close();
