import { createHash } from 'crypto';
import { readFileSync, writeFileSync, copyFileSync, mkdirSync } from 'fs';

const SRC = 'site';
const DIST = 'site-dist';

function hashFile(path) {
    const content = readFileSync(path);
    return createHash('md5').update(content).digest('hex').slice(0, 8);
}

mkdirSync(DIST, { recursive: true });
mkdirSync(`${DIST}/js`, { recursive: true });
mkdirSync(`${DIST}/css`, { recursive: true });

const jsHash = hashFile(`${SRC}/js/app.js`);
const cssHash = hashFile(`${SRC}/css/style.css`);

copyFileSync(`${SRC}/js/app.js`, `${DIST}/js/app.${jsHash}.js`);
copyFileSync(`${SRC}/css/style.css`, `${DIST}/css/style.${cssHash}.css`);

let html = readFileSync(`${SRC}/index.html`, 'utf8');
html = html.replace('js/app.js', `js/app.${jsHash}.js`);
html = html.replace('css/style.css', `css/style.${cssHash}.css`);
writeFileSync(`${DIST}/index.html`, html);

console.log(`js/app.${jsHash}.js`);
console.log(`css/style.${cssHash}.css`);