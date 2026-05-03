import { test as base, Page } from '@playwright/test';
import { isolateTestEnvironment } from '../helpers/isolation';

type TestFixtures = {
  isolatedPage: Page;
};

export const test = base.extend<TestFixtures>({
  isolatedPage: async ({ page }, use) => {
    await isolateTestEnvironment(page);
    await use(page);
  },
});

export { expect };
