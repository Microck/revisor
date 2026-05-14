package revisor

import (
	"strings"
	"testing"
)

func TestParseGitHubTarget(t *testing.T) {
	target, err := ParseGitHubTarget("https://github.com/acme/widgets/pull/42")
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.Owner != "acme" || target.Repo != "widgets" || target.Kind != TargetPull || target.Number != 42 {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestParseArgsIssueAlias(t *testing.T) {
	args, mode, err := ParseArgs([]string{"issue", "https://github.com/acme/widgets/issues/7", "--json", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "" || args.Command != CommandFix || !args.Options.JSON || !args.Options.Yes {
		t.Fatalf("unexpected args: %#v mode=%q", args, mode)
	}
}

func TestDefaultBranchNameUsesIssueSlug(t *testing.T) {
	target := &Target{Owner: "acme", Repo: "widgets", Kind: TargetIssue, Number: 7}
	branch := DefaultBranchName(target, `{"title":"Crash when opening settings!"}`)
	if branch != "revisor/issue-7-crash-when-opening-settings" {
		t.Fatalf("unexpected branch: %s", branch)
	}
}

func TestFixPromptLeavesRemoteMutationToRevisor(t *testing.T) {
	target := &Target{Owner: "acme", Repo: "widgets", Kind: TargetIssue, Number: 7, URL: "https://github.com/acme/widgets/issues/7"}
	prompt := BuildPrompt(CommandFix, target.URL, target, `{"title":"Bug"}`)
	for _, want := range []string{"Fix the GitHub issue", "Do not push changes yourself", "GitHub metadata"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestParseGitHubRemote(t *testing.T) {
	for _, remote := range []string{"git@github.com:alice/widgets.git", "https://github.com/alice/widgets.git"} {
		owner, repo, ok := parseGitHubRemote(remote)
		if !ok || owner != "alice" || repo != "widgets" {
			t.Fatalf("unexpected parse for %s: %s %s %v", remote, owner, repo, ok)
		}
	}
}

func TestPRNumberFromURL(t *testing.T) {
	number, ok := prNumberFromURL("https://github.com/Microck/tailstick/pull/47")
	if !ok || number != 47 {
		t.Fatalf("unexpected PR number parse: %d %v", number, ok)
	}
}
