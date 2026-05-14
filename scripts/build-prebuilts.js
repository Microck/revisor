#!/usr/bin/env node
const { mkdirSync } = require('node:fs');
const { join } = require('node:path');
const { spawnSync } = require('node:child_process');

const root = join(__dirname, '..');
const targets = [
  ['darwin', 'arm64'],
  ['darwin', 'x64'],
  ['linux', 'arm64'],
  ['linux', 'x64'],
  ['win32', 'arm64'],
  ['win32', 'x64'],
];

const goos = new Map([
  ['darwin', 'darwin'],
  ['linux', 'linux'],
  ['win32', 'windows'],
]);

const goarch = new Map([
  ['arm64', 'arm64'],
  ['x64', 'amd64'],
]);

for (const [platform, arch] of targets) {
  const packageDir = join(root, 'npm', `revisor-${platform}-${arch}`);
  const binDir = join(packageDir, 'bin');
  const output = join(binDir, platform === 'win32' ? 'revisor.exe' : 'revisor');

  mkdirSync(binDir, { recursive: true });

  const result = spawnSync('go', ['build', '-buildvcs=false', '-trimpath', '-ldflags=-s -w', '-o', output, './cmd/revisor'], {
    cwd: root,
    env: {
      ...process.env,
      CGO_ENABLED: '0',
      GOOS: goos.get(platform),
      GOARCH: goarch.get(arch),
    },
    stdio: 'inherit',
  });

  if (result.error) {
    console.error(`failed to build ${platform}/${arch}: ${result.error.message}`);
    process.exit(1);
  }
  if ((result.status ?? 1) !== 0) {
    process.exit(result.status ?? 1);
  }
}
