<p align="center">
  <a href="https://github.com/Microck/revisor/releases"><img src="https://img.shields.io/github/v/release/Microck/revisor?display_name=tag&style=flat-square&label=release&color=000000" alt="release badge"></a>
  <a href="https://www.npmjs.com/package/@microck/revisor"><img src="https://img.shields.io/npm/dt/@microck/revisor?style=flat-square&label=downloads&color=000000" alt="npm downloads"></a>
  <a href="https://github.com/Microck/revisor/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/Microck/revisor/ci.yml?branch=main&style=flat-square&label=ci&color=000000" alt="ci badge"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-mit-000000?style=flat-square" alt="license badge"></a>
</p>

---

`revisor` is a codex-powered github review and repair cli. it clones pull requests and issues into temporary workspaces, runs the user's installed `codex` cli with bundled workflows, and then reports, patches, or opens a pull request depending on the command.

it is built for two audiences at once: humans who want a direct terminal command, and agents that need deterministic json output, non-interactive flags, and disposable workspaces.

## why

if you already trust `codex exec` for code work, `revisor` gives it a narrower command surface for common github loops:

- review a pr without touching your local checkout
- debug an issue and get a source-backed diagnosis
- fix an issue in a temp clone and open or update a pr
- keep codex skills bundled with the tool instead of depending on `~/.codex/skills`
- use `--json`, `--no-input`, and `--dry-run` in agent workflows
- clean up temp checkouts by default, with `--keep` when you want the workspace

## quickstart

```bash
npm install -g @microck/revisor

revisor review https://github.com/owner/repo/pull/123
revisor debug https://github.com/owner/repo/issues/456
revisor fix https://github.com/owner/repo/issues/456 --yes
```

`fix` mutates remote state by design: it creates or updates a branch and opens or updates a pull request. for humans, omit `--yes` to get a confirmation prompt. for agents, pass `--yes --no-input --json`.

## requirements

- `codex` installed and authenticated
- `git`
- `gh` authenticated for private repos, issue/pr metadata, forks, pushes, and pr creation
- node.js 18+ for the npm wrapper

prebuilt npm binary packages are used when available. if the matching platform package is not installed, `revisor` falls back to building the go binary locally, which requires go.

## installation

### npm

```bash
npm install -g @microck/revisor
```

the main package installs a small javascript launcher. published platform packages provide the go binary:

| package | platform |
| --- | --- |
| `@microck/revisor-darwin-arm64` | macos apple silicon |
| `@microck/revisor-darwin-x64` | macos intel |
| `@microck/revisor-linux-arm64` | linux arm64 |
| `@microck/revisor-linux-x64` | linux x64 |
| `@microck/revisor-win32-arm64` | windows arm64 |
| `@microck/revisor-win32-x64` | windows x64 |

if the optional platform package is missing, the launcher builds from source on first run. that fallback is useful for local tarballs and unusual platforms, but normal npm installs should use prebuilt packages.

### from source

```bash
git clone https://github.com/Microck/revisor
cd revisor
npm test
npm run build
node bin/revisor.js --help
```

build all npm platform binaries:

```bash
npm run build:prebuilts
```

## command surface

| command | purpose |
| --- | --- |
| `revisor review <pr-url>` | clone a pr, check out the pr branch, and run a read-only codex review |
| `revisor debug <url-or-text>` | diagnose an issue, pr, or text prompt without remote mutation |
| `revisor fix <issue-url>` | fix a github issue, push a branch, and open or update a pr |
| `revisor issue <issue-url>` | alias for `fix` |

## workflows

### review

```bash
revisor review https://github.com/owner/repo/pull/123
```

`review` clones the repository into a temp directory, checks out the pr branch, and runs codex with revisor's bundled review workflow. codex runs in a read-only sandbox and is instructed not to modify files.

useful flags:

| flag | description |
| --- | --- |
| `--json` | print a machine-readable run summary |
| `--keep` | keep the temp checkout and print its path |
| `--model <model>` | pass a model to `codex exec` |
| `--no-input` | disable codex approval prompts |

### debug

```bash
revisor debug https://github.com/owner/repo/issues/456
revisor debug "why does this cli hang after startup?"
```

