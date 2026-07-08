package modelgen

import (
	"bytes"
	"encoding/json"
)

// propOrder recovers per-def property order from the raw schema JSON.
func propOrder(raw []byte) map[string][]string {
	out := map[string][]string{}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return out
	}
	var defs map[string]json.RawMessage
	if json.Unmarshal(top["$defs"], &defs) != nil {
		return out
	}
	for name, defRaw := range defs {
		var def map[string]json.RawMessage
		if json.Unmarshal(defRaw, &def) != nil {
			continue
		}
		if props, ok := def["properties"]; ok {
			out[name] = orderedKeys(props)
		}
	}
	return out
}

// orderedKeys returns the keys of a JSON object in document order.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening '{'
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}
