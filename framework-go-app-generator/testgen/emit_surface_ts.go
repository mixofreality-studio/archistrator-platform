package testgen

import (
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// surfaceSpecTS is a surface's surface.spec.gen.ts: for each screen of the UI design, go to its route
// and check that every element the design says is there is attached to the page, found by the role and
// name the design gives it. It runs before any scenario (the generated config makes every scenario
// depend on it), so a built surface that does not match its design fails once, naming the element,
// instead of failing every scenario at its first locator.
//
// An element that repeats is one per item, and an element inside a repeating one is inside an item:
// neither is on a page that holds no items, so neither is checked here; the scenarios that put an item
// there find them.
func surfaceSpecTS(surface string, d methodcheck.UIDesignRecord) string {
	var b strings.Builder
	b.WriteString(genHeader + "\n")
	fmt.Fprintf(&b, "// Source: project.json .phaseArtifacts.uiDesign.%s\n", surface)
	if len(d.Screens) == 0 {
		b.WriteString("// The design has no screens: nothing to check.\n")
		return b.String()
	}
	byID := map[string]methodcheck.UIElement{}
	for _, e := range designElements(d) {
		byID[e.ID] = e
	}
	b.WriteString("import { test, expect } from '@playwright/test';\nimport { locate } from './elements.gen';\n")
	for _, s := range d.Screens {
		annotation := fmt.Sprintf("{ type: 'surface', description: %s }", tsStr(s.ID))
		fmt.Fprintf(&b, "\ntest(%s, { annotation: %s }, async ({ page }) => {\n", tsStr("surface — "+s.ID), annotation)
		fmt.Fprintf(&b, "  await page.goto(%s);\n", tsStr(s.Route))
		for _, e := range s.Elements {
			if !onEmptyPage(e, byID) {
				continue
			}
			fmt.Fprintf(&b, "  await expect(locate(page, %s)).toBeAttached();\n", tsStr(e.ID))
		}
		b.WriteString("});\n")
	}
	return b.String()
}

// onEmptyPage reports whether an element is on a page that holds no items: it does not repeat and
// nothing that contains it does.
func onEmptyPage(e methodcheck.UIElement, byID map[string]methodcheck.UIElement) bool {
	seen := map[string]bool{}
	for cur, ok := e, true; ok; cur, ok = byID[cur.Within] {
		if cur.Repeats || seen[cur.ID] {
			return false
		}
		seen[cur.ID] = true
		if cur.Within == "" {
			return true
		}
	}
	return false // contained by an element the design does not have
}
