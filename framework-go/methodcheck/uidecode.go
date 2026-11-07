package methodcheck

import (
	"encoding/json"
	"fmt"
	"strings"
)

// uidecode.go is the decode side of the UI dialect: one strict decoder per variant, a
// self-check (validate) per variant for what the schema's `required` cannot say
// (non-empty strings, a non-negative count), and the target accessor the rules use.

// decodeVariant strictly decodes an envelope's model into the variant T and runs its
// self-check.
func decodeVariant[T interface{ validate() error }](model json.RawMessage) (T, error) {
	var v T
	if err := strictUnmarshal(model, &v); err != nil {
		return v, err
	}
	return v, v.validate()
}

func actionDecoder[T interface {
	UIAction
	validate() error
}]() func(json.RawMessage) (UIAction, error) {
	return func(model json.RawMessage) (UIAction, error) { return decodeVariant[T](model) }
}

func observableDecoder[T interface {
	UIObservable
	validate() error
}]() func(json.RawMessage) (UIObservable, error) {
	return func(model json.RawMessage) (UIObservable, error) { return decodeVariant[T](model) }
}

// countDecoder refuses a count whose n is absent: an absent n would decode as 0 and
// assert "zero rows" by omission rather than by statement.
func countDecoder() func(json.RawMessage) (UIObservable, error) {
	return func(model json.RawMessage) (UIObservable, error) {
		var keys map[string]json.RawMessage
		if json.Unmarshal(model, &keys) == nil {
			if _, ok := keys["n"]; !ok {
				return nil, needs(false, `"n"`)
			}
		}
		return decodeVariant[UICount](model)
	}
}

var uiActionDecoders = map[UIActionKind]func(json.RawMessage) (UIAction, error){
	UIActionNavigate: actionDecoder[UINavigate](),
	UIActionClick:    actionDecoder[UIClick](),
	UIActionDblclick: actionDecoder[UIDblclick](),
	UIActionFill:     actionDecoder[UIFill](),
	UIActionPress:    actionDecoder[UIPress](),
	UIActionCheck:    actionDecoder[UICheck](),
	UIActionUncheck:  actionDecoder[UIUncheck](),
	UIActionHover:    actionDecoder[UIHover](),
	UIActionSelect:   actionDecoder[UISelect](),
}

var uiObservableDecoders = map[UIObservableKind]func(json.RawMessage) (UIObservable, error){
	UIObservableVisible:   observableDecoder[UIVisible](),
	UIObservableHidden:    observableDecoder[UIHidden](),
	UIObservableAbsent:    observableDecoder[UIAbsent](),
	UIObservableText:      observableDecoder[UIText](),
	UIObservableValue:     observableDecoder[UIValue](),
	UIObservableChecked:   observableDecoder[UIChecked](),
	UIObservableUnchecked: observableDecoder[UIUnchecked](),
	UIObservableEnabled:   observableDecoder[UIEnabled](),
	UIObservableDisabled:  observableDecoder[UIDisabled](),
	UIObservableFocused:   observableDecoder[UIFocused](),
	UIObservableCount:     countDecoder(),
	UIObservableURL:       observableDecoder[UIURL](),
}

func decodeUIAction(kind UIActionKind, model json.RawMessage) (UIAction, error) {
	dec, ok := uiActionDecoders[kind]
	if !ok {
		return nil, fmt.Errorf("unknown UI action kind %q; the dialect has %s", kind, strings.Join(UIActionKinds(), ", "))
	}
	return dec(model)
}

func decodeUIObservable(kind UIObservableKind, model json.RawMessage) (UIObservable, error) {
	dec, ok := uiObservableDecoders[kind]
	if !ok {
		return nil, fmt.Errorf("unknown UI observable kind %q; the dialect has %s", kind, strings.Join(UIObservableKinds(), ", "))
	}
	return dec(model)
}

// ---- self-checks ----

