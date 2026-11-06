package methodcheck

import (
	"encoding/json"
	"fmt"
	"slices"
)

// The semantic UI dialect (FPC plan 2, Task 2.5): what a user does in a browser and
// what they see afterwards, as sum types over UI-design elements.
//
// A step never names a selector. Every target is the id of an element in the surface's
// UIDesignRecord (uidesign.go); the locator (role, accessible name, containing element)
// is derived from that design, so the test finds the control the way a user does and
// the design is the one place a control's role and name are written down.
//
// UIAction and UIObservable are sealed interfaces over one struct per kind, written in
// the wire envelope {"kind": "<variant>", "model": {...}} (bindings.go). They decode
// strictly: an unknown kind, an unknown field and a missing required field are errors.
// The observation of a step is an array of UIObservable; an empty array asserts nothing
// and is refused.

// UIRole is a WAI-ARIA role a design element can carry. The set is closed (UIRoles).
type UIRole string

// The closed role set.
const (
	UIRoleButton     UIRole = "button"
	UIRoleCheckbox   UIRole = "checkbox"
	UIRoleTextbox    UIRole = "textbox"
	UIRoleListitem   UIRole = "listitem"
	UIRoleList       UIRole = "list"
	UIRoleLink       UIRole = "link"
	UIRoleHeading    UIRole = "heading"
	UIRoleStatus     UIRole = "status"
	UIRoleDialog     UIRole = "dialog"
	UIRoleNavigation UIRole = "navigation"
	UIRoleMain       UIRole = "main"
	UIRoleForm       UIRole = "form"
	UIRoleOption     UIRole = "option"
	UIRoleCombobox   UIRole = "combobox"
	UIRoleSwitch     UIRole = "switch"
	UIRoleTab        UIRole = "tab"
	UIRoleTabpanel   UIRole = "tabpanel"
	UIRoleTable      UIRole = "table"
	UIRoleRow        UIRole = "row"
	UIRoleCell       UIRole = "cell"
	UIRoleImg        UIRole = "img"
)

// UIRoles is the closed role set, in declaration order.
func UIRoles() []UIRole {
	return []UIRole{
		UIRoleButton, UIRoleCheckbox, UIRoleTextbox, UIRoleListitem, UIRoleList, UIRoleLink,
		UIRoleHeading, UIRoleStatus, UIRoleDialog, UIRoleNavigation, UIRoleMain, UIRoleForm,
		UIRoleOption, UIRoleCombobox, UIRoleSwitch, UIRoleTab, UIRoleTabpanel, UIRoleTable,
		UIRoleRow, UIRoleCell, UIRoleImg,
	}
}

// UITarget names the design element an action acts on or an observable observes. Nth
// picks one of a repeating element's instances (zero-based); absent means the element
// is unique or the first.
type UITarget struct {
	Element string `json:"element"`
	Nth     *int   `json:"nth,omitempty"`
}

func (t UITarget) validate() error {
	if t.Element == "" {
		return fmt.Errorf("a target needs an element (the id of a UI-design element)")
	}
	if t.Nth != nil && *t.Nth < 0 {
		return fmt.Errorf("target %q: nth must not be negative", t.Element)
	}
	return nil
}

// ---- UIAction ----

// UIActionKind discriminates UIAction variants.
type UIActionKind string

// The UIAction variants.
const (
	UIActionNavigate UIActionKind = "navigate"
	UIActionClick    UIActionKind = "click"
	UIActionDblclick UIActionKind = "dblclick"
	UIActionFill     UIActionKind = "fill"
	UIActionPress    UIActionKind = "press"
	UIActionCheck    UIActionKind = "check"
	UIActionUncheck  UIActionKind = "uncheck"
	UIActionHover    UIActionKind = "hover"
	UIActionSelect   UIActionKind = "select"
)

// UIAction is the one browser action a step performs.
//
//sumtype:decl
type UIAction interface {
	Kind() UIActionKind
	isUIAction()
}

// UINavigate goes to a screen route of the design.
type UINavigate struct {
	Route string `json:"route"`
}

// UIClick clicks an element.
type UIClick struct {
	Target UITarget `json:"target"`
}

// UIDblclick double-clicks an element.
type UIDblclick struct {
	Target UITarget `json:"target"`
}

