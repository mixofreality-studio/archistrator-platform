package methodassets

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The registry is strict data: an unknown ref kind, an unknown field and a malformed waiver are
// refused, never skipped, so a typo cannot silently drop an enforcement claim.
func TestRegistry_Decodes(t *testing.T) {
	entries, err := DoctrineRegistry()
	if err != nil {
		t.Fatalf("the embedded registry must decode: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the registry is empty")
	}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	for _, e := range entries {
		if seen[e.ID] {
			t.Errorf("duplicate entry id %q", e.ID)
		}
		seen[e.ID] = true
		for _, r := range e.EnforcedBy {
			switch r.(type) {
			case MethodcheckRule:
				kinds["methodcheck"] = true
			case ArchRule:
				kinds["arch"] = true
			case GeneratorInvariant:
				kinds["generatorInvariant"] = true
			}
		}
	}
	for _, k := range []string{"methodcheck", "arch", "generatorInvariant"} {
		if !kinds[k] {
			t.Errorf("no seed entry exercises the %s ref kind", k)
		}
	}

	bad := map[string]string{
		"unknown ref kind": `{"entries":[{"id":"a","statement":"s","scope":"builtApp","enforcedBy":[{"kind":"vibes","rule":"X"}]}]}`,
		"unknown field":    `{"entries":[{"id":"a","statement":"s","scope":"builtApp","enforcedBy":[],"extra":1}]}`,
		"unknown ref field": `{"entries":[{"id":"a","statement":"s","scope":"builtApp",` +
			`"enforcedBy":[{"kind":"arch","rule":"R","firedBy":"TestX","note":"n"}]}]}`,
		"unknown scope":       `{"entries":[{"id":"a","statement":"s","scope":"everything"}]}`,
		"empty id":            `{"entries":[{"id":"","statement":"s","scope":"builtApp"}]}`,
		"empty rule":          `{"entries":[{"id":"a","statement":"s","scope":"builtApp","enforcedBy":[{"kind":"arch","rule":"","firedBy":"TestX"}]}]}`,
		"empty firedBy":       `{"entries":[{"id":"a","statement":"s","scope":"builtApp","enforcedBy":[{"kind":"methodcheck","rule":"R","firedBy":""}]}]}`,
		"bad waiver date":     `{"entries":[{"id":"a","statement":"s","scope":"builtApp","waiver":{"reason":"r","owner":"o","expires":"soon"}}]}`,
		"waiver without why":  `{"entries":[{"id":"a","statement":"s","scope":"builtApp","waiver":{"reason":"","owner":"o","expires":"2026-12-31"}}]}`,
		"duplicate entry ids": `{"entries":[{"id":"a","statement":"s","scope":"judgement"},{"id":"a","statement":"s","scope":"judgement"}]}`,
		"invariant no test":   `{"entries":[{"id":"a","statement":"s","scope":"builtApp","enforcedBy":[{"kind":"generatorInvariant","module":"m","package":"p","test":""}]}]}`,
		"trailing garbage":    `{"entries":[]} {"entries":[]}`,
	}
	for name, raw := range bad {
		if _, err := decodeRegistry([]byte(raw)); err == nil {
			t.Errorf("%s: want a decode error", name)
		}
	}
	good := `{"entries":[{"id":"a","statement":"s","scope":"builtApp","enforcedBy":[{"kind":"arch","rule":"R","firedBy":"TestX"}],"promptRefs":["claude/x.md#y"]}]}`
	if _, err := decodeRegistry([]byte(good)); err != nil {
		t.Errorf("a well-formed registry must decode: %v", err)
	}
}

