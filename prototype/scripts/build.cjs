/* Build the deployable directory and an optional single-file offline demo. */
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
const out = path.join(root, 'dist');
fs.mkdirSync(out, { recursive: true });
const html = fs.readFileSync(path.join(root, 'index.html'), 'utf8');
fs.writeFileSync(path.join(out, 'index.html'), html);
fs.cpSync(path.join(root, 'assets'), path.join(out, 'assets'), { recursive: true });
fs.rmSync(path.join(out, 'app.js'), { force: true });
let standalone = html.replace(/<link rel="stylesheet" href="([^"]+)">/, (_, file) =>
  '<style>' + fs.readFileSync(path.join(root, file), 'utf8') + '</style>');
standalone = standalone.replace(/<link rel="icon"[^>]+>/, '');
standalone = standalone.replace(/<script src="([^"]+)"><\/script>/g, (_, file) =>
  '<script>' + fs.readFileSync(path.join(root, file), 'utf8').replace(/<\/script/gi, '<\\/script') + '</script>');
fs.writeFileSync(path.join(root, 'TaskForce_Standalone.html'), standalone);
fs.writeFileSync(path.join(out, 'TaskForce_Standalone.html'), standalone);
console.log('Built dist/ and TaskForce_Standalone.html');
