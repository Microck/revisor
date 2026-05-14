#!/usr/bin/env node
const { spawnSync } = require('node:child_process');
const { existsSync, mkdirSync } = require('node:fs');
const { join } = require('node:path');

const exe = process.platform === 'win32' ? 'revisor.exe' : 'revisor';
const legacyBinary = join(__dirname, process.platform === 'win32' ? 'revisor-go.exe' : 'revisor-go');
const localBinary = join(__dirname, exe);
const platformPackage = `@microck/revisor-${process.platform}-${process.arch}`;

function platformBinary() {
  try {
    const packageJson = require.resolve(`${platformPackage}/package.json`);
    return join(packageJson, '..', 'bin', exe);
  } catch {
    return undefined;
  }
}

let binary = platformBinary();
if (!binary || !existsSync(binary)) {
  binary = existsSync(localBinary) ? localBinary : legacyBinary;
}

if (!existsSync(binary)) {
  mkdirSync(__dirname, { recursive: true });
  binary = localBinary;
  const build = spawnSync('go', ['build', '-buildvcs=false', '-o', binary, './cmd/revisor'], {
    cwd: join(__dirname, '..'),
    stdio: 'inherit',
  });
  if (build.error) {
    console.error('revisor: Go is required to build this npm package.');
    console.error('Install Go, then run `npm rebuild revisor` or rerun `revisor`.');
    process.exit(1);
  }
  if ((build.status ?? 1) !== 0) {
    process.exit(build.status ?? 1);
  }
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' });
if (result.error) {
  console.error(`revisor: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status ?? 1);
