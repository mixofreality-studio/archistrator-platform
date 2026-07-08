package modelgen

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// emitImplSurface emits the concrete impl struct + public DI constructor that make
// the generated contract the ONLY public surface of the component's package.
// Emitted for the contracts that have been re-homed onto the generated struct: an
// RA with a non-empty `infra` list (one <Infra><Component> struct +
// New<Infra><Component>(client) constructor per infra), and an engine on the
// engine-impl allowlist (a pure <Component>Impl + New<Component>()). The
// hand-written interface methods hang off the generated struct; modelgen emits only
// the struct + constructor + a compile-time interface assertion.
func emitImplSurface(buf *bytes.Buffer, iface Interface, meta contractMeta, modulePath string, allow map[string]bool, resolver map[string]contractRef) error {
	switch {
	case meta.Stub:
		// STUB: the component is contracted (goPackage + $defs + interface) but
		// not yet built. Emit the fully-generated not-implemented impl (unexported
		// impl struct + no-arg public constructor + every method returning the
		// layer's not-implemented error) so the package compiles; as it is built
		// later these bodies are replaced and the stub flag cleared.
		emitStubImpl(buf, iface)
	case iface.Layer == "resourceaccess":
		return emitRAImplIfInfra(buf, iface, meta.Infra)
	case iface.Layer == "engine":
		if allow[meta.Component] {
			emitEngineImpl(buf, iface)
		}
	case iface.Layer == "manager":
		return emitManagerCtorIfDeps(buf, iface, meta.Deps, resolver, modulePath)
	}
	return nil
}

// emitRAImplIfInfra emits the RA impl only when the ResourceAccess declares one
// or more infra bindings (an RA with no infra keeps only its generated interface).
func emitRAImplIfInfra(buf *bytes.Buffer, iface Interface, infra []string) error {
	if len(infra) == 0 {
		return nil
	}
	if err := emitRAImpl(buf, iface, infra); err != nil {
		return fmt.Errorf("emit RA impl: %w", err)
	}
	return nil
}

// emitManagerCtorIfDeps emits a manager's generated DI constructor only when it
// declares `deps`. A MANAGER with declared `deps` gains New<Iface>(deps…) <Iface>
// delegating to the hand-written, unexported builder new<Iface>(deps…). The impl
// struct, the builder, and the interface methods are hand-written + unexported
// (managers carry real state — Temporal client, deps, config), so only the
// generated interface + models + constructor are public. Managers without deps
// (un-migrated) emit no constructor.
func emitManagerCtorIfDeps(buf *bytes.Buffer, iface Interface, deps []contractDep, resolver map[string]contractRef, modulePath string) error {
	if len(deps) == 0 {
		return nil
	}
	if err := emitManagerConstructor(buf, iface, deps, resolver, modulePath); err != nil {
		return fmt.Errorf("emit manager constructor: %w", err)
	}
	return nil
}

// emitRAImpl writes, for each infra the ResourceAccess uses, a concrete
// <Infra><Component> struct holding that infra's framework client field(s) + a
// public DI constructor New<Infra><Component>(client...) returning the generated
// interface, plus a compile-time assertion. The interface methods are hand-written
// on the returned struct (re-homed from the prior hand-written impl).
func emitRAImpl(buf *bytes.Buffer, iface Interface, infra []string) error {
	for _, name := range infra {
		binding, ok := infraBindings[name]
		if !ok {
			return fmt.Errorf("unapproved/unknown infra %q (no framework binding)", name)
		}
		for _, f := range binding.params {
			for path, alias := range f.imports {
				pendingImports[path] = alias
			}
		}
		if binding.delegated {
			emitDelegatingConstructor(buf, iface, name, binding)
			continue
		}
		emitArtifactImpl(buf, iface, name, binding)
	}
	return nil
}

