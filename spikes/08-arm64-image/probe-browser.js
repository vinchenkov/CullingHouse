// S8 probe: one Chromium launch + screenshot from inside the container.
// Runs as the non-root `agent` user. --no-sandbox is required because Docker's
// default seccomp profile blocks the unprivileged user namespaces Chromium's
// sandbox needs; the container itself is the sandbox in the MC design.
const fs = require('fs');
const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const version = browser.version();
  const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  await page.setContent(
    '<h1 style="font-family:sans-serif">mcspike-08 smoke</h1><p id="t">' +
      new Date().toISOString() + '</p>');
  const out = '/tmp/smoke.png';
  await page.screenshot({ path: out });
  const h1 = await page.evaluate(() => document.querySelector('h1').textContent);
  await browser.close();

  const buf = fs.readFileSync(out);
  const sig = buf.subarray(0, 8).toString('hex');
  if (sig !== '89504e470d0a1a0a') throw new Error('screenshot is not a PNG: ' + sig);
  if (h1 !== 'mcspike-08 smoke') throw new Error('unexpected page content: ' + h1);
  console.log(JSON.stringify({
    ok: true,
    chromiumVersion: version,
    pngBytes: buf.length,
    pngSignature: sig,
    arch: process.arch,
  }));
})().catch((e) => {
  console.error('SMOKE-FAIL', e);
  process.exit(1);
});