// UIFill types Value into a text element; "" clears it.
type UIFill struct {
	Target UITarget `json:"target"`
	Value  string   `json:"value"`
}

// UIPress presses Key on an element.
type UIPress struct {
	Target UITarget `json:"target"`
	Key    string   `json:"key"`
}

// UICheck ticks a checkbox or switch.
type UICheck struct {
	Target UITarget `json:"target"`
}

// UIUncheck clears a checkbox or switch.
type UIUncheck struct {
	Target UITarget `json:"target"`
}

// UIHover hovers an element.
type UIHover struct {
	Target UITarget `json:"target"`
}

// UISelect picks Option in a combobox.
type UISelect struct {
	Target UITarget `json:"target"`
	Option string   `json:"option"`
}

// Kind implements UIAction.
func (UINavigate) Kind() UIActionKind { return UIActionNavigate }

// Kind implements UIAction.
func (UIClick) Kind() UIActionKind { return UIActionClick }

// Kind implements UIAction.
func (UIDblclick) Kind() UIActionKind { return UIActionDblclick }

// Kind implements UIAction.
func (UIFill) Kind() UIActionKind { return UIActionFill }

// Kind implements UIAction.
func (UIPress) Kind() UIActionKind { return UIActionPress }

// Kind implements UIAction.
func (UICheck) Kind() UIActionKind { return UIActionCheck }

// Kind implements UIAction.
func (UIUncheck) Kind() UIActionKind { return UIActionUncheck }

// Kind implements UIAction.
func (UIHover) Kind() UIActionKind { return UIActionHover }

// Kind implements UIAction.
func (UISelect) Kind() UIActionKind { return UIActionSelect }

func (UINavigate) isUIAction() {}
func (UIClick) isUIAction()    {}
func (UIDblclick) isUIAction() {}
func (UIFill) isUIAction()     {}
func (UIPress) isUIAction()    {}
func (UICheck) isUIAction()    {}
func (UIUncheck) isUIAction()  {}
func (UIHover) isUIAction()    {}
func (UISelect) isUIAction()   {}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UINavigate) MarshalJSON() ([]byte, error) {
	type w UINavigate
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIClick) MarshalJSON() ([]byte, error) {
	type w UIClick
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIDblclick) MarshalJSON() ([]byte, error) {
	type w UIDblclick
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIFill) MarshalJSON() ([]byte, error) { type w UIFill; return marshalEnvelope(v.Kind(), w(v)) }

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIPress) MarshalJSON() ([]byte, error) {
	type w UIPress
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UICheck) MarshalJSON() ([]byte, error) {
	type w UICheck
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIUncheck) MarshalJSON() ([]byte, error) {
	type w UIUncheck
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIHover) MarshalJSON() ([]byte, error) {
	type w UIHover
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UISelect) MarshalJSON() ([]byte, error) {
	type w UISelect
	return marshalEnvelope(v.Kind(), w(v))
}

// UIActionKinds is the sorted set of browser action kinds.
func UIActionKinds() []string {
	return []string{
		string(UIActionCheck), string(UIActionClick), string(UIActionDblclick), string(UIActionFill),
		string(UIActionHover), string(UIActionNavigate), string(UIActionPress), string(UIActionSelect),
		string(UIActionUncheck),
	}
}

// UnmarshalUIAction decodes the {"kind","model"} envelope into a UIAction; an unknown
// kind, an unknown field, or a missing required field is an error.
func UnmarshalUIAction(raw []byte) (UIAction, error) {
	kind, model, err := openEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("not a UI action: %w", err)
	}
	a, err := decodeUIAction(UIActionKind(kind), model)
	if err != nil {
		return nil, fmt.Errorf("UI action %q: %w", kind, err)
	}
	return a, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ParseUIAction decodes one action from its JSON text.
func ParseUIAction(raw string) (UIAction, error) { return UnmarshalUIAction([]byte(raw)) }

