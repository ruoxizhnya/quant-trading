import { test, expect } from '@playwright/test';
import { apiRequest, waitForBackendReady } from '../helpers/api';
import { isolateTestEnvironment } from '../helpers/isolation';

/**
 * 鉴权形态下的 SPA 端到端：登录页 / 首个管理员引导 / 登出。
 *
 * 为什么这个文件自己分流而不直接假定"开着鉴权"：
 *
 *   同一个 SPA 要同时服务两种部署形态，而 e2e 默认跑的是 open-access
 *   （本地 compose 没配 JWT_SECRET）。写成"断言一定出现登录页"会让默认
 *   形态全红，写成"断言一定不出现登录页"则在鉴权形态下什么也没测。所以
 *   先问一次 /api/auth/status，再断言**与该形态相符**的那一半。
 *
 *   ⚠️ 开着鉴权时**不能**跑整套 playwright：dashboard/cross-navigation 那些
 *   用例都假定"打开就是控制台"。本文件是唯一在两种形态下都成立的，鉴权形态
 *   下请只跑这一个文件：
 *
 *     BASE_URL=http://localhost:8080 npx playwright test tests/auth-login-flow.spec.ts
 *
 * 关于凭据：走完登录流程需要一对能用的账号，来源只有两处 ——
 *   ① 窗口还开着（users 表为空）⇒ 现场创建。这本身就是被测路径。
 *   ② 已经有人建过号 ⇒ 只能从 E2E_AUTH_USER / E2E_AUTH_PASSWORD 拿；
 *      没给就 skip，并说清楚缺什么。**不猜别人的密码，也不去清库。**
 */

let authEnabled = false;
let bootstrapRequired = false;

const BOOTSTRAP_USER = 'e2e-bootstrap-admin';
const BOOTSTRAP_PASS = 'e2e-strong-password';

test.beforeAll(async () => {
  const ready = await waitForBackendReady(60000);
  expect(ready, '后端 /health 必须在跑').toBe(true);

  const ctx = await apiRequest();
  const res = await ctx.get('/api/auth/status');
  expect(res.ok(), '/api/auth/status 必须公开可达（否则 SPA 连"该显示哪个表单"都问不出来）').toBe(true);
  const body = await res.json();
  authEnabled = body.auth_enabled === true;
  bootstrapRequired = body.bootstrap_required === true;
  await ctx.dispose();
});

test.beforeEach(async ({ page }) => {
  await isolateTestEnvironment(page);
});

/**
 * 关于输入框选择器：naive-ui 的 NInput 根节点是 `<div class="n-input">`，
 * 真正的 `<input>` 在里面一层。data-testid 落在根 div 上，所以 fill() 必须
 * 追到后代的 input —— 直接 fill 那个 div 会报 "element is not editable"。
 */
const USERNAME = '[data-testid="login-username"] input';
const PASSWORD = '[data-testid="login-password"] input';
const CONFIRM = '[data-testid="login-confirm"] input';

test('open-access 形态：/ 直接进控制台，全程不出现登录页', async ({ page }) => {
  test.skip(authEnabled, '该后端已开启鉴权，这条属于 open-access 形态');

  await page.goto('/');
  await page.waitForSelector('.dashboard-page', { timeout: 20000 });
  await expect(page.locator('.app-header')).toBeVisible();
  // 反证腿：确认我们不是在"登录页碰巧也有 .dashboard-page"的情况下通过的
  await expect(page.locator('[data-testid="login-title"]')).toHaveCount(0);
});

test('开启鉴权形态：未登录访问受保护页面被拦到登录页，并带上原目标', async ({ page }) => {
  test.skip(!authEnabled, '该后端是 open-access，没有登录页可拦');

  await page.goto('/screener');
  await expect(page).toHaveURL(/\/login/, { timeout: 20000 });
  await expect(page.locator('[data-testid="login-title"]')).toBeVisible();
  await expect(page.locator('[data-testid="login-submit"]')).toBeVisible();
  // 原目标要被带过去，登录后才能跳回来。vue-router 不把 query 里的 `/`
  // 百分号编码（实测得到 redirect=/screener），两种写法都接受。
  expect(page.url()).toMatch(/redirect=(%2F|\/)screener/);
});

test('引导 -> 登录 -> 控制台 -> 登出 -> 再被拦住', async ({ page }) => {
  test.skip(!authEnabled, '该后端是 open-access，本流程不存在');

  const envUser = process.env.E2E_AUTH_USER;
  const envPass = process.env.E2E_AUTH_PASSWORD;
  test.skip(
    !bootstrapRequired && (!envUser || !envPass),
    '首个管理员窗口已关闭，且未提供 E2E_AUTH_USER / E2E_AUTH_PASSWORD —— 没有凭据就走不了登录流程',
  );

  await page.goto('/screener');
  await expect(page).toHaveURL(/\/login/, { timeout: 20000 });

  if (bootstrapRequired) {
    // 空库：SPA 必须已经切到「创建首个管理员」表单（多一个确认密码框）
    await expect(page.locator('[data-testid="login-title"]')).toHaveText('创建首个管理员');
    await expect(page.locator(CONFIRM)).toBeVisible();
    await page.fill(USERNAME, BOOTSTRAP_USER);
    await page.fill(PASSWORD, BOOTSTRAP_PASS);
    await page.fill(CONFIRM, BOOTSTRAP_PASS);
  } else {
    await expect(page.locator(CONFIRM)).toHaveCount(0);
    await page.fill(USERNAME, envUser!);
    await page.fill(PASSWORD, envPass!);
  }

  await page.click('[data-testid="login-submit"]');

  // 登录成功应当回到最初请求的 /screener（redirect 参数生效）
  await expect(page).toHaveURL(/\/screener/, { timeout: 20000 });
  await expect(page.locator('.app-header')).toBeVisible();
  // 顶栏的身份区只在已登录时出现 —— 它是"确实拿到了可用 token"的可见证据
  await expect(page.locator('[data-testid="header-user"]')).toBeVisible();
  await expect(page.locator('[data-testid="header-user"]')).toContainText('管理员');

  // 登出：回登录页，且再访问受保护页面仍被拦住（不是仅前端藏了入口）
  await page.click('[data-testid="header-logout"]');
  await expect(page).toHaveURL(/\/login/, { timeout: 20000 });

  await page.goto('/screener');
  await expect(page).toHaveURL(/\/login/, { timeout: 20000 });
  await expect(page.locator('[data-testid="login-title"]')).toBeVisible();
});
