package revisor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const Version = "0.1.4"

type Command string

const (
	CommandReview Command = "review"
	CommandDebug  Command = "debug"
	CommandFix    Command = "fix"
)

type TargetKind string

const (
	TargetPull  TargetKind = "pull"
	TargetIssue TargetKind = "issue"
	TargetRepo  TargetKind = "repo"
)

type Target struct {
	Owner  string     `json:"owner"`
	Repo   string     `json:"repo"`
	Kind   TargetKind `json:"kind"`
	Number int        `json:"number,omitempty"`
	URL    string     `json:"url"`
}

type Options struct {
	Branch         string
	CodexBin       string
	DryRun         bool
	JSON           bool
	Keep           bool
	Model          string
	NoInput        bool
	Patch          string
	PatchOnly      bool
	Sandbox        string
	TmpDir         string
	UpstreamBranch bool
	Yes            bool
}

type Args struct {
	Command Command
	Input   string
	Options Options
}

type Runtime struct {
	Cwd    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type RunSummary struct {
	Command  Command `json:"command"`
	Target   *Target `json:"target,omitempty"`
	TempPath string  `json:"tempPath,omitempty"`
	Kept     bool    `json:"kept"`
	Patch    string  `json:"patch,omitempty"`
	PRURL    string  `json:"prUrl,omitempty"`
	ExitCode int     `json:"exitCode"`
}

func SystemRuntime() Runtime {
	cwd, _ := os.Getwd()
	return Runtime{
		Cwd:    cwd,
		Env:    os.Environ(),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

func Main(ctx context.Context, argv []string, rt Runtime) (int, error) {
	parsed, mode, err := ParseArgs(argv)
	if err != nil {
		return 2, err
	}
	if mode == "help" {
		fmt.Fprint(rt.Stdout, Help())
		return 0, nil
	}
	if mode == "version" {
		fmt.Fprintln(rt.Stdout, Version)
		return 0, nil
	}
	return Run(ctx, parsed, rt)
}

func ParseArgs(argv []string) (Args, string, error) {
	options := Options{CodexBin: "codex", Sandbox: "workspace-write"}
	positionals := make([]string, 0, 2)

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "-h", "--help":
			return Args{}, "help", nil
		case "--version":
			return Args{}, "version", nil
		case "--dry-run":
			options.DryRun = true
		case "--json":
			options.JSON = true
		case "--keep":
			options.Keep = true
		case "--no-input":
			options.NoInput = true
		case "--patch-only":
			options.PatchOnly = true
		case "--upstream-branch":
			options.UpstreamBranch = true
		case "--yes", "-y":
			options.Yes = true
		case "--branch", "--codex", "--model", "--patch", "--sandbox", "--tmp-dir":
			if i+1 >= len(argv) {
				return Args{}, "", fmt.Errorf("%s requires a value", arg)
			}
			i++
			value := argv[i]
			switch arg {
			case "--branch":
				options.Branch = value
			case "--codex":
				options.CodexBin = value
			case "--model":
				options.Model = value
			case "--patch":
				options.Patch = value
			case "--sandbox":
				if !validSandbox(value) {
					return Args{}, "", fmt.Errorf("unsupported sandbox: %s", value)
				}
				options.Sandbox = value
			case "--tmp-dir":
				options.TmpDir = value
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return Args{}, "", fmt.Errorf("unknown flag: %s", arg)
			}
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) < 2 {
		return Args{}, "", errors.New("missing command or input")
	}

	command := Command(positionals[0])
	if command == "issue" {
		command = CommandFix
	}
	if command != CommandReview && command != CommandDebug && command != CommandFix {
		return Args{}, "", fmt.Errorf("unknown command: %s", positionals[0])
	}

	return Args{Command: command, Input: positionals[1], Options: options}, "", nil
}

func ParseGitHubTarget(input string) (*Target, error) {
	parsed, err := url.Parse(input)
	if err != nil || parsed.Host != "github.com" {
		return nil, nil
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil, nil
	}

	target := &Target{Owner: parts[0], Repo: parts[1], Kind: TargetRepo, URL: input}
	if len(parts) >= 4 && (parts[2] == "pull" || parts[2] == "issues") {
		number, err := strconv.Atoi(parts[3])
		if err != nil || number <= 0 {
			return nil, fmt.Errorf("invalid GitHub issue/PR number: %s", parts[3])
		}
		target.Number = number
		if parts[2] == "pull" {
			target.Kind = TargetPull
		} else {
			target.Kind = TargetIssue
		}
	}
	return target, nil
}

func Run(ctx context.Context, args Args, rt Runtime) (int, error) {
	target, err := ParseGitHubTarget(args.Input)
	if err != nil {
		return 2, err
	}
	if err := validateTarget(args.Command, target); err != nil {
		return 2, err
	}
	if args.Command == CommandFix && !args.Options.PatchOnly && !args.Options.Yes {
		if err := confirmRemoteMutation(rt); err != nil {
			return 2, err
		}
	}

	tempRoot := ""
	repoPath := rt.Cwd
	if target != nil {
		parent := args.Options.TmpDir
		if parent == "" {
			parent = os.TempDir()
		}
		tempRoot, err = os.MkdirTemp(parent, "revisor-")
		if err != nil {
			return 1, err
		}
		repoPath = filepath.Join(tempRoot, target.Repo)
	}

	summary := RunSummary{Command: args.Command, Target: target, TempPath: repoPath}
	if args.Options.Keep {
		summary.Kept = true
	}
	cleanup := tempRoot != "" && !args.Options.Keep
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempRoot)
		}
	}()

	if target != nil {
		if err := cloneTarget(ctx, target, repoPath, args.Options, rt); err != nil {
			return 1, err
		}
		if target.Kind == TargetPull {
			if err := checkoutPull(ctx, target, repoPath, args.Options, rt); err != nil {
				return 1, err
			}
		}
	}

	metadata := ""
	if target != nil {
		metadata = fetchMetadata(ctx, target, repoPath, args.Options, rt)
	}

	switch args.Command {
	case CommandReview, CommandDebug:
		code, err := runCodex(ctx, args.Command, BuildPrompt(args.Command, args.Input, target, metadata), repoPath, args.Options, rt)
		summary.ExitCode = code
		if args.Options.Keep && tempRoot != "" {
			fmt.Fprintf(rt.Stderr, "revisor: kept temp checkout at %s\n", repoPath)
		}
		if args.Options.JSON {
			writeJSON(rt.Stdout, summary)
		}
		return code, err
	case CommandFix:
		code, err := runFix(ctx, args, target, metadata, repoPath, &summary, rt)
		if args.Options.Keep {
			fmt.Fprintf(rt.Stderr, "revisor: kept temp checkout at %s\n", repoPath)
		}
		if args.Options.JSON {
			writeJSON(rt.Stdout, summary)
		}
		return code, err
	default:
		return 2, fmt.Errorf("unsupported command: %s", args.Command)
	}
}