// emitArtifactImpl writes the ARTIFACT-mode (Stage-1 thin wrapper) surface: a
// generated exported <Infra><Component> struct holding the framework client
// field(s), a public New<Infra><Component>(client...) constructor returning the
// generated interface, and a compile-time assertion.
func emitArtifactImpl(buf *bytes.Buffer, iface Interface, name string, binding infraBinding) {
	struct_ := name + iface.Name
	fmt.Fprintf(buf, "// %s is the generated %s-backed implementation of %s. Its fields\n", struct_, name, iface.Name)
	fmt.Fprintf(buf, "// are the framework infrastructure client(s) the resource access is built on;\n")
	fmt.Fprintf(buf, "// the interface methods are hand-written on this struct.\n")
	fmt.Fprintf(buf, "type %s struct {\n", struct_)
	for _, f := range binding.params {
		fmt.Fprintf(buf, "\t%s %s\n", f.name, f.typ)
	}
	buf.WriteString("}\n\n")

	params := make([]string, 0, len(binding.params))
	inits := make([]string, 0, len(binding.params))
	for _, f := range binding.params {
		params = append(params, f.name+" "+f.typ)
		inits = append(inits, f.name+": "+f.name)
	}
	fmt.Fprintf(buf, "// New%s constructs a %s-backed %s over the supplied framework\n", struct_, name, iface.Name)
	fmt.Fprintf(buf, "// infrastructure client(s).\n")
	fmt.Fprintf(buf, "func New%s(%s) %s {\n", struct_, strings.Join(params, ", "), iface.Name)
	fmt.Fprintf(buf, "\treturn &%s{%s}\n", struct_, strings.Join(inits, ", "))
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "var _ %s = (*%s)(nil)\n\n", iface.Name, struct_)
}

// emitDelegatingConstructor writes the OPTION-1 (delegated) public constructor for a
// stateful RA: New<Infra><Component>(params) (<Iface>[, error]) whose body delegates
// to the hand-written, unexported builder new<Infra><Component>(args). NOTHING else
// is generated for this infra — the impl struct, the builder, and the interface
// methods are hand-written + unexported in the RA package, so the concrete impl and
// its package-local state stay unexported (only the generated interface + models +
// this constructor are public). No compile-time assertion is emitted here: the
// hand-written builder returns the interface, which IS the compile-time proof.
func emitDelegatingConstructor(buf *bytes.Buffer, iface Interface, name string, binding infraBinding) {
	ctor := "New" + name + iface.Name
	builder := "new" + name + iface.Name

	params := make([]string, 0, len(binding.params))
	args := make([]string, 0, len(binding.params))
	for _, f := range binding.params {
		params = append(params, f.name+" "+f.typ)
		args = append(args, f.name)
	}
	ret := iface.Name
	if binding.returnsError {
		ret = "(" + iface.Name + ", error)"
	}

	fmt.Fprintf(buf, "// %s constructs the %s-backed %s, delegating to the hand-written,\n", ctor, name, iface.Name)
	fmt.Fprintf(buf, "// unexported builder %s in the RA package (which owns the stateful setup).\n", builder)
	fmt.Fprintf(buf, "// The constructor returns the interface, so the concrete impl stays unexported.\n")
	fmt.Fprintf(buf, "func %s(%s) %s {\n", ctor, strings.Join(params, ", "), ret)
	fmt.Fprintf(buf, "\treturn %s(%s)\n", builder, strings.Join(args, ", "))
	buf.WriteString("}\n\n")
}

// emitEngineImpl writes the pure-engine impl: an empty <Component>Impl struct (no
// dependencies — engines are pure), a public DI constructor New<Component>()
// returning the generated interface, and a compile-time assertion. The interface
// methods are hand-written on this struct.
func emitEngineImpl(buf *bytes.Buffer, iface Interface) {
	struct_ := iface.Name + "Impl"
	fmt.Fprintf(buf, "// %s is the generated concrete %s. Engines are pure (no\n", struct_, iface.Name)
	fmt.Fprintf(buf, "// dependencies), so the impl carries no fields and the constructor takes none.\n")
	fmt.Fprintf(buf, "// The interface methods are hand-written on this struct.\n")
	fmt.Fprintf(buf, "type %s struct{}\n\n", struct_)
	fmt.Fprintf(buf, "// New%s returns the production %s.\n", iface.Name, iface.Name)
	fmt.Fprintf(buf, "func New%s() %s { return %s{} }\n\n", iface.Name, iface.Name, struct_)
	fmt.Fprintf(buf, "var _ %s = %s{}\n", iface.Name, struct_)
}