func readCounter(t *testing.T, name string) int {
	t.Helper()
	raw, err := os.ReadFile(path.Join("doctrine", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return atoiCounter(t, name, string(raw))
}

func atoiCounter(t *testing.T, name, s string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		t.Fatalf("%s must hold one non-negative integer, got %q", name, s)
	}
	return n
}

// The number of waived entries is a ratchet that only decreases: waivers.max is lowered when a
// waiver is retired and never raised. Every expiry is a real date.
func TestRegistry_WaiversOnlyDecrease(t *testing.T) {
	entries, err := DoctrineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	max := readCounter(t, "waivers.max")
	n := 0
	for _, e := range entries {
		if e.Waiver == nil {
			continue
		}
		n++
		if _, err := time.Parse("2006-01-02", e.Waiver.Expires); err != nil {
			t.Errorf("%s: waiver expires %q is not YYYY-MM-DD", e.ID, e.Waiver.Expires)
		}
	}
	if n > max {
		t.Errorf("%d waived entries exceed waivers.max %d: a waiver is retired by enforcing the rule, never added", n, max)
	}
	if n < max {
		t.Errorf("%d waived entries are below waivers.max %d: lower waivers.max to lock the gain", n, max)
	}
}

func claudeFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(assetsFS, "assets/claude", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, rerr := assetsFS.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[strings.TrimPrefix(p, "assets/")] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every methodcheck or arch rule the registry says enforces a doctrine entry is cited as `[ID]`
// in at least one prompt: a rule the agents are never told about only fails them after the fact.
func TestRegistry_EnforcedRulesArePrompted(t *testing.T) {
	entries, err := DoctrineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	files := claudeFiles(t)
	cited := func(id string) bool {
		for _, body := range files {
			if strings.Contains(body, "["+id+"]") || strings.Contains(body, "["+id+",") {
				return true
			}
		}
		return false
	}
	for _, e := range entries {
		for _, r := range e.EnforcedBy {
			var id string
			switch v := r.(type) {
			case MethodcheckRule:
				id = v.Rule
			case ArchRule:
				id = v.Rule
			case GeneratorInvariant:
				continue
			}
			if !cited(id) {
				t.Errorf("%s: enforcing rule %s is cited as [%s] in no prompt under assets/claude", e.ID, id, id)
			}
		}
	}
}

// Every promptRef is "<file under assets>#<anchor text>"; the file exists and holds the anchor.
func TestRegistry_PromptRefsResolve(t *testing.T) {
	entries, err := DoctrineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	refs := 0
	for _, e := range entries {
		for _, ref := range e.PromptRefs {
			refs++
			file, anchor, ok := strings.Cut(ref, "#")
			if !ok || file == "" || anchor == "" {
				t.Errorf("%s: promptRef %q is not <file>#<anchor>", e.ID, ref)
				continue
			}
			b, err := assetsFS.ReadFile("assets/" + file)
			if err != nil {
				t.Errorf("%s: promptRef file %s: %v", e.ID, file, err)
				continue
			}
			if !strings.Contains(string(b), anchor) {
				t.Errorf("%s: %s does not contain %q", e.ID, file, anchor)
			}
		}
	}
	if refs == 0 {
		t.Error("no entry carries a promptRef")
	}
}

var (
	normativeWord = regexp.MustCompile(`(?i)\b(must|never|always|every|required)\b`)
	// A rule citation is `[RULE-ID]` or `[RULE-ID, note]`: an upper-case id with at least one dash.
	ruleCitation = regexp.MustCompile(`\[[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+[\],]`)
	sentenceEnd  = regexp.MustCompile(`[.!?]\s+`)
)

// uncitedNormativeSentences counts the sentences of the prose of body that use a normative word
// and cite no rule. Front matter and fenced blocks are not prose.
func uncitedNormativeSentences(body string) []string {
	var out []string
	inFence, inFront := false, false
	for i, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if i == 0 && trim == "---" {
			inFront = true
			continue
		}
		if inFront {
			if trim == "---" {
				inFront = false
			}
			continue
		}
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if inFence || trim == "" {
			continue
		}
		rest := trim
		for rest != "" {
			loc := sentenceEnd.FindStringIndex(rest)
			sentence := rest
			if loc == nil {
				rest = ""
			} else {
				sentence, rest = rest[:loc[1]], rest[loc[1]:]
			}
			if normativeWord.MatchString(sentence) && !ruleCitation.MatchString(sentence) {
				out = append(out, strings.TrimSpace(sentence))
			}
		}
	}
	return out
}

func TestPromptLint_Counter(t *testing.T) {
	body := "---\nname: x\n---\nYou must do it. Never do that `[ARCH-X]`. Fine here.\n```\nnever in a fence\n```\nAlways. Every [TP-A, note] counts."
	got := uncitedNormativeSentences(body)
	if len(got) != 2 {
		t.Fatalf("want 2 uncited sentences (You must do it.; Always.), got %d: %q", len(got), got)
	}
}

// A normative prompt sentence with no citation is prose nothing enforces. The count of such
// sentences over the commands and agents is a ratchet that only decreases: promptlint.max is
// lowered as sentences gain a citation (or are rewritten) and never raised.
func TestPromptLint_NormativeSentencesCiteARule(t *testing.T) {
	max := readCounter(t, "promptlint.max")
	n := 0
	for name, body := range claudeFiles(t) {
		if !strings.HasPrefix(name, "claude/commands/") && !strings.HasPrefix(name, "claude/agents/") {
			continue
		}
		n += len(uncitedNormativeSentences(body))
	}
	if n > max {
		t.Errorf("%d uncited normative sentences exceed promptlint.max %d: cite the enforcing rule as `[RULE-ID]`, or rewrite the sentence", n, max)
	}
	if n < max {
		t.Errorf("%d uncited normative sentences are below promptlint.max %d: lower promptlint.max to lock the gain", n, max)
	}
}
