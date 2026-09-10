// Browser walk for tasker.
//
// Drives the BUILT binary in a real Chromium the way a person would — register,
// run several timers at once, watch them tick, use a second tab, restart the
// server, filter the history, sign out and back in, then again on a phone-sized
// screen — and fails on anything a person would notice.
//
// It starts its own server on a scratch database, so nothing needs to be running:
//
//   TASKER_BIN=/path/to/tasker node walk.mjs
//
// Screenshots and the server's log go to ./shots. The last line printed is
// BROWSER_FAILURES=<n>, and the exit code is 0 only when n is 0.
//
// ⚠ Nothing here waits for 'networkidle'. The dashboard holds a stream open for
// as long as it is showing, so the network is never idle and such a wait only
// ever times out. Every wait is for a selector, a URL or a value.

import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import { createWriteStream, mkdirSync, mkdtempSync, readFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';

const bin = process.env.TASKER_BIN;
if (!bin) {
  console.error('Set TASKER_BIN to the built tasker binary.');
  process.exit(2);
}

const shots = fileURLToPath(new URL('./shots/', import.meta.url));
mkdirSync(shots, { recursive: true });
const logPath = join(shots, 'server.log');
const serverLog = createWriteStream(logPath);
const dbPath = join(mkdtempSync(join(tmpdir(), 'tasker-walk-')), 'tasker.db');
const port = await freePort();
const base = `http://127.0.0.1:${port}`;
const password = 'correct horse battery';

const failures = [];
function check(ok, what) {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${what}`);
  if (!ok) failures.push(what);
}

// ---- the server ---------------------------------------------------------------

let server;

function freePort() {
  return new Promise((resolve, reject) => {
    const s = createServer();
    s.once('error', reject);
    s.listen(0, '127.0.0.1', () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });
}

async function startServer() {
  server = spawn(bin, ['--addr', `127.0.0.1:${port}`, '--db', dbPath], { stdio: ['ignore', 'pipe', 'pipe'] });
  server.stdout.pipe(serverLog, { end: false });
  server.stderr.pipe(serverLog, { end: false });
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`${base}/healthz`)).ok) return;
    } catch {
      // Not listening yet.
    }
    await sleep(100);
  }
  throw new Error('the server did not answer /healthz within 10 seconds');
}

// stopServer sends SIGTERM and reports how long the process took to exit.
function stopServer() {
  const started = Date.now();
  return new Promise(resolve => {
    server.once('exit', code => resolve({ ms: Date.now() - started, code }));
    server.kill('SIGTERM');
  });
}

// ---- page helpers -------------------------------------------------------------

// While the server restarts, the browser reports the refused connections its
// stream's retries run into. Those are the retry working, not a defect.
let restarting = false;
const restartNoise = /ERR_CONNECTION_REFUSED|ERR_EMPTY_RESPONSE|ERR_INCOMPLETE_CHUNKED_ENCODING|Failed to load resource|Failed to fetch/;

let current;

function watch(page, label) {
  page.on('console', msg => {
    if (msg.type() !== 'error') return;
    if (restarting && restartNoise.test(msg.text())) return;
    check(false, `${label}: console error: ${msg.text()}`);
  });
  page.on('pageerror', err => check(false, `${label}: uncaught error: ${err.message}`));
}

// record keeps what a page sends, so a check can assert the REQUEST rather than
// only its effect: a leaked signal or a missing field lives in the request, and
// the server-side tests cannot see it because they write the request themselves.
function record(page) {
  const sent = [];
  page.on('request', req => {
    const url = new URL(req.url());
    if (url.origin !== base || url.pathname.startsWith('/assets/')) return;
    sent.push({ method: req.method(), path: url.pathname, query: url.searchParams, body: req.postData() });
  });
  return {
    last: (method, path) => [...sent].reverse().find(r => r.method === method && path.test(r.path)),
    async wait(method, path) {
      for (let i = 0; i < 50; i++) {
        const r = this.last(method, path);
        if (r) return r;
        await sleep(100);
      }
      throw new Error(`the page never sent ${method} ${path}`);
    },
  };
}

function signalsOf(req) {
  if (!req) return null;
  const raw = req.method === 'GET' ? req.query.get('datastar') : req.body;
  return JSON.parse(raw ?? 'null');
}

// shot takes a full-page screenshot with transitions and animations stopped, so
// the picture is the state the page settled into rather than a frame halfway
// through a colour or opacity change — which reads as a styling defect that is
// not there.
async function shot(page, name, colorScheme = 'light') {
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme });
  await page.screenshot({ path: join(shots, name), fullPage: true });
  await page.emulateMedia({ reducedMotion: 'no-preference', colorScheme: 'light' });
}

const noSideScroll = page => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
const waitForTimers = (page, n) =>
  page.waitForFunction(want => document.querySelectorAll('#running-timers li.timer').length === want, n, { timeout: 5000 });
// A loading indicator that is never switched off leaves its button disabled for
// good, and every check that only counts timers would pass straight over it.
const waitForStartButtonsUsable = page =>
  page.waitForFunction(() => [...document.querySelectorAll('#company-list button.btn-start')].every(b => !b.disabled), null, {
    timeout: 3000,
  });
const card = (page, name) =>
  page.locator('article.company').filter({ has: page.locator('h3', { hasText: new RegExp(`^${name}$`) }) });
const elapsedOf = page => page.locator('#running-timers li.timer .timer-elapsed').first().textContent();
const results = page => page.locator('#history-results').textContent();
const modeButton = (page, label) => page.locator('.segmented button', { hasText: new RegExp(`^${label}$`) });

// ---- desktop -----------------------------------------------------------------

async function desktop(browser) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, timezoneId: 'Europe/Vilnius' });
  const page = await ctx.newPage();
  current = page;
  watch(page, 'desktop');
  const sent = record(page);

  // Registration.
  await page.goto(`${base}/register`, { waitUntil: 'load' });
  await page.fill('#email', 'ada@example.com');
  await page.fill('#password', password);
  await page.click('button:has-text("Create account")');
  await page.waitForURL(`${base}/`);
  await page.waitForSelector('#running-timers');
  const reg = signalsOf(sent.last('POST', /^\/register$/));
  check(reg?.registerTimezone === 'Europe/Vilnius', `registration sends the browser's time zone (${reg?.registerTimezone})`);
  const stream = await sent.wait('GET', /^\/stream$/);
  check(stream.query.get('datastar') === '{}', `the stream request carries no signals (${stream.query.get('datastar')})`);

  const navigations = [];
  page.on('framenavigated', f => {
    if (f === page.mainFrame()) navigations.push(f.url());
  });

  // Companies: one added with Enter, one with the button, one refused.
  await page.fill('#new-company', 'Acme');
  await page.press('#new-company', 'Enter');
  await card(page, 'Acme').waitFor();
  check(JSON.stringify(signalsOf(sent.last('POST', /^\/companies$/))) === '{"newCompany":"Acme"}', 'adding a company sends only its name');
  check((await page.inputValue('#new-company')) === '', 'the new-company box empties once the company is saved');
  await page.fill('#new-company', 'Beta');
  await page.click('button:has-text("Add company")');
  await card(page, 'Beta').waitFor();
  await page.fill('#new-company', 'ACME');
  await page.press('#new-company', 'Enter');
  await page.waitForSelector('#flash .flash-error');
  check((await page.textContent('#flash')).includes('already have a company'), 'a duplicate company name is explained on the page');
  await page.fill('#new-company', '');

  // Several timers at once.
  await card(page, 'Acme').locator('input').fill('Invoices');
  await card(page, 'Acme').locator('button.btn-start').click();
  await waitForTimers(page, 1);
  const first = signalsOf(sent.last('POST', /^\/companies\/\d+\/timers$/));
  check(Object.keys(first).length === 1 && Object.values(first)[0] === 'Invoices', `Start sends only that company's box (${JSON.stringify(first)})`);
  check((await card(page, 'Acme').locator('input').inputValue()) === '', "the company's box empties once its timer starts");
  check((await page.textContent('#flash')).trim() === '', 'the earlier error message is gone after a successful action');
  await waitForStartButtonsUsable(page);
  check(true, 'the pressed Start button is usable again once its timer has started');

  await card(page, 'Acme').locator('input').fill('half-typed');
  await card(page, 'Beta').locator('input').fill('Code review');
  await card(page, 'Beta').locator('input').press('Enter');
  await waitForTimers(page, 2);
  const second = signalsOf(sent.last('POST', /^\/companies\/\d+\/timers$/));
  check(Object.keys(second).length === 1 && Object.values(second)[0] === 'Code review', `a second Start sends only its own box (${JSON.stringify(second)})`);
  check((await card(page, 'Acme').locator('input').inputValue()) === 'half-typed', 'a half-typed task name survives the page updating around it');
  await card(page, 'Acme').locator('input').fill('');
  check((await card(page, 'Acme').locator('.badge').textContent()).trim() === '1 running', 'a company card shows how many of its timers run');

  // Live: the timers tick, and a second tab stays in step.
  const t1 = await elapsedOf(page);
  await sleep(2300);
  const t2 = await elapsedOf(page);
  check(t1 !== t2, `running timers tick on their own (${t1} → ${t2})`);

  const tab = await ctx.newPage();
  watch(tab, 'desktop second tab');
  const tabSent = record(tab);
  await tab.goto(`${base}/`, { waitUntil: 'load' });
  await waitForTimers(tab, 2);
  await tab.locator('#running-timers li.timer', { hasText: 'Code review' }).locator('button.btn-stop').click();
  await waitForTimers(tab, 1);
  check(JSON.stringify(signalsOf(tabSent.last('POST', /^\/timers\/\d+\/stop$/))) === '{}', 'Stop sends no signals');
  await waitForTimers(page, 1);
  check(true, 'stopping a timer in one tab updates the other without a reload');
  check((await card(page, 'Beta').locator('.badge').count()) === 0, "a stopped timer's company no longer shows as running");
  await shot(page, 'desktop-dashboard.png');
  await shot(page, 'desktop-dashboard-dark.png', 'dark');

  // A restart with two dashboards open: shutdown must not wait on their streams,
  // and each page must pick its stream up again by itself.
  restarting = true;
  const stopped = await stopServer();
  check(stopped.code === 0, `the server exits cleanly on SIGTERM (exit code ${stopped.code})`);
  check(stopped.ms < 3000, `shutdown with two dashboards open took ${stopped.ms} ms, not the 10 s timeout`);
  await startServer();
  const frozen = await elapsedOf(page);
  await page.waitForFunction(
    prev => document.querySelector('#running-timers li.timer .timer-elapsed')?.textContent !== prev,
    frozen,
    { timeout: 20000 },
  );
  restarting = false;
  check(true, 'the dashboard picked its stream up again after the restart');
  check(navigations.length === 0, `the dashboard never reloaded (${navigations.length} navigations)`);
  await tab.close();

  // History.
  await page.click('nav a:has-text("History")');
  await page.waitForURL(`${base}/history`);
  await page.waitForSelector('#history-results');
  let text = await results(page);
  check(text.includes('Acme') && text.includes('Beta'), 'this month lists both companies');

  await modeButton(page, 'Day').click();
  await page.waitForFunction(() => location.search.includes('mode=day'));
  check((await modeButton(page, 'Day').getAttribute('aria-pressed')) === 'true', 'the Day button reads as pressed');
  check((await page.isVisible('#hist-day')) && !(await page.isVisible('#hist-month')), 'day mode shows the day picker and hides the month picker');

  await page.selectOption('#hist-company', { label: 'Acme' });
  await page.waitForFunction(() => location.search.includes('company='));
  text = await results(page);
  check(text.includes('Acme') && !text.includes('Beta'), 'narrowing to one company hides the others');
  const hist = signalsOf(sent.last('GET', /^\/history\/results$/));
  check(hist?.histMode === 'day' && hist?.histCompany !== '' && !('newCompany' in hist), `the history request sends its filter (${JSON.stringify(hist)})`);

  const today = await page.inputValue('#hist-day');
  await page.click('button[aria-label="Previous period"]');
  await page.waitForFunction(was => document.querySelector('#hist-day').value !== was, today);
  check((await results(page)).includes('No time recorded'), 'the previous day shows the empty state');
  await shot(page, 'desktop-history-empty.png');

  await page.fill('#hist-day', today);
  await page.waitForFunction(() => document.querySelector('#history-results')?.textContent.includes('Invoices'));
  check(true, 'typing a date into the day picker loads that day');

  await page.click('button:has-text("This year")');
  await page.waitForFunction(() => location.search.includes('mode=year'));
  check((await results(page)).includes('Invoices'), 'the This year preset shows the entries');
  check((await modeButton(page, 'Year').getAttribute('aria-pressed')) === 'true', 'the preset presses the Year button');
  await shot(page, 'desktop-history.png');

  await page.reload({ waitUntil: 'load' });
  check((await modeButton(page, 'Year').getAttribute('aria-pressed')) === 'true', 'a reload keeps the period');
  check((await page.inputValue('#hist-company')) !== '', 'a reload keeps the company');

  // Sign out, and back in.
  await page.click('button:has-text("Sign out")');
  await page.waitForURL(`${base}/login`);
  await page.fill('#email', 'ada@example.com');
  await page.fill('#password', 'not the password');
  await page.click('button:has-text("Sign in")');
  await page.waitForSelector('#auth-message .flash-error');
  check((await page.textContent('#auth-message')).includes('do not match'), 'a wrong password is explained on the page');
  await shot(page, 'desktop-login-error.png');
  await page.fill('#password', password);
  await page.click('button:has-text("Sign in")');
  await page.waitForURL(`${base}/`);
  await waitForTimers(page, 1);
  check(true, 'signing back in shows the timer still running');
  await ctx.close();
}

// ---- a phone -------------------------------------------------------------------

async function phone(browser) {
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    timezoneId: 'Europe/Vilnius',
  });
  const page = await ctx.newPage();
  current = page;
  watch(page, 'phone');

  await page.goto(`${base}/register`, { waitUntil: 'load' });
  check(await noSideScroll(page), 'registration fits a 390 px screen');
  await shot(page, 'phone-register.png');
  await page.fill('#email', 'bob@example.com');
  await page.fill('#password', password);
  await page.tap('button:has-text("Create account")');
  await page.waitForURL(`${base}/`);
  await page.waitForSelector('#company-list');
  check(!(await page.locator('#running-timers li.timer').count()), "a new account does not see another user's timers");

  const name = 'Northwind Logistics International';
  await page.fill('#new-company', name);
  await page.tap('button:has-text("Add company")');
  await card(page, name).waitFor();
  await card(page, name).locator('input').fill('Quarterly reconciliation of every regional warehouse ledger');
  await card(page, name).locator('button.btn-start').tap();
  await waitForTimers(page, 1);
  await waitForStartButtonsUsable(page);
  check(true, 'the tapped Start button is usable again once its timer has started');
  check(await noSideScroll(page), 'the dashboard fits a 390 px screen with long names and a running timer');
  const stop = await page.locator('#running-timers .btn-stop').first().boundingBox();
  check(stop.width >= 44 && stop.height >= 44, `the Stop button is a comfortable tap target (${Math.round(stop.width)}×${Math.round(stop.height)})`);
  await shot(page, 'phone-dashboard.png');

  await page.tap('nav a:has-text("History")');
  await page.waitForURL(`${base}/history`);
  await page.waitForSelector('#history-results');
  check(await noSideScroll(page), 'the history fits a 390 px screen');
  await shot(page, 'phone-history.png');
  await ctx.close();
}

// ---- run -----------------------------------------------------------------------

const browser = await chromium.launch();
try {
  await startServer();
  await desktop(browser);
  await phone(browser);
} catch (err) {
  check(false, `the walk stopped: ${err.message.split('\n')[0]}`);
  await current?.screenshot({ path: join(shots, 'stopped.png'), fullPage: true }).catch(() => {});
} finally {
  await browser.close();
  if (server && server.exitCode === null && server.signalCode === null) await stopServer();
  await new Promise(resolve => serverLog.end(resolve));
}

const log = readFileSync(logPath, 'utf8');
check(!/superfluous response\.WriteHeader/.test(log), 'the server log has no superfluous WriteHeader warnings');
check(!/level=ERROR/.test(log), 'the server log has no errors');

console.log(`\nBROWSER_FAILURES=${failures.length}`);
for (const f of failures) console.log(` - ${f}`);
process.exit(failures.length === 0 ? 0 : 1);
