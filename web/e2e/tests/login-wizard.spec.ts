import { test, expect } from '@playwright/test';

// Fresh data dir is created by webServer env; login with default admin:admin.

test.describe('Smoothie bootstrap UI', () => {
  test('health endpoint is up', async ({ request }) => {
    const res = await request.get('/api/health');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.status).toBe('ok');
  });

  test('login with admin:admin and complete wizard', async ({ page, request }) => {
    await page.goto('/');
    await expect(page.getByTestId('login-card')).toBeVisible();
    await page.getByTestId('login-username').fill('admin');
    await page.getByTestId('login-password').fill('admin');
    await page.getByTestId('login-submit').click();

    // Fresh DB: wizard required
    await expect(page.getByTestId('wizard-card')).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId('wizard-banner')).toBeVisible();

    await page.getByTestId('wizard-movies-path').fill('/tmp/smoothie-e2e-movies');
    await page.getByTestId('wizard-tv-path').fill('/tmp/smoothie-e2e-tv');
    await page.getByTestId('wizard-skip-finish').click();

    await expect(page.getByTestId('app-card')).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId('playlist-url')).toContainText('playlist.m3u');

    const login = await request.post('/api/auth/login', {
      data: { username: 'admin', password: 'admin' },
    });
    expect(login.ok()).toBeTruthy();
    const { token } = await login.json();
    const st = await request.get('/api/setup/status', {
      headers: { Authorization: `Bearer ${token}` },
    });
    const body = await st.json();
    expect(body.wizard_required).toBe(false);
    expect(body.setup_complete).toBe(true);
  });
});
