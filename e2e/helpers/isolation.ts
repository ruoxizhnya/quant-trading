import { Page, expect } from '@playwright/test';

export async function clearTestState(page: Page): Promise<void> {
  await page.evaluate(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
    } catch (e) {}
  });
}

export async function resetPiniaStores(page: Page): Promise<void> {
  await page.evaluate(() => {
    const piniaKey = '__pinia__';
    if (typeof window !== 'undefined' && piniaKey in window) {
      try {
        delete (window as any)[piniaKey];
      } catch (e) {}
    }
  });
}

export async function isolateTestEnvironment(page: Page): Promise<void> {
  await clearTestState(page);
  await resetPiniaStores(page);
}

export function withTestIsolation() {
  return async ({ page }: { page: Page }) => {
    await isolateTestEnvironment(page);
  };
}
