import { createHash } from 'crypto';
import { readFileSync, writeFileSync, rmSync, cpSync } from 'fs';

const SRC = 'site';
const DIST = 'site-dist';

function hashFile(path) {
    const content = readFileSync(path);
    return createHash('md5').update(content).digest('hex').slice(0, 8);
}

rmSync(DIST, { recursive: true, force: true });
cpSync(SRC, DIST, { recursive: true });

const jsHash = hashFile(`${DIST}/js/app.js`);
const cssHash = hashFile(`${DIST}/css/style.css`);

cpSync(`${DIST}/js/app.js`, `${DIST}/js/app.${jsHash}.js`);
cpSync(`${DIST}/css/style.css`, `${DIST}/css/style.${cssHash}.css`);

rmSync(`${DIST}/js/app.js`);
rmSync(`${DIST}/css/style.css`);

let html = readFileSync(`${DIST}/index.html`, 'utf8');
html = html.replace('js/app.js', `js/app.${jsHash}.js`);
html = html.replace('css/style.css', `css/style.${cssHash}.css`);
writeFileSync(`${DIST}/index.html`, html);

console.log(`js/app.${jsHash}.js`);
console.log(`css/style.${cssHash}.css`);