// contractRef is a dependency contract's published-interface coordinates: the Go
// package it lives in (goPackage, relative to the server module root) and the
// generated interface's name. Built once from .serviceContracts for every contract
// key, it lets a manager's `deps` resolve a dependency component → a typed
// constructor parameter.
type contractRef struct {
	goPackage string
	iface     string
}

// contractDep is one manager constructor dependency, declared in project.json under
// `.serviceContracts.<mgr>.deps`. Two kinds:
//
//   - COMPONENT dep ({name, component}): `component` is the dependency's contract
//     key; modelgen resolves it (via the resolver) to its goPackage + interface name
//     and emits a param `name <pkg-base>.<Iface>`, importing the dep's package. This
//     is the founder model — managers depend on each dependency's GENERATED/published
//     interface, never a hand-written consumer mirror.
//   - PLAIN dep ({name, goType[, goImport]}): a constructor param the generator can
//     NOT derive from a component — a Temporal client, a config scalar (repoBase), a
//     resolver func. `goType` is emitted verbatim; `goImport` (optional) is the single
//     import path that type needs.
type contractDep struct {
	Name      string `json:"name"`
	Component string `json:"component,omitempty"`
	GoType    string `json:"goType,omitempty"`
	GoImport  string `json:"goImport,omitempty"`
}

// emitManagerConstructor writes a manager's generated public DI constructor
// New<Iface>(deps…) <Iface> delegating to the hand-written, unexported builder
// new<Iface>(deps…). Each dep becomes one ordered constructor parameter: a COMPONENT
// dep resolves to its published interface type (<pkg-base>.<Iface>, with the dep
// package imported); a PLAIN dep emits its goType verbatim (with its optional import).
// No struct, no assertion: the hand-written builder returns the interface, which IS
// the compile-time proof (exactly the option-1 delegated-RA pattern).
func emitManagerConstructor(buf *bytes.Buffer, iface Interface, deps []contractDep, resolver map[string]contractRef, modulePath string) error {
	ctor := "New" + iface.Name
	builder := "new" + iface.Name

	params := make([]string, 0, len(deps))
	args := make([]string, 0, len(deps))
	for _, d := range deps {
		typ, err := depType(d, resolver, modulePath)
		if err != nil {
			return err
		}
		params = append(params, d.Name+" "+typ)
		args = append(args, d.Name)
	}

	fmt.Fprintf(buf, "// %s constructs the %s, delegating to the hand-written, unexported\n", ctor, iface.Name)
	fmt.Fprintf(buf, "// builder %s in the manager package (which owns the stateful facade setup:\n", builder)
	fmt.Fprintf(buf, "// the Temporal client, the deps, and config). The constructor returns the\n")
	fmt.Fprintf(buf, "// interface, so the concrete manager impl stays unexported.\n")
	fmt.Fprintf(buf, "func %s(%s) %s {\n", ctor, strings.Join(params, ", "), iface.Name)
	fmt.Fprintf(buf, "\treturn %s(%s)\n", builder, strings.Join(args, ", "))
	buf.WriteString("}\n\n")
	return nil
}

