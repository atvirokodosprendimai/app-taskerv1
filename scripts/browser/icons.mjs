// Renders the web app manifest's PNG icons from their SVG sources, so no PNG is
// ever edited by hand and all of them can be made again after an SVG changes:
//
//   node icons.mjs
//
// The "any" icons are the favicon as it is, rounded corners and all. The
// maskable one is static/icon.svg, whose mark sits inside the safe zone a
// launcher may crop to. Each is drawn at exactly its size, on a transparent
// background, by the same Chromium the browser walk uses.

import { chromium } from 'playwright';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const web = fileURLToPath(new URL('../../internal/web/', import.meta.url));
const icons = [
  { src: 'assets/favicon.svg', out: 'static/icon-192.png', size: 192 },
  { src: 'assets/favicon.svg', out: 'static/icon-512.png', size: 512 },
  { src: 'static/icon.svg', out: 'static/icon-512-maskable.png', size: 512 },
];

const browser = await chromium.launch();
try {
  for (const { src, out, size } of icons) {
    const page = await browser.newPage({ viewport: { width: size, height: size }, deviceScaleFactor: 1 });
    const svg = readFileSync(web + src, 'base64');
    await page.setContent(
      `<style>html,body{margin:0;background:transparent}img{display:block;width:${size}px;height:${size}px}</style>` +
        `<img alt="" src="data:image/svg+xml;base64,${svg}">`,
    );
    // An <img> decodes asynchronously; a screenshot taken before it has would
    // save an empty square.
    await page.locator('img').evaluate(img => img.decode());
    await page.screenshot({ path: web + out, omitBackground: true });
    await page.close();
    console.log(`${out} ${size}x${size} from ${src}`);
  }
} finally {
  await browser.close();
}