// ActionRoles is the roles an element may have for the action kind to be performable on
// it; nil means any role. check and uncheck act on a checkbox or switch, fill on a
// textbox or combobox, select on a combobox.
func ActionRoles(k UIActionKind) []UIRole {
	switch k {
	case UIActionCheck, UIActionUncheck:
		return []UIRole{UIRoleCheckbox, UIRoleSwitch}
	case UIActionFill:
		return []UIRole{UIRoleTextbox, UIRoleCombobox}
	case UIActionSelect:
		return []UIRole{UIRoleCombobox}
	case UIActionPress, UIActionClick, UIActionDblclick, UIActionHover, UIActionNavigate:
		return nil
	}
	return nil
}

// LayerAObservables is what a user sees the instant the action lands, before any server
// round trip: a checked box, a cleared box, the text just typed or picked. The generated
// test asserts these immediately, under injected latency. Actions that change no
// element state in place return nil.
func LayerAObservables(a UIAction) []UIObservable {
	switch v := a.(type) {
	case UICheck:
		return []UIObservable{UIChecked(v)}
	case UIUncheck:
		return []UIObservable{UIUnchecked(v)}
	case UIFill:
		return []UIObservable{UIValue(v)}
	case UISelect:
		return []UIObservable{UIValue{Target: v.Target, Value: v.Option}}
	case UINavigate, UIClick, UIDblclick, UIPress, UIHover:
		return nil
	}
	return nil
}

// ---- UIObservable ----

// UIObservableKind discriminates UIObservable variants.
type UIObservableKind string

// The UIObservable variants.
const (
	UIObservableVisible   UIObservableKind = "visible"
	UIObservableHidden    UIObservableKind = "hidden"
	UIObservableAbsent    UIObservableKind = "absent"
	UIObservableText      UIObservableKind = "text"
	UIObservableValue     UIObservableKind = "value"
	UIObservableChecked   UIObservableKind = "checked"
	UIObservableUnchecked UIObservableKind = "unchecked"
	UIObservableEnabled   UIObservableKind = "enabled"
	UIObservableDisabled  UIObservableKind = "disabled"
	UIObservableFocused   UIObservableKind = "focused"
	UIObservableCount     UIObservableKind = "count"
	UIObservableURL       UIObservableKind = "url"
)

// UIObservable is one thing a user sees.
//
//sumtype:decl
type UIObservable interface {
	Kind() UIObservableKind
	isUIObservable()
}

// UIVisible: the element is shown.
type UIVisible struct {
	Target UITarget `json:"target"`
}

// UIHidden: the element exists and is not shown.
type UIHidden struct {
	Target UITarget `json:"target"`
}

// UIAbsent: the element is not in the page.
type UIAbsent struct {
	Target UITarget `json:"target"`
}

// UIText: the element's text. "" asserts the element holds no text.
type UIText struct {
	Target UITarget `json:"target"`
	Text   string   `json:"text"`
}

// UIValue: a field's value. "" asserts the field is empty.
type UIValue struct {
	Target UITarget `json:"target"`
	Value  string   `json:"value"`
}

// UIChecked: the checkbox or switch is on.
type UIChecked struct {
	Target UITarget `json:"target"`
}

// UIUnchecked: the checkbox or switch is off.
type UIUnchecked struct {
	Target UITarget `json:"target"`
}

// UIEnabled: the element accepts input.
type UIEnabled struct {
	Target UITarget `json:"target"`
}

// UIDisabled: the element does not accept input.
type UIDisabled struct {
	Target UITarget `json:"target"`
}

// UIFocused: the element holds keyboard focus.
type UIFocused struct {
	Target UITarget `json:"target"`
}

// UICount: N instances of a repeating element are shown.
type UICount struct {
	Target UITarget `json:"target"`
	N      int      `json:"n"`
}

// UIURL: the page is at a screen route of the design.
type UIURL struct {
	Route string `json:"route"`
}

// Kind implements UIObservable.
func (UIVisible) Kind() UIObservableKind { return UIObservableVisible }

// Kind implements UIObservable.
func (UIHidden) Kind() UIObservableKind { return UIObservableHidden }

// Kind implements UIObservable.
func (UIAbsent) Kind() UIObservableKind { return UIObservableAbsent }

// Kind implements UIObservable.
func (UIText) Kind() UIObservableKind { return UIObservableText }

// Kind implements UIObservable.
func (UIValue) Kind() UIObservableKind { return UIObservableValue }

// Kind implements UIObservable.
func (UIChecked) Kind() UIObservableKind { return UIObservableChecked }

