package testgen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// designElements is the design's elements in declaration order; the first declaration of an id wins.
func designElements(d methodcheck.UIDesignRecord) []methodcheck.UIElement {
	seen := map[string]bool{}
	var out []methodcheck.UIElement
	for _, s := range d.Screens {
		for _, e := range s.Elements {
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out
}

// elementsTS is a surface's elements.gen.ts: the inventory of its UI design (role, accessible name,
// containing element, whether it repeats) and `locate`, the one function every generated spec finds an
// element with. A locator is how a user finds a control: by its role and accessible name, inside the
// element that contains it. It is never a test id or a selector.
func elementsTS(surface string, d methodcheck.UIDesignRecord) string {
	var b strings.Builder
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.uiDesign.%s\n", surface)
	b.WriteString(`import type { Locator, Page } from '@playwright/test';

type Role = Parameters<Page['getByRole']>[0];

type Spec = {
  readonly role: Role;
  readonly name: string;
  readonly within?: ElementId;
  readonly repeats: boolean;
};

export const elements = {
`)
	for _, e := range designElements(d) {
		fmt.Fprintf(&b, "  %s: { role: %s, name: %s, %srepeats: %t },\n", tsStr(e.ID), tsStr(string(e.Role)), tsStr(e.Name), withinField(e), e.Repeats)
	}
	b.WriteString(`} as const;

export type ElementId = keyof typeof elements;

/**
 * Finds an element the way a user does: by role and accessible name, inside the element that
 * contains it. nth picks one instance of an element that repeats (zero-based).
 */
export function locate(page: Page, id: ElementId, nth?: number): Locator {
  const spec: Spec = elements[id];
  const scope: Page | Locator = spec.within === undefined ? page : locate(page, spec.within);
  const found = scope.getByRole(spec.role, spec.name === '' ? {} : { name: spec.name, exact: true });
  return nth === undefined ? found : found.nth(nth);
}
`)
	return b.String()
}

// withinField spells the containing element of e, with its trailing separator, or nothing.
func withinField(e methodcheck.UIElement) string {
	if e.Within == "" {
		return ""
	}
	return "within: " + tsStr(e.Within) + ", "
}

// checkWithin refuses a design whose containers loop (a within b, b within a): `locate` resolves an
// element by first locating its container, so a loop would recurse for ever at run time. methodcheck's
// UID-WITHIN rejects only a self-reference and a container off the screen.
func checkWithin(d methodcheck.UIDesignRecord) error {
	byID := map[string]methodcheck.UIElement{}
	for _, e := range designElements(d) {
		byID[e.ID] = e
	}
	for _, e := range designElements(d) {
		chain := []string{e.ID}
		for cur := e; cur.Within != ""; {
			next, ok := byID[cur.Within]
			if !ok {
				break // contained by an element the design lacks: methodcheck's finding, not a loop
			}
			if i := slices.Index(chain, next.ID); i >= 0 {
				return fmt.Errorf("uiDesign elements contain each other in a loop (%s): an element is located inside its container, so the loop can never be located", strings.Join(append(chain[i:], next.ID), " within "))
			}
			chain = append(chain, next.ID)
			cur = next
		}
	}
	return nil
}
