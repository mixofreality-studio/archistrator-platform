package methodcheck

import "slices"

// ContractsOf names the contracts a component owns, sorted: the contract under key plus every
// contract whose component field is key (exact match, no normalization). None when key is "".
// components maps every contract key to its component field. A key no contract is recorded
// under is still named, so what its (empty) scenario projection shows is what a check lists.
//
// This is the ONE component-to-contracts join (FPC D7): the construction gate, the task spec and
// the state tool all read it, so the three cannot disagree on which contracts a component owns.
func ContractsOf(key string, components map[string]string) []string {
	if key == "" {
		return nil
	}
	var out []string
	for name, component := range components {
		if name == key || component == key {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return []string{key}
	}
	slices.Sort(out)
	return out
}