// Kind implements UIObservable.
func (UIUnchecked) Kind() UIObservableKind { return UIObservableUnchecked }

// Kind implements UIObservable.
func (UIEnabled) Kind() UIObservableKind { return UIObservableEnabled }

// Kind implements UIObservable.
func (UIDisabled) Kind() UIObservableKind { return UIObservableDisabled }

// Kind implements UIObservable.
func (UIFocused) Kind() UIObservableKind { return UIObservableFocused }

// Kind implements UIObservable.
func (UICount) Kind() UIObservableKind { return UIObservableCount }

// Kind implements UIObservable.
func (UIURL) Kind() UIObservableKind { return UIObservableURL }

func (UIVisible) isUIObservable()   {}
func (UIHidden) isUIObservable()    {}
func (UIAbsent) isUIObservable()    {}
func (UIText) isUIObservable()      {}
func (UIValue) isUIObservable()     {}
func (UIChecked) isUIObservable()   {}
func (UIUnchecked) isUIObservable() {}
func (UIEnabled) isUIObservable()   {}
func (UIDisabled) isUIObservable()  {}
func (UIFocused) isUIObservable()   {}
func (UICount) isUIObservable()     {}
func (UIURL) isUIObservable()       {}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIVisible) MarshalJSON() ([]byte, error) {
	type w UIVisible
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIHidden) MarshalJSON() ([]byte, error) {
	type w UIHidden
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIAbsent) MarshalJSON() ([]byte, error) {
	type w UIAbsent
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIText) MarshalJSON() ([]byte, error) { type w UIText; return marshalEnvelope(v.Kind(), w(v)) }

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIValue) MarshalJSON() ([]byte, error) {
	type w UIValue
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIChecked) MarshalJSON() ([]byte, error) {
	type w UIChecked
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIUnchecked) MarshalJSON() ([]byte, error) {
	type w UIUnchecked
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIEnabled) MarshalJSON() ([]byte, error) {
	type w UIEnabled
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIDisabled) MarshalJSON() ([]byte, error) {
	type w UIDisabled
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIFocused) MarshalJSON() ([]byte, error) {
	type w UIFocused
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UICount) MarshalJSON() ([]byte, error) {
	type w UICount
	return marshalEnvelope(v.Kind(), w(v))
}

// MarshalJSON writes the {"kind","model"} envelope.
func (v UIURL) MarshalJSON() ([]byte, error) { type w UIURL; return marshalEnvelope(v.Kind(), w(v)) }

// UIObservableKinds is the sorted set of browser observable kinds.
func UIObservableKinds() []string {
	return []string{
		string(UIObservableAbsent), string(UIObservableChecked), string(UIObservableCount),
		string(UIObservableDisabled), string(UIObservableEnabled), string(UIObservableFocused),
		string(UIObservableHidden), string(UIObservableText), string(UIObservableUnchecked),
		string(UIObservableURL), string(UIObservableValue), string(UIObservableVisible),
	}
}

// UnmarshalUIObservable decodes the {"kind","model"} envelope into a UIObservable.
func UnmarshalUIObservable(raw []byte) (UIObservable, error) {
	kind, model, err := openEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("not a UI observable: %w", err)
	}
	o, err := decodeUIObservable(UIObservableKind(kind), model)
	if err != nil {
		return nil, fmt.Errorf("UI observable %q: %w", kind, err)
	}
	return o, nil
}

// ParseUIObservation decodes a step's observation: a JSON array of UIObservable
// envelopes. An empty array is refused: an observation that asserts nothing.
func ParseUIObservation(raw string) ([]UIObservable, error) {
	var items []json.RawMessage
	if err := strictUnmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("not a UI observation (an array of {kind, model} observables): %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("observation asserts nothing: it needs at least one observable")
	}
	out := make([]UIObservable, 0, len(items))
	for i, it := range items {
		o, err := UnmarshalUIObservable(it)
		if err != nil {
			return nil, fmt.Errorf("observable %d: %w", i, err)
		}
		out = append(out, o)
	}
	return out, nil
}

// hasRole reports whether role is in roles; nil roles admit any role.
func hasRole(roles []UIRole, role UIRole) bool {
	return roles == nil || slices.Contains(roles, role)
}
