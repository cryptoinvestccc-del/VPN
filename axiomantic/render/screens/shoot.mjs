// Screenshots the case screens made by make.py into PNG textures.
//   node axiomantic/render/screens/shoot.mjs   (needs the playwright package)
import { chromium } from 'playwright';
import { readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = join(dirname(fileURLToPath(import.meta.url)), '..', '.cache', 'screens');
const browser = await chromium.launch();
for (const name of readdirSync(dir).filter((n) => n.endsWith('.html'))) {
  const phone = name.includes('-phone');
  const page = await browser.newPage({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 1.5,
  });
  await page.goto('file://' + join(dir, name));
  await page.waitForTimeout(150);
  await page.screenshot({ path: join(dir, name.replace('.html', '.png')) });
  await page.close();
  console.log('screen', name);
}
await browser.close();
