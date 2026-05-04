import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(__dirname, '..');
const distDir = path.join(frontendRoot, 'dist');
const targetDir = path.resolve(frontendRoot, '..', 'cmd', 'iworkercenter', 'web', 'admin');
const distAssetsDir = path.join(distDir, 'assets');
const targetAssetsDir = path.join(targetDir, 'assets');

if (!fs.existsSync(path.join(distDir, 'index.html'))) {
  throw new Error('dist/index.html not found. Run npm run build first.');
}
if (!fs.existsSync(distAssetsDir)) {
  throw new Error('dist/assets not found. Run npm run build first.');
}

fs.mkdirSync(targetDir, { recursive: true });
fs.cpSync(distDir, targetDir, { recursive: true });

const distAssetNames = new Set(fs.readdirSync(distAssetsDir));
if (fs.existsSync(targetAssetsDir)) {
  for (const name of fs.readdirSync(targetAssetsDir)) {
    if (!distAssetNames.has(name)) {
      fs.rmSync(path.join(targetAssetsDir, name), { force: true, recursive: true });
    }
  }
}

console.log('Embedded iWorkerCenter admin frontend -> ' + targetDir);