func runFix(ctx context.Context, args Args, target *Target, metadata, repoPath string, summary *RunSummary, rt Runtime) (int, error) {
	if target == nil || target.Kind != TargetIssue {
		return 2, errors.New("fix requires a GitHub issue URL")
	}
	if !args.Options.PatchOnly && !commandSucceeds(ctx, rt, "gh", "auth", "status") {
		return 2, errors.New("fix requires authenticated gh unless --patch-only is used")
	}

	defaultBranch := ghField(ctx, rt, repoPath, "repo", "view", target.FullName(), "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name")
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	branch := args.Options.Branch
	if branch == "" {
		branch = DefaultBranchName(target, metadata)
	}
	if err := prepareFixBranch(ctx, target, repoPath, branch, defaultBranch, args.Options, rt); err != nil {
		return 1, err
	}

	code, err := runCodex(ctx, CommandFix, BuildPrompt(CommandFix, args.Input, target, metadata), repoPath, args.Options, rt)
	summary.ExitCode = code
	if err != nil || code != 0 {
		return code, err
	}

	if commandSucceeds(ctx, withCwd(rt, repoPath), "git", "diff", "--quiet") {
		if args.Options.PatchOnly {
			return 1, errors.New("codex completed without producing a diff")
		}
		prURL, err := updateExistingPR(ctx, target, repoPath, branch, metadata, args.Options, summary, rt)
		if err != nil {
			return 1, err
		}
		if prURL == "" {
			return 1, errors.New("codex completed without producing a diff and no existing PR was found")
		}
		return 0, nil
	}

	if args.Options.PatchOnly {
		patch := args.Options.Patch
		if patch == "" {
			patch = DefaultPatchPath(rt.Cwd, target)
		}
		if err := writePatch(ctx, repoPath, patch, rt); err != nil {
			return 1, err
		}
		summary.Patch = patch
		return 0, nil
	}

	if err := commitAndOpenPR(ctx, target, repoPath, branch, defaultBranch, metadata, args.Options, summary, rt); err != nil {
		return 1, err
	}
	return 0, nil
}

