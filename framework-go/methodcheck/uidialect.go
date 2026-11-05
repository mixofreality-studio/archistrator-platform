package methodcheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The browser scenario dialect (the-method-testing §1): the one browser action a UI
// scenario step performs, and what the step observes after it. A step states what a
// user does and sees: check/uncheck a box (Playwright's check() fails unless the box
// flips on the click itself), double-click, hover, press a key on a focused element.
//
// This is the ONE list of supported kinds. testgen emits exactly these (its emitter
// table is gated against UIActionKinds/UIObservableKinds), the derived browser
// contract's schema enumerates exactly these (gated in the app's projectstate), and
// the TP rules below read them. Nothing else spells the set.

// UI action kinds.
const (
	UIActionCheck    = "check"
	UIActionClick    = "click"
	UIActionDblclick = "dblclick"
	UIActionFill     = "fill"
	UIActionHover    = "hover"
	UIActionNavigate = "navigate"
	UIActionPress    = "press"
	UIActionUncheck  = "uncheck"
)

// UI observable kinds: the one field a UIObservable carries.
const (
	UIObservableChecked   = "checked"
	UIObservableText      = "text"
	UIObservableUnchecked = "unchecked"
	UIObservableURL       = "url"
	UIObservableVisible   = "visible"
)

// uiActionField is how one action kind uses a field.
type uiActionField int

const (
	fieldNone     uiActionField = iota // the kind takes no such field
	fieldOptional                      // fill's value: "" clears the field
	fieldRequired
)

// uiActionFields is, per action kind, how it uses target and value. Every kind but
// navigate acts on the element carrying data-testid target (press too: a key press
// with no focused element is not a user action); fill types value into it, press
// presses key value on it, navigate goes to value.
var uiActionFields = map[string]struct{ target, value uiActionField }{
	UIActionCheck:    {target: fieldRequired},
	UIActionClick:    {target: fieldRequired},
	UIActionDblclick: {target: fieldRequired},
	UIActionFill:     {target: fieldRequired, value: fieldOptional},
	UIActionHover:    {target: fieldRequired},
	UIActionNavigate: {value: fieldRequired},
	UIActionPress:    {target: fieldRequired, value: fieldRequired},
	UIActionUncheck:  {target: fieldRequired},
}

// UIActionKinds is the sorted set of browser action kinds.
func UIActionKinds() []string {
	return slices.Sorted(func(yield func(string) bool) {
		for k := range uiActionFields {
			if !yield(k) {
				return
			}
		}
	})
}

// UIObservableKinds is the sorted set of browser observable kinds.
func UIObservableKinds() []string {
	return []string{UIObservableChecked, UIObservableText, UIObservableUnchecked, UIObservableURL, UIObservableVisible}
}

// UIAction is one browser action: {kind, target, value}.
type UIAction struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Value  string `json:"value,omitempty"`
}

// ParseUIAction decodes and validates one action: a known kind carrying exactly the
// fields that kind requires. An unknown field, kind or a missing field is an error.
func ParseUIAction(raw string) (UIAction, error) {
	var a UIAction
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return UIAction{}, fmt.Errorf("not a UI action {kind, target, value}: %w", err)
	}
	f, ok := uiActionFields[a.Kind]
	if !ok {
		return UIAction{}, fmt.Errorf("unknown UI action kind %q; the dialect has %s", a.Kind, strings.Join(UIActionKinds(), ", "))
	}
	if err := fieldUse("target", f.target, a.Target, a.Kind); err != nil {
		return UIAction{}, err
	}
	if err := fieldUse("value", f.value, a.Value, a.Kind); err != nil {
		return UIAction{}, err
	}
	return a, nil
}

// fieldUse checks one action field against how its kind uses it.
func fieldUse(name string, use uiActionField, v, kind string) error {
	switch {
	case use == fieldRequired && v == "":
		meaning, ok := uiFieldMeaning[kind+"."+name]
		if !ok {
			meaning = "the data-testid of the element it acts on"
		}
		return fmt.Errorf("a %q action needs %s (%s)", kind, name, meaning)
	case use == fieldNone && v != "":
		return fmt.Errorf("a %q action takes no %s", kind, name)
	}
	return nil
}

var uiFieldMeaning = map[string]string{
	"navigate.value": "the route it goes to",
	"press.value":    "the key it presses, e.g. Enter or Escape",
}

// UIObservable is one observation. It states exactly one thing: the data-testid that is
// visible, checked or unchecked; the text a data-testid (target) holds; or the URL.
// Text is a pointer because its presence, not its value, states the kind: "the field
// holds no text" ({"target": "new-todo", "text": ""}) is an observation a fresh
// project's plan writes, and the contract schema (presence-based oneOf) admits it.
type UIObservable struct {
	Visible   string  `json:"visible,omitempty"`
	Text      *string `json:"text,omitempty"`
	Target    string  `json:"target,omitempty"`
	URL       string  `json:"url,omitempty"`
	Checked   string  `json:"checked,omitempty"`
	Unchecked string  `json:"unchecked,omitempty"`
}

// Kind is the one kind the observable states ("" when it states none or several).
func (o UIObservable) Kind() string {
	var kinds []string
	if o.Text != nil {
		kinds = append(kinds, UIObservableText)
	}
	for k, v := range map[string]string{
		UIObservableVisible: o.Visible, UIObservableURL: o.URL,
		UIObservableChecked: o.Checked, UIObservableUnchecked: o.Unchecked,
	} {
		if v != "" {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) != 1 {
		return ""
	}
	return kinds[0]
}

// ParseUIObservation decodes one observable or an array of them, each stating exactly
// one thing; target goes with text and nothing else.
func ParseUIObservation(raw string) ([]UIObservable, error) {
	var many []UIObservable
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&many); err != nil {
			return nil, fmt.Errorf("not a UI observation: %w", err)
		}
	} else {
		var one UIObservable
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&one); err != nil {
			return nil, fmt.Errorf("not a UI observation: %w", err)
		}
		many = []UIObservable{one}
	}
	for i, o := range many {
		kind := o.Kind()
		if kind == "" {
			return nil, fmt.Errorf("observable %d must state exactly one of %s", i, strings.Join(UIObservableKinds(), ", "))
		}
		if (kind == UIObservableText) != (o.Target != "") {
			return nil, fmt.Errorf("observable %d: target goes with text (the data-testid that holds it) and nothing else", i)
		}
	}
	return many, nil
}
