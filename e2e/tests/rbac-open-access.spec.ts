import { test, expect } from '@playwright/test';
import { apiRequest } from '../helpers/api';

/**
 * AUD-02 (ODR-065 H5): RBAC on order-mutating endpoints.
 *
 * The e2e environment runs with auth DISABLED (no JWT_SECRET), so the
 * assertions here are the *open-access* half of the contract: when auth
 * is off, execution and tools must behave exactly as before — no
 * accidental 401/403 from the newly added role middleware.
 *
 * The enforcement half (viewer -> 403, trader -> 201) is covered by
 * Go tests in cmd/analysis/handlers_rbac_test.go, which can construct
 * a real auth.Service. Playwright cannot: it has no way to mint a
 * signed access token without the server's JWT secret.
 *
 * Why this file still matters: the most likely way to break AUD-02 in
 * production is to wire RequireRole unconditionally so that the dev /
 * CI stack starts rejecting every order. That failure would show up
 * here as a 401 where a 201/400 is expected.
 */
test.describe('AUD-02: RBAC does not break open-access mode', () => {
  let ctx: any;

  test.beforeAll(async () => {
    ctx = await apiRequest();
  });

  test.afterAll(async () => {
    await ctx.dispose();
  });

  test('POST /api/execution/orders is not rejected by role middleware', async () => {
    const res = await ctx.post('/api/execution/orders', {
      data: { symbol: '000001.SZ', side: 'long', type: 'market', quantity: 100, price: 10 },
    });
    // 201 = accepted. 400 = domain-level rejection (e.g. market closed /
    // insufficient cash) — also fine: RBAC let it through to the handler.
    // What must NOT happen is 401 or 403, which would mean the role
    // middleware is enforcing in open-access mode.
    expect([201, 400]).toContain(res.status());
    expect(res.status()).not.toBe(401);
    expect(res.status()).not.toBe(403);
  });

  test('legacy POST /orders behaves identically to /api/execution/orders', async () => {
    const res = await ctx.post('/orders', {
      data: { symbol: '000001.SZ', side: 'long', type: 'market', quantity: 100, price: 10 },
    });
    expect([201, 400]).toContain(res.status());
    expect(res.status()).not.toBe(401);
    expect(res.status()).not.toBe(403);
  });

  test('GET /api/execution/account is readable without a token', async () => {
    const res = await ctx.get('/api/execution/account');
    expect(res.status()).toBe(200);
  });

  test('POST /api/tools/<write-tool> is not rejected by role middleware', async () => {
    // save_factor is classified SideEffectWrite. With auth disabled the
    // handler must not enforce. We send a deliberately invalid body so
    // the request stops at validation (400) rather than actually
    // mutating the gene pool.
    const res = await ctx.post('/api/tools/save_factor', { data: { args: {} } });
    expect([200, 400, 404, 500]).toContain(res.status());
    expect(res.status()).not.toBe(401);
    expect(res.status()).not.toBe(403);
  });

  test('GET /api/tools lists tools without a token', async () => {
    const res = await ctx.get('/api/tools');
    expect(res.status()).toBe(200);
  });
});
