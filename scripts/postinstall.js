#!/usr/bin/env node
const { spawnSync } = require('node:child_process');
const { existsSync, mkdirSync } = require('node:fs');
const { join } = require('node:path');

const root = join(__dirname, '..');
const exe = process.platform === 'win32' ? 'revisor.exe' : 'revisor';
const platformPackage = `@microck/revisor-${process.platform}-${process.arch}`;
const output = join(root, 'bin', exe);

try {
  const packageJson = require.resolve(`${platformPackage}/package.json`);
  const binary = join(packageJson, '..', 'bin', exe);
  if (existsSync(binary)) {
    process.exit(0);
  }
} catch {
  // Optional platform packages are not present when installing from source or a local tarball.
}

mkdirSync(join(root, 'bin'), { recursive: true });

const result = spawnSync('go', ['build', '-buildvcs=false', '-o', output, './cmd/revisor'], {
  cwd: root,
  stdio: 'inherit',
});

if (result.error) {
  console.error('revisor: Go is required to build this npm package.');
  console.error('Install Go, then run `npm rebuild revisor`.');
  process.exit(1);
}

process.exit(result.status ?? 1);
