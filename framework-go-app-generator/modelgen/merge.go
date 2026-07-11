package modelgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/google/jsonschema-go/jsonschema"
)

// parsedEntry is one `.serviceContracts` entry, parsed once for the merge
// path: its key, its metadata, and its full decoded schema document (which
// carries both `$defs` and, in Extra, the `interface` descriptor).
type parsedEntry struct {
	key  string
	meta contractMeta
	doc  jsonschema.Schema
}

// mergedDefSet accumulates the deduped $defs type block across every entry in
// a goPackage group: the decoded defs to emit (defs) and the property order
// recovered per def name (order), both keyed by def name.
type mergedDefSet struct {
	defs  map[string]*jsonschema.Schema
	order map[string][]string
}

// genGroup emits ONE contract.gen.go for a goPackage shared by two or more
// `.serviceContracts` entries — the multi-component-per-package case (e.g. a
// primary ResourceAccess plus one or more secondary RAs re-homed onto the same
// Go package because RA→RA imports are banned). keys MUST already be sorted
// ascending (the contract-key order IS the deterministic merge order: the
// alphabetically-first key is conventionally the package's original/primary
// component, but the rule applies uniformly — there is no other tie-break).
//
// The merge has two parts:
//
//  1. $defs: every entry's $defs are folded into one type block, emitted once
//     each (see emitDefs, parseAndMergeGroup). A def name that appears in more
//     than one entry MUST be byte-identical (after JSON canonicalization)
//     across all entries that declare it — that is the expected case for
//     types the components share (e.g. a common ID type). A same-named def
//     with a DIFFERENT shape across entries is a hard error: silently picking
//     one entry's version would silently drop the other's fields, which is
//     exactly the failure mode this function exists to rule out.
//  2. interface + impl surface: emitted once per entry, in key order, exactly
//     as genOne would for that entry alone — each entry keeps its own
//     Interface, Component, Infra, Deps (see emitGroupEntry).
//
// For a single-entry group, Generate calls genOne directly instead (see
// emitGoPackage) so single-component packages are byte-identical to before
// this function existed; genGroup only runs when len(keys) > 1.
func genGroup(goPkg string, keys []string, contracts map[string]json.RawMessage, modulePath string, allow map[string]bool, resolver map[string]contractRef) ([]byte, error) {
	pendingImports = map[string]string{}
	var buf bytes.Buffer

	entries, merged, err := parseAndMergeGroup(goPkg, keys, contracts)
	if err != nil {
		return nil, err
	}

	combined := &jsonschema.Schema{Defs: merged.defs}
	if _, err := emitDefs(&buf, combined, merged.order); err != nil {
		return nil, fmt.Errorf("modelgen: emit merged defs for %s: %w", goPkg, err)
	}

	for _, e := range entries {
		if err := emitGroupEntry(&buf, e, modulePath, allow, resolver); err != nil {
			return nil, err
		}
	}

	return formatGeneratedFile(filepath.Base(goPkg), &buf)
}

// parseAndMergeGroup parses every entry in the group (in key order) and folds
// their $defs into one deduped mergedDefSet, failing loudly on a same-name
// collision (see mergeEntryDefs). Returns the parsed entries (for the
// interface+impl pass in genGroup) alongside the merged defs.
func parseAndMergeGroup(goPkg string, keys []string, contracts map[string]json.RawMessage) ([]parsedEntry, mergedDefSet, error) {
	merged := mergedDefSet{defs: map[string]*jsonschema.Schema{}, order: map[string][]string{}}
	mergedRawDefs := map[string]json.RawMessage{}
	definedBy := map[string]string{} // def name -> first contract key that defined it
	entries := make([]parsedEntry, 0, len(keys))

	for _, k := range keys {
		e, rawDefs, order, err := parseGroupEntry(k, contracts[k])
		if err != nil {
			return nil, mergedDefSet{}, err
		}
		if err := mergeEntryDefs(goPkg, e, rawDefs, order, merged, mergedRawDefs, definedBy); err != nil {
			return nil, mergedDefSet{}, err
		}
		entries = append(entries, e)
	}
	return entries, merged, nil
}

