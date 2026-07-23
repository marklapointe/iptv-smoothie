# Playwright E2E (planned)

Bootstrap UI uses stable `data-testid` hooks for Playwright:

- `login-username`, `login-password`, `login-submit`
- `wizard-banner`, `wizard-finish`, `wizard-skip-finish`
- `wizard-movies-path`, `wizard-tv-path`, `wizard-source-url`, `wizard-hdhr-url`
- `sources-list`, `channels-list`, `channel-search`, `playlist-url`

Install later:

```bash
cd web && npm init -y && npm i -D @playwright/test
npx playwright install chromium
```

Example smoke (once Angular or bootstrap is served):

```js
// web/e2e/login.spec.ts
import { test, expect } from '@playwright/test';
test('login and wizard banner', async ({ page }) => {
  await page.goto('http://127.0.0.1:8787/');
  await expect(page.getByTestId('login-card')).toBeVisible();
  await page.getByTestId('login-submit').click();
  // fresh DB shows wizard
  await expect(page.getByTestId('wizard-card').or(page.getByTestId('app-card'))).toBeVisible();
});
```
