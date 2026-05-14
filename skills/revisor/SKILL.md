---
name: revisor
description: Runs revisor for codex-powered github pull request review, issue debugging, and issue fixing. Use when the user asks to review a github pr, diagnose a github issue or bug report, fix an issue with a pr, or use revisor from an agent workflow.
---

# revisor

## quick start

use `revisor` when the task is centered on a github pr or issue and the user wants a disposable codex-powered review, diagnosis, or fix.

```bash
revisor review https://github.com/owner/repo/pull/123
revisor debug https://github.com/owner/repo/issues/456
revisor fix https://github.com/owner/repo/issues/456 --yes
```

## choose the command

| command | use when | behavior |
| --- | --- | --- |
| `review` | the target is a github pull request | clones the pr into a temp checkout and runs a read-only codex review |
| `debug` | the user wants root cause analysis or a non-mutating answer | runs codex read-only and returns diagnosis, evidence, suspected files, and smallest fix |
| `fix` | the user wants the issue fixed | clones the repo, runs codex, commits the diff, pushes a branch, and opens or updates a pr |
| `issue` | the user says issue but wants a fix | alias for `fix` |

prefer `debug` when the user asks what is wrong, why something fails, or wants an explanation. prefer `fix` when the user asks to repair, patch, implement the issue, or open a pr.

## agent usage

for non-interactive agent runs, pass json and no-prompt flags:

```bash
revisor debug https://github.com/owner/repo/issues/456 --no-input --json
revisor fix https://github.com/owner/repo/issues/456 --yes --no-input --json
```

use `--dry-run --json` before a mutating `fix` when you need to inspect the planned codex prompt and github actions:

```bash
revisor fix https://github.com/owner/repo/issues/456 --dry-run --yes --no-input --json
```

use `--patch-only` when the user wants the fix but does not want a branch pushed or pr opened:

```bash
revisor fix https://github.com/owner/repo/issues/456 --patch-only --patch ./issue-456.patch --yes
```

## safety checks

- confirm `codex`, `git`, and `gh` are installed before real runs.
- do not use `fix` without user intent to mutate github state. it opens or updates a pr by default.
- use `debug` for read-only diagnosis.
- use `review` only for pull request urls.
- use `--keep` only when the user needs the temp checkout; revisor deletes temp workspaces by default.
- if a command fails, report the revisor command, exit code, and the relevant stderr/stdout lines.

## output handling

when `--json` is used, treat the json summary as the stable completion artifact. progress and codex output can still appear on stdout or stderr, so do not parse free-form logs as the contract.