func (v UINavigate) validate() error { return needs(v.Route != "", "a route") }
func (v UIClick) validate() error    { return v.Target.validate() }
func (v UIDblclick) validate() error { return v.Target.validate() }
func (v UIFill) validate() error     { return v.Target.validate() }
func (v UIPress) validate() error {
	return firstErr(needs(v.Key != "", "a key, e.g. Enter or Escape"), v.Target.validate())
}
func (v UICheck) validate() error   { return v.Target.validate() }
func (v UIUncheck) validate() error { return v.Target.validate() }
func (v UIHover) validate() error   { return v.Target.validate() }
func (v UISelect) validate() error {
	return firstErr(needs(v.Option != "", "an option"), v.Target.validate())
}

func (v UIVisible) validate() error   { return v.Target.validate() }
func (v UIHidden) validate() error    { return v.Target.validate() }
func (v UIAbsent) validate() error    { return v.Target.validate() }
func (v UIText) validate() error      { return v.Target.validate() }
func (v UIValue) validate() error     { return v.Target.validate() }
func (v UIChecked) validate() error   { return v.Target.validate() }
func (v UIUnchecked) validate() error { return v.Target.validate() }
func (v UIEnabled) validate() error   { return v.Target.validate() }
func (v UIDisabled) validate() error  { return v.Target.validate() }
func (v UIFocused) validate() error   { return v.Target.validate() }
func (v UICount) validate() error {
	if v.N < 0 {
		return fmt.Errorf("n must not be negative")
	}
	return v.Target.validate()
}
func (v UIURL) validate() error { return needs(v.Route != "", "a route") }

func needs(ok bool, what string) error {
	if ok {
		return nil
	}
	return fmt.Errorf("needs %s", what)
}

// ---- targets ----

// uiTarget is required by the sealed interfaces themselves, so a new variant that does
// not say whether it has a design element fails to compile: there is no fallback arm to
// fall into silently.
func (UINavigate) uiTarget() (UITarget, bool) { return UITarget{}, false }
func (UIURL) uiTarget() (UITarget, bool)      { return UITarget{}, false }

func (v UIClick) uiTarget() (UITarget, bool)     { return v.Target, true }
func (v UIDblclick) uiTarget() (UITarget, bool)  { return v.Target, true }
func (v UIFill) uiTarget() (UITarget, bool)      { return v.Target, true }
func (v UIPress) uiTarget() (UITarget, bool)     { return v.Target, true }
func (v UICheck) uiTarget() (UITarget, bool)     { return v.Target, true }
func (v UIUncheck) uiTarget() (UITarget, bool)   { return v.Target, true }
func (v UIHover) uiTarget() (UITarget, bool)     { return v.Target, true }
func (v UISelect) uiTarget() (UITarget, bool)    { return v.Target, true }
func (v UIVisible) uiTarget() (UITarget, bool)   { return v.Target, true }
func (v UIHidden) uiTarget() (UITarget, bool)    { return v.Target, true }
func (v UIAbsent) uiTarget() (UITarget, bool)    { return v.Target, true }
func (v UIText) uiTarget() (UITarget, bool)      { return v.Target, true }
func (v UIValue) uiTarget() (UITarget, bool)     { return v.Target, true }
func (v UIChecked) uiTarget() (UITarget, bool)   { return v.Target, true }
func (v UIUnchecked) uiTarget() (UITarget, bool) { return v.Target, true }
func (v UIEnabled) uiTarget() (UITarget, bool)   { return v.Target, true }
func (v UIDisabled) uiTarget() (UITarget, bool)  { return v.Target, true }
func (v UIFocused) uiTarget() (UITarget, bool)   { return v.Target, true }
func (v UICount) uiTarget() (UITarget, bool)     { return v.Target, true }

// ActionTarget is the element an action acts on (ok false for navigate).
func ActionTarget(a UIAction) (UITarget, bool) { return a.uiTarget() }

// ObservableTarget is the element an observable is about (ok false for url).
func ObservableTarget(o UIObservable) (UITarget, bool) { return o.uiTarget() }