// depType resolves one manager dependency to its constructor-parameter Go type,
// registering any import it needs. A COMPONENT dep resolves through the resolver to
// its published interface type; a PLAIN dep emits its goType verbatim.
func depType(d contractDep, resolver map[string]contractRef, modulePath string) (string, error) {
	switch {
	case d.Component != "":
		ref, ok := resolver[d.Component]
		if !ok {
			return "", fmt.Errorf("dep %q: unknown component %q", d.Name, d.Component)
		}
		if ref.goPackage == "" || ref.iface == "" {
			return "", fmt.Errorf("dep %q: component %q has no goPackage/interface (unbuilt?)", d.Name, d.Component)
		}
		pkg := filepath.Base(ref.goPackage)
		pendingImports[modulePath+"/"+ref.goPackage] = ""
		return pkg + "." + ref.iface, nil
	case d.GoType != "":
		if d.GoImport != "" {
			pendingImports[d.GoImport] = ""
		}
		return d.GoType, nil
	default:
		return "", fmt.Errorf("dep %q: neither component nor goType set", d.Name)
	}
}

// emitStubImpl writes the generated not-implemented implementation for a STUB
// contract (one flagged `"stub": true` in project.json): an unexported impl struct,
// a no-arg public constructor New<Iface>() <Iface> returning the interface, a
// compile-time assertion, and every interface method returning the layer's
// not-implemented error. A stub has no infra yet, so the constructor takes no
// arguments; only the generated interface + models + this constructor are public.
// As the component is built later these generated bodies are replaced.
func emitStubImpl(buf *bytes.Buffer, iface Interface) {
	lc, hasLayer := layerContext[iface.Layer]
	struct_ := "stub" + iface.Name

	fmt.Fprintf(buf, "// %s is the generated not-implemented %s. The component is contracted\n", struct_, iface.Name)
	fmt.Fprintf(buf, "// but not yet built, so every method returns a not-implemented error; the bodies\n")
	fmt.Fprintf(buf, "// are replaced when the component is constructed.\n")
	fmt.Fprintf(buf, "type %s struct{}\n\n", struct_)

	fmt.Fprintf(buf, "// New%s returns the not-implemented %s. It takes no arguments\n", iface.Name, iface.Name)
	fmt.Fprintf(buf, "// (the component has no infrastructure binding yet).\n")
	fmt.Fprintf(buf, "func New%s() %s { return &%s{} }\n\n", iface.Name, iface.Name, struct_)

	fmt.Fprintf(buf, "var _ %s = (*%s)(nil)\n\n", iface.Name, struct_)

	for _, op := range iface.Operations {
		emitStubMethod(buf, struct_, iface.Layer, lc, hasLayer, op)
	}
}

// emitStubMethod writes one stub method: the signature (with all params blanked)
// and the body returning the layer's not-implemented error / zero values.
func emitStubMethod(buf *bytes.Buffer, struct_, layer string, lc struct{ alias, path, typ string }, hasLayer bool, op Operation) {
	params := make([]string, 0, len(op.Params)+1)
	if hasLayer {
		params = append(params, "_ "+lc.typ)
		pendingImports[lc.path] = lc.alias
	}
	for _, p := range op.Params {
		params = append(params, "_ "+paramType(p))
	}
	fmt.Fprintf(buf, "func (*%s) %s(%s)%s {\n", struct_, op.Name, strings.Join(params, ", "), returnClause(op))
	switch {
	case op.Result != nil && op.Error:
		fmt.Fprintf(buf, "\tvar zero %s\n", goType(op.Result))
		fmt.Fprintf(buf, "\treturn zero, %s\n", notImplementedExpr(layer))
	case op.Error:
		fmt.Fprintf(buf, "\treturn %s\n", notImplementedExpr(layer))
	case op.Result != nil:
		fmt.Fprintf(buf, "\tvar zero %s\n\treturn zero\n", goType(op.Result))
	}
	buf.WriteString("}\n\n")
}

// notImplementedExpr renders the layer-appropriate not-implemented error
// expression for a stub method body. ResourceAccess stubs use the framework RA
// error (fwra.New); the fwra import is already present (every RA method takes the
// rc fwra.Context). Other layers fall back to a plain errors.New so modelgen does
// not have to guess each framework's error API (no non-RA stubs exist today).
func notImplementedExpr(layer string) string {
	if layer == "resourceaccess" {
		return `fwra.New(fwra.Unknown, "not implemented")`
	}
	pendingImports["errors"] = ""
	return `errors.New("not implemented")`
}