// parseGroupEntry decodes one contract entry's metadata + schema document, and
// separately recovers its raw (undecoded) $defs + property order — the raw
// forms feed the byte-level collision comparison in mergeEntryDefs.
func parseGroupEntry(k string, raw json.RawMessage) (parsedEntry, map[string]json.RawMessage, map[string][]string, error) {
	var meta contractMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return parsedEntry{}, nil, nil, fmt.Errorf("parse contract %q: %w", k, err)
	}

	var doc jsonschema.Schema
	if err := json.Unmarshal(raw, &doc); err != nil {
		return parsedEntry{}, nil, nil, fmt.Errorf("modelgen %s: parse schema: %w", k, err)
	}
	if len(doc.Defs) == 0 {
		return parsedEntry{}, nil, nil, fmt.Errorf("modelgen %s: schema has no $defs (every built contract must declare its model types)", k)
	}

	rawDefs, err := rawDefsOf(raw)
	if err != nil {
		return parsedEntry{}, nil, nil, fmt.Errorf("modelgen %s: %w", k, err)
	}

	return parsedEntry{key: k, meta: meta, doc: doc}, rawDefs, propOrder(raw), nil
}

// mergeEntryDefs folds one entry's $defs into merged, in place. A def name not
// yet seen is added outright. A def name already contributed by an earlier
// entry in this group is skipped when byte-identical (the shared-type case)
// and a hard error when it differs in shape — never silently overwritten.
func mergeEntryDefs(goPkg string, e parsedEntry, rawDefs map[string]json.RawMessage, order map[string][]string, merged mergedDefSet, mergedRawDefs map[string]json.RawMessage, definedBy map[string]string) error {
	for name, def := range e.doc.Defs {
		if prior, ok := definedBy[name]; ok {
			if err := requireSameShape(goPkg, name, prior, e.key, mergedRawDefs[name], rawDefs[name]); err != nil {
				return err
			}
			continue // identical shape already merged in — keep the first
		}
		merged.defs[name] = def
		mergedRawDefs[name] = rawDefs[name]
		definedBy[name] = e.key
		if len(order[name]) > 0 {
			merged.order[name] = order[name]
		}
	}
	return nil
}

// requireSameShape errors unless a and b (the same-named $def's raw JSON from
// two different entries in the group) are shape-identical.
func requireSameShape(goPkg, defName, firstKey, secondKey string, a, b json.RawMessage) error {
	equal, err := defsEqual(a, b)
	if err != nil {
		return fmt.Errorf("modelgen: compare $def %q shared by %q and %q: %w", defName, firstKey, secondKey, err)
	}
	if !equal {
		return fmt.Errorf("modelgen: $def %q is defined differently in contracts %q and %q, both sharing goPackage %q — merging requires an identical shape; rename one of the types or make them match", defName, firstKey, secondKey, goPkg)
	}
	return nil
}

// emitGroupEntry writes one entry's interface + impl surface (its own
// Interface, Component, Infra, Deps) into buf — the second half of genGroup,
// run once per entry after the merged $defs block.
func emitGroupEntry(buf *bytes.Buffer, e parsedEntry, modulePath string, allow map[string]bool, resolver map[string]contractRef) error {
	iface, haveIface, err := decodeAndEmitInterface(buf, &e.doc)
	if err != nil {
		return fmt.Errorf("modelgen %s: %w", e.key, err)
	}
	if haveIface {
		if err := emitImplSurface(buf, iface, e.meta, modulePath, allow, resolver); err != nil {
			return fmt.Errorf("modelgen %s: %w", e.key, err)
		}
	}
	return nil
}

// rawDefsOf pulls one contract entry's `$defs` object out as raw JSON per def
// name, for byte-level collision comparison (defsEqual) independent of the
// typed jsonschema.Schema decode.
func rawDefsOf(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	var defs map[string]json.RawMessage
	if err := json.Unmarshal(top["$defs"], &defs); err != nil {
		return nil, fmt.Errorf("parse $defs: %w", err)
	}
	return defs, nil
}

// defsEqual reports whether two raw $def JSON documents describe the same
// shape, ignoring key order and whitespace: both are unmarshaled into `any`
// and re-marshaled (encoding/json sorts map keys on marshal), then compared as
// strings.
func defsEqual(a, b json.RawMessage) (bool, error) {
	ca, err := canonicalJSON(a)
	if err != nil {
		return false, err
	}
	cb, err := canonicalJSON(b)
	if err != nil {
		return false, err
	}
	return ca == cb, nil
}

// canonicalJSON re-marshals raw JSON through `any` so object key order and
// whitespace differences do not affect comparison.
func canonicalJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(b), nil
}