func cloneTarget(ctx context.Context, target *Target, repoPath string, options Options, rt Runtime) error {
	if options.DryRun {
		fmt.Fprintf(rt.Stderr, "[dry-run] gh repo clone %s %s -- --quiet\n", target.FullName(), repoPath)
		return nil
	}
	if commandSucceeds(ctx, rt, "gh", "auth", "status") {
		return runStream(ctx, rt, rt.Cwd, "gh", "repo", "clone", target.FullName(), repoPath, "--", "--quiet")
	}
	return runStream(ctx, rt, rt.Cwd, "git", "clone", "--quiet", "https://github.com/"+target.FullName()+".git", repoPath)
}

func checkoutPull(ctx context.Context, target *Target, repoPath string, options Options, rt Runtime) error {
	if options.DryRun {
		fmt.Fprintf(rt.Stderr, "[dry-run] gh pr checkout %d\n", target.Number)
		return nil
	}
	local := withCwd(rt, repoPath)
	if commandSucceeds(ctx, local, "gh", "auth", "status") {
		return runStream(ctx, local, repoPath, "gh", "pr", "checkout", strconv.Itoa(target.Number))
	}
	branch := fmt.Sprintf("revisor-pr-%d", target.Number)
	if err := runStream(ctx, local, repoPath, "git", "fetch", "origin", fmt.Sprintf("pull/%d/head:%s", target.Number, branch)); err != nil {
		return err
	}
	return runStream(ctx, local, repoPath, "git", "checkout", branch)
}

func prepareFixBranch(ctx context.Context, target *Target, repoPath, branch, defaultBranch string, options Options, rt Runtime) error {
	local := withCwd(rt, repoPath)
	remote := "origin"
	if options.DryRun {
		if !options.UpstreamBranch {
			remote = "fork"
			fmt.Fprintf(rt.Stderr, "[dry-run] gh repo fork %s --remote=true --remote-name fork --clone=false\n", target.FullName())
			fmt.Fprintf(rt.Stderr, "[dry-run] if fork is unavailable but upstream is writable, use origin for the PR branch\n")
		}
		fmt.Fprintf(rt.Stderr, "[dry-run] git fetch %s %s || true\n", remote, branch)
		fmt.Fprintf(rt.Stderr, "[dry-run] git checkout -B %s <existing-branch-or-origin/%s>\n", branch, defaultBranch)
		return nil
	}
	if !options.UpstreamBranch {
		_, _ = runCapture(ctx, local, repoPath, "gh", "repo", "fork", target.FullName(), "--remote=true", "--remote-name", "fork", "--clone=false")
		if !commandSucceeds(ctx, local, "git", "remote", "get-url", "fork") {
			if !canWriteRepo(ctx, local, repoPath, target) {
				return errors.New("could not create or find fork remote, and upstream is not writable; use --patch-only")
			}
			fmt.Fprintln(rt.Stderr, "revisor: fork unavailable; using upstream branch because gh reports write access")
			remote = "origin"
		} else {
			remote = "fork"
		}
	}
	if commandSucceeds(ctx, local, "git", "fetch", remote, branch) {
		return runStream(ctx, local, repoPath, "git", "checkout", "-B", branch, "FETCH_HEAD")
	}
	return runStream(ctx, local, repoPath, "git", "checkout", "-B", branch, "origin/"+defaultBranch)
}