`debug` is for diagnosis. with a github url, it clones the repository and includes issue or pr metadata in the codex prompt. with plain text, it runs in the current directory. it uses a read-only sandbox by default and does not push, commit, or open prs.

### fix

```bash
revisor fix https://github.com/owner/repo/issues/456 --yes
```

`fix` is the remote-mutating path:

1. clone the target repository into a temp directory
2. create or reuse a branch named `revisor/issue-<number>-<slug>`
3. run codex with revisor's bundled fix workflow
4. commit the resulting diff with a `Revisor-Run: <issue-url>` footer
5. push the branch
6. open or update a pull request
7. delete the temp checkout unless `--keep` is set

for repos owned by the authenticated user, `revisor` uses an upstream branch when a fork is unavailable and `gh` reports write access. for other repos, it prefers a fork branch.

use patch-only mode when you want the fix but no remote mutation:

```bash
revisor fix https://github.com/owner/repo/issues/456 --patch-only --patch ./issue-456.patch
```

## flags

| flag | default | description |
| --- | --- | --- |
| `--branch <name>` | generated | override the `revisor/issue-N-slug` branch name |
| `--codex <path>` | `codex` | codex executable to run |
| `--dry-run` | `false` | print planned commands and prompt text without mutating state |
| `--json` | `false` | print a structured run summary |
| `--keep` | `false` | keep the temp checkout |
| `--model <model>` | unset | pass a model to `codex exec` |
| `--no-input` | `false` | set codex approval policy to never |
| `--patch <path>` | generated | patch destination for `--patch-only` |
| `--patch-only` | `false` | write a patch instead of pushing/opening a pr |
| `--sandbox <mode>` | `workspace-write` | codex sandbox for writable commands |
| `--tmp-dir <path>` | os temp dir | parent directory for temp checkouts |
| `--upstream-branch` | `false` | push to upstream instead of a fork |
| `-y, --yes` | `false` | approve push/pr creation without prompting |
| `-h, --help` | | show help |
| `--version` | | print version |

for `review` and `debug`, revisor forces codex into `read-only` mode. for `fix`, `--sandbox` controls the writable codex run.

## agent usage

use `--json --no-input --yes` for non-interactive fix runs:

```bash
revisor fix https://github.com/owner/repo/issues/456 \
  --yes \
  --no-input \
  --json
```

example json shape:

```json
{
  "command": "fix",
  "target": {
    "owner": "owner",
    "repo": "repo",
    "kind": "issue",
    "number": 456,
    "url": "https://github.com/owner/repo/issues/456"
  },
  "tempPath": "/tmp/revisor-123/repo",
  "kept": false,
  "prUrl": "https://github.com/owner/repo/pull/789",
  "exitCode": 0
}
```

progress, clone output, and codex diagnostics go to stderr/stdout as produced by the underlying tools. the json summary is intended as the stable completion artifact.

## safety model

- `review` and `debug` are read-only codex runs
- `fix` requires a github issue url
- `fix` prompts before remote mutation unless `--yes` is provided
- `fix --patch-only` avoids push and pr creation
- temp checkouts are deleted by default
- `--keep` preserves the checkout when you need to inspect or continue manually
- revisor never depends on user-installed codex skills; prompts are bundled in the binary

## examples

review a pr:

```bash
revisor review https://github.com/Microck/tailstick/pull/47
```

diagnose an issue:

```bash
revisor debug https://github.com/Microck/tailstick/issues/46
```

open or update a pr from an issue:

```bash
revisor fix https://github.com/Microck/tailstick/issues/46 --yes
```

run a dry-run to inspect the planned codex prompt:

```bash
revisor fix https://github.com/Microck/tailstick/issues/46 --dry-run --yes
```

generate a patch only:

```bash
revisor fix https://github.com/Microck/tailstick/issues/46 \
  --patch-only \
  --patch ./tailstick-46.patch
```

keep the temp checkout:

```bash
revisor review https://github.com/Microck/tailstick/pull/47 --keep
```

## development

run tests:

```bash
npm test
```

build the local binary:

```bash
npm run build
```

build all prebuilt npm package binaries:

```bash
npm run build:prebuilts
```

package smoke test:

```bash
npm pack
tmp="$(mktemp -d)"
cd "$tmp"
npm init -y
npm install /path/to/microck-revisor-0.1.0.tgz
./node_modules/.bin/revisor --version
```