func commitAndOpenPR(ctx context.Context, target *Target, repoPath, branch, defaultBranch, metadata string, options Options, summary *RunSummary, rt Runtime) error {
	local := withCwd(rt, repoPath)
	title := prTitle(target, metadata)
	body := prBody(target, branch)
	remote := "fork"
	if options.UpstreamBranch {
		remote = "origin"
	}
	if !commandSucceeds(ctx, local, "git", "remote", "get-url", remote) {
		remote = "origin"
	}

	if options.DryRun {
		fmt.Fprintf(rt.Stderr, "[dry-run] git add -A && git commit -m %q\n", title)
		fmt.Fprintf(rt.Stderr, "[dry-run] git push -u %s %s\n", remote, branch)
		fmt.Fprintf(rt.Stderr, "[dry-run] gh pr create/edit for %s\n", branch)
		return nil
	}

	if err := runStream(ctx, local, repoPath, "git", "add", "-A"); err != nil {
		return err
	}
	commitMessage := fmt.Sprintf("%s\n\nRevisor-Run: %s\n", title, target.URL)
	if err := runStream(ctx, local, repoPath, "git", "-c", "user.name=Microck", "-c", "user.email=contact@micr.dev", "commit", "-m", commitMessage); err != nil {
		return err
	}
	if err := runStream(ctx, local, repoPath, "git", "push", "-u", remote, branch); err != nil {
		return err
	}

	head := branch
	if !options.UpstreamBranch {
		forkURL := strings.TrimSpace(captureOrEmpty(ctx, local, repoPath, "git", "remote", "get-url", "fork"))
		if owner, _, ok := parseGitHubRemote(forkURL); ok {
			head = owner + ":" + branch
		}
	}
	existing := existingPRURL(ctx, local, repoPath, head)
	if existing != "" {
		if err := editPR(ctx, local, repoPath, target, existing, title, body); err != nil {
			return err
		}
		summary.PRURL = existing
		fmt.Fprintf(rt.Stderr, "revisor: updated PR %s\n", existing)
		return nil
	}

	out, err := runCapture(ctx, local, repoPath, "gh", "pr", "create", "--base", defaultBranch, "--head", head, "--title", title, "--body", body)
	if err != nil {
		return err
	}
	summary.PRURL = strings.TrimSpace(out)
	fmt.Fprintf(rt.Stderr, "revisor: opened PR %s\n", summary.PRURL)
	return nil
}

func updateExistingPR(ctx context.Context, target *Target, repoPath, branch, metadata string, options Options, summary *RunSummary, rt Runtime) (string, error) {
	local := withCwd(rt, repoPath)
	head := branch
	if !options.UpstreamBranch {
		forkURL := strings.TrimSpace(captureOrEmpty(ctx, local, repoPath, "git", "remote", "get-url", "fork"))
		if owner, _, ok := parseGitHubRemote(forkURL); ok {
			head = owner + ":" + branch
		}
	}

	existing := existingPRURL(ctx, local, repoPath, head)
	if existing == "" && head != branch {
		existing = existingPRURL(ctx, local, repoPath, branch)
	}
	if existing == "" {
		return "", nil
	}

	if options.DryRun {
		fmt.Fprintf(rt.Stderr, "[dry-run] gh pr edit %s for unchanged branch %s\n", existing, branch)
		summary.PRURL = existing
		return existing, nil
	}

	summary.PRURL = existing
	if err := editPR(ctx, local, repoPath, target, existing, prTitle(target, metadata), prBody(target, branch)); err != nil {
		fmt.Fprintf(rt.Stderr, "revisor: found existing PR %s but could not update body: %v\n", existing, err)
		return existing, nil
	}
	fmt.Fprintf(rt.Stderr, "revisor: no new diff; updated existing PR %s\n", existing)
	return existing, nil
}

func runCodex(ctx context.Context, command Command, prompt, repoPath string, options Options, rt Runtime) (int, error) {
	args := []string{"exec", "--cd", repoPath, "--ephemeral", "--sandbox", options.Sandbox}
	if command == CommandReview || command == CommandDebug {
		args = []string{"exec", "--cd", repoPath, "--ephemeral", "--sandbox", "read-only"}
	}
	if options.Model != "" {
		args = append(args, "--model", options.Model)
	}
	if options.NoInput {
		args = append(args, "--config", `approval_policy="never"`)
	}
	args = append(args, "-")

	if options.DryRun {
		fmt.Fprintf(rt.Stderr, "[dry-run] %s %s\n", options.CodexBin, strings.Join(args, " "))
		fmt.Fprint(rt.Stdout, prompt)
		return 0, nil
	}
	return runStreamInput(ctx, rt, repoPath, prompt, options.CodexBin, args...)
}

func BuildPrompt(command Command, input string, target *Target, metadata string) string {
	context := "Input: " + input
	if target != nil {
		context += fmt.Sprintf("\n\nGitHub target: %s %s", target.FullName(), target.Kind)
		if target.Number > 0 {
			context += fmt.Sprintf(" #%d", target.Number)
		}
	}
	if metadata != "" {
		context += "\n\nGitHub metadata:\n" + metadata
	}

	switch command {
	case CommandReview:
		return "You are running Revisor's bundled codex-review workflow.\n\nReview the checked-out PR branch for correctness, regressions, security issues, missing tests, and maintainability problems. Do not modify files. Report actionable findings first, ordered by severity, with file and line references where possible. If there are no actionable findings, say that clearly.\n\n" + context + "\n"
	case CommandDebug:
		return "You are running Revisor's bundled codex-debugging workflow.\n\nDiagnose the reported behavior from source first. Run focused repros or tests when useful. Do not push changes or create a PR. Return root cause, evidence, suspected files, and the smallest recommended fix.\n\n" + context + "\n"
	default:
		return "You are running Revisor's bundled codex-fix workflow.\n\nFix the GitHub issue in this temporary checkout. Inspect the code, implement the smallest correct change, and run the most relevant verification you can discover. Do not push changes yourself; Revisor handles commit, push, and PR creation after you finish.\n\nFinal response must include files changed, behavior changed, verification command/result, and remaining risk.\n\n" + context + "\n"
	}
}

func fetchMetadata(ctx context.Context, target *Target, repoPath string, options Options, rt Runtime) string {
	if options.DryRun || !commandSucceeds(ctx, withCwd(rt, repoPath), "gh", "auth", "status") || target.Kind == TargetRepo {
		return ""
	}
	if target.Kind == TargetPull {
		return captureOrEmpty(ctx, withCwd(rt, repoPath), repoPath, "gh", "pr", "view", strconv.Itoa(target.Number), "--json", "title,body,author,baseRefName,headRefName,url")
	}
	return captureOrEmpty(ctx, withCwd(rt, repoPath), repoPath, "gh", "issue", "view", strconv.Itoa(target.Number), "--json", "title,body,author,labels,state,url")
}

func existingPRURL(ctx context.Context, rt Runtime, repoPath, head string) string {
	existing := strings.TrimSpace(captureOrEmpty(ctx, rt, repoPath, "gh", "pr", "list", "--head", head, "--state", "open", "--json", "url", "--jq", ".[0].url"))
	if existing == "null" {
		return ""
	}
	return existing
}

func editPR(ctx context.Context, rt Runtime, repoPath string, target *Target, prURL, title, body string) error {
	number, ok := prNumberFromURL(prURL)
	if !ok {
		return fmt.Errorf("could not parse PR number from %s", prURL)
	}
	_, err := runCapture(ctx, rt, repoPath, "gh", "api", "-X", "PATCH", fmt.Sprintf("repos/%s/pulls/%d", target.FullName(), number), "-f", "title="+title, "-f", "body="+body)
	return err
}

func prNumberFromURL(prURL string) (int, bool) {
	parsed, err := url.Parse(prURL)
	if err != nil {
		return 0, false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return 0, false
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}

func canWriteRepo(ctx context.Context, rt Runtime, repoPath string, target *Target) bool {
	permission := strings.TrimSpace(captureOrEmpty(ctx, rt, repoPath, "gh", "repo", "view", target.FullName(), "--json", "viewerPermission", "--jq", ".viewerPermission"))
	return permission == "ADMIN" || permission == "MAINTAIN" || permission == "WRITE"
}

func DefaultBranchName(target *Target, metadata string) string {
	title := jsonField(metadata, "title")
	slug := slugify(title)
	if slug == "" {
		slug = "fix"
	}
	return fmt.Sprintf("revisor/issue-%d-%s", target.Number, slug)
}

func DefaultPatchPath(cwd string, target *Target) string {
	return filepath.Join(cwd, fmt.Sprintf("revisor-%s-%s-issue-%d.patch", target.Owner, target.Repo, target.Number))
}

func Help() string {
	return `revisor ` + Version + `

USAGE
  revisor review <github-pr-url> [flags]
  revisor debug <github-url-or-text> [flags]
  revisor fix <github-issue-url> [flags]
  revisor issue <github-issue-url> [flags]

COMMANDS
  review   Clone a PR into a temp checkout and run a bundled Codex review workflow.
  debug    Diagnose an issue/PR/text prompt without changing remote state.
  fix      Fix a GitHub issue, push a fork branch, and open or update a PR.
  issue    Alias for fix.

FLAGS
  --branch <name>         Override the default revisor/issue-N-slug branch.
  --codex <path>          Codex executable. Default: codex
  --dry-run               Print planned commands and prompt without mutating state.
  --json                  Print a machine-readable run summary.
  --keep                  Keep the temp checkout and print its path.
  --model <model>         Pass a model to codex exec.
  --no-input              Disable Codex approval prompts.
  --patch <path>          Patch path for --patch-only.
  --patch-only            Fix locally and write a patch instead of opening a PR.
  --sandbox <mode>        Codex sandbox: read-only, workspace-write, danger-full-access.
  --tmp-dir <path>        Parent directory for temp checkouts.
  --upstream-branch       Push to upstream instead of fork.
  -y, --yes               Approve push/PR creation without an interactive prompt.
  -h, --help              Show help.
  --version               Show version.
`
}

func validateTarget(command Command, target *Target) error {
	if command == CommandReview && (target == nil || target.Kind != TargetPull) {
		return errors.New("review requires a GitHub PR URL")
	}
	if command == CommandFix && (target == nil || target.Kind != TargetIssue) {
		return errors.New("fix requires a GitHub issue URL")
	}
	return nil
}

func confirmRemoteMutation(rt Runtime) error {
	file, ok := rt.Stdin.(*os.File)
	if !ok {
		return errors.New("fix opens a PR; pass --yes for non-interactive use or --patch-only to avoid remote changes")
	}
	stat, err := file.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice == 0 {
		return errors.New("fix opens a PR; pass --yes for non-interactive use or --patch-only to avoid remote changes")
	}
	fmt.Fprint(rt.Stderr, "revisor fix will push a branch and open/update a PR. Continue? [y/N] ")
	answer, _ := bufio.NewReader(rt.Stdin).ReadString('\n')
	if strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes") {
		return nil
	}
	return errors.New("cancelled")
}

func writePatch(ctx context.Context, repoPath, patch string, rt Runtime) error {
	out, err := runCapture(ctx, withCwd(rt, repoPath), repoPath, "git", "diff", "--binary")
	if err != nil {
		return err
	}
	if err := os.WriteFile(patch, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(rt.Stderr, "revisor: wrote patch to %s\n", patch)
	return nil
}

func (t *Target) FullName() string {
	return t.Owner + "/" + t.Repo
}

func prTitle(target *Target, metadata string) string {
	title := jsonField(metadata, "title")
	if title == "" {
		return fmt.Sprintf("fix: address issue #%d", target.Number)
	}
	return "fix: " + title
}

func prBody(target *Target, branch string) string {
	return fmt.Sprintf(`<!-- revisor:issue=%s branch=%s -->

Fixes %s

Generated by Revisor using the user's installed Codex CLI.
`, target.URL, branch, target.URL)
}

func jsonField(raw, field string) string {
	var data map[string]any
	if json.Unmarshal([]byte(raw), &data) != nil {
		return ""
	}
	value, _ := data[field].(string)
	return value
}

func slugify(input string) string {
	lower := strings.ToLower(input)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := strings.Trim(re.ReplaceAllString(lower, "-"), "-")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	return slug
}

func validSandbox(value string) bool {
	return value == "read-only" || value == "workspace-write" || value == "danger-full-access"
}

func writeJSON(w io.Writer, summary RunSummary) {
	encoded, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Fprintln(w, string(encoded))
}

func withCwd(rt Runtime, cwd string) Runtime {
	rt.Cwd = cwd
	return rt
}

func commandSucceeds(ctx context.Context, rt Runtime, name string, args ...string) bool {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = rt.Cwd
	cmd.Env = rt.Env
	return cmd.Run() == nil
}

func ghField(ctx context.Context, rt Runtime, cwd string, args ...string) string {
	return strings.TrimSpace(captureOrEmpty(ctx, rt, cwd, "gh", args...))
}

func captureOrEmpty(ctx context.Context, rt Runtime, cwd, name string, args ...string) string {
	out, err := runCapture(ctx, rt, cwd, name, args...)
	if err != nil {
		return ""
	}
	return out
}

func runCapture(ctx context.Context, rt Runtime, cwd, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	cmd.Env = rt.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return stdout.String(), fmt.Errorf("%s: %s", name, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func runStream(ctx context.Context, rt Runtime, cwd, name string, args ...string) error {
	code, err := runStreamInput(ctx, rt, cwd, "", name, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%s exited with %d", name, code)
	}
	return nil
}

func runStreamInput(ctx context.Context, rt Runtime, cwd, stdin, name string, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	cmd.Env = rt.Env
	cmd.Stdout = rt.Stdout
	cmd.Stderr = rt.Stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if err := cmd.Start(); err != nil {
		return 127, err
	}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func parseGitHubRemote(remote string) (owner, repo string, ok bool) {
	remote = strings.TrimSuffix(remote, ".git")
	if strings.HasPrefix(remote, "git@github.com:") {
		parts := strings.Split(strings.TrimPrefix(remote, "git@github.com:"), "/")
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
	}
	parsed, err := url.Parse(remote)
	if err == nil && parsed.Host == "github.com" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}
