package methodcheck

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// project.go holds the LIGHTWEIGHT STRUCTURAL mirror of the committed
// `.aiarch/state/project.json` document. It decodes the SAME on-disk shape the
// aiarch projectstate codec writes (captured verbatim from EncodeProjectJSON), but
// carries ONLY the fields the ported rules + the alignment check read. Enums are
// decoded from their canonical camelCase wire NAMES (the shape the SPA + the git
// store persist). framework-go imports nothing from aiarch — these are independent
// structs that happen to mirror the same JSON contract.

// ---- enum string constants (mirror projectstate/enumjson.go wire names) ----

// Component kinds (the `kind` field of a System component).
const (
	kindClient         = "client"
	kindManager        = "manager"
	kindEngine         = "engine"
	kindResourceAccess = "resourceAccess"
	kindResource       = "resource"
	kindUtility        = "utility"
)

// Layer wire names (the `layer` field of a System component).
const (
	layerClient         = "client"
	layerManager        = "manager"
	layerEngine         = "engine"
	layerResourceAccess = "resourceAccess"
	layerResource       = "resource"
	layerUtility        = "utility"
)

// CallMode wire names.
const (
	modeSync        = "sync"
	modeQueued      = "queued"
	modeEventPubSub = "eventPubSub"
)

// Component build-status wire names (the optional `buildStatus` field). Absent (the
// empty string) means BUILT — a component expected to carry code.
const (
	buildStatusPlanned  = "planned"  // designed but not yet constructed; no code expected yet
	buildStatusExternal = "external" // framework-provided (Utility only); no app code of its own
)

// EdgeKind wire names.
const (
	edgeControlFlow = "controlFlow"
	edgeGuardedFlow = "guardedFlow"
)

// ActivityNodeKind wire names (only the ones the activity-diagram rules read).
const (
	nodeStart    = "start"
	nodeAction   = "action"
	nodeDecision = "decision"
	nodeMerge    = "merge"
	nodeFork     = "fork"
	nodeJoin     = "join"
)

// Classification wire names.
const classCore = "core"

// CheckStatus wire names.
const (
	checkPass   = "pass"
	checkWaived = "waived"
	checkFail   = "fail"
)

// Axis wire names.
const (
	axisSameCustomerOverTime  = "sameCustomerOverTime"
	axisAllCustomersAtOneTime = "allCustomersAtOneTime"
)

// DeliveryStyle wire names.
const (
	styleCloud = "cloud"
	styleLocal = "local"
	styleBoth  = "both"
)

// DeploymentProfile wire names.
const (
	profileCloud = "cloud"
	profileLocal = "local"
	profileTest  = "test"
)

// ReviewCommitted is the slot status ordinal that marks an architect-approved slot
// (projectstate.ReviewCommitted == iota: None=0, AwaitingReview=1, Committed=2).
// The committed JSON encodes status as the integer ordinal.
const reviewCommitted = 2

// ---- typed models (mirror the per-slot `model` JSON) ----

// MissionStatement mirrors the Mission slot model.
type MissionStatement struct {
	Vision     string      `json:"vision"`
	Objectives []Objective `json:"objectives"`
	Mission    string      `json:"mission"`
}

// Objective is one numbered business objective.
type Objective struct {
	Number    int    `json:"number"`
	Statement string `json:"statement"`
}

// Glossary mirrors the Glossary slot model.
type Glossary struct {
	Items []GlossaryItem `json:"items"`
}

// GlossaryItem is one glossary entry.
type GlossaryItem struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
	Category   string `json:"category"`
}

// ScrubbedRequirements mirrors the ScrubbedRequirements slot model.
type ScrubbedRequirements struct {
	Items []Requirement `json:"items"`
}

// Requirement is one scrubbed requirement.
type Requirement struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

// Volatilities mirrors the Volatilities slot model.
type Volatilities struct {
	Items []Volatility `json:"items"`
}

// Volatility is one identified volatility.
type Volatility struct {
	Name      string `json:"name"`
	Rationale string `json:"rationale"`
	Axis      string `json:"axis"`
}

// CoreUseCases mirrors the CoreUseCases slot model.
type CoreUseCases struct {
	Decisions []UseCaseDecision `json:"decisions"`
}

// UseCaseDecision pairs a use case with its inclusion rationale.
type UseCaseDecision struct {
	UseCase         UseCase `json:"useCase"`
	RejectionReason string  `json:"rejectionReason"`
}

// UseCase mirrors a use-case model.
type UseCase struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Actors         []Actor          `json:"actors"`
	Trigger        string           `json:"trigger"`
	Classification string           `json:"classification"`
	VariationOf    *string          `json:"variationOf"`
	Activity       *ActivityDiagram `json:"activity"`
}

// Actor is a use-case participant role.
type Actor struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

// ActivityDiagram mirrors a use-case activity diagram.
type ActivityDiagram struct {
	Nodes []ActivityNode `json:"nodes"`
	Edges []ActivityEdge `json:"edges"`
}

// ActivityNode is a node in an activity diagram.
type ActivityNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// ActivityEdge is a directed edge in an activity diagram.
type ActivityEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	Guard string `json:"guard"`
}

// System mirrors the System slot model (Grammar A).
type System struct {
	Components    []Component    `json:"components"`
	Relationships []Relationship `json:"relationships"`
	DynamicViews  []DynamicView  `json:"dynamicViews"`
}

// Component is a node in the System static architecture model.
type Component struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Kind                string   `json:"kind"`
	Layer               string   `json:"layer"`
	Encapsulates        string   `json:"encapsulates"`
	AtomicBusinessVerbs []string `json:"atomicBusinessVerbs"`

	// ContractKey is an OPTIONAL pointer to this component's `.serviceContracts[key]`
	// entry. It is the join key the alignment pass uses to resolve a SHARED-goPackage
	// secondary component: when no code package matches the component's own normalized
	// name, ContractKey → serviceContracts[ContractKey].GoPackage names the package
	// that actually implements it (several RA components fronting one git-as-DB
	// aggregate package is the motivating shape). Empty means the component carries no
	// contract pointer — the ordinary name-matched alignment applies unchanged.
	ContractKey string `json:"contractKey,omitempty"`

	// BuildStatus is an OPTIONAL per-component lifecycle marker the design carries so
	// UIs can render a "planned"/"external" badge and the alignment rules can honor
	// the component's intended state. It lives in STATE (slot-5 component JSON), not in
	// the consuming module's test config, so the same value drives both the rendered
	// badge and the gate. Empty (absent) means BUILT — the component is expected to
	// have code. See buildStatus* constants.
	BuildStatus string `json:"buildStatus,omitempty"`
}

// Relationship is a directed edge between two Components.
type Relationship struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Mode  string `json:"mode"`
	Label string `json:"label"`
}

// DynamicView is one call chain per use case.
type DynamicView struct {
	UseCaseID    string         `json:"useCaseId"`
	Key          string         `json:"key"`
	Title        string         `json:"title"`
	Participants []string       `json:"participants"`
	Edges        []Relationship `json:"edges"`
}

// OperationalConcepts mirrors the OperationalConcepts slot model.
type OperationalConcepts struct {
	Decisions  []OperationalDecision `json:"decisions"`
	Deployment DeploymentTopology    `json:"deployment"`
}

// OperationalDecision is one infrastructure/topology decision.
type OperationalDecision struct {
	Topic               string `json:"topic"`
	Decision            string `json:"decision"`
	JustifyingObjective int    `json:"justifyingObjective"`
}

// DeploymentTopology is the typed C4 deployment model.
type DeploymentTopology struct {
	DeliveryStyle string                  `json:"deliveryStyle"`
	Containers    []DeployContainer       `json:"containers"`
	Environments  []DeploymentEnvironment `json:"environments"`
}

// DeployContainer is a deployable unit (C4 Container) packaging System components by name.
type DeployContainer struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Components  []string `json:"components"` // System component NAMES
}

// DeploymentEnvironment is the set of nodes for one profile.
type DeploymentEnvironment struct {
	Profile string           `json:"profile"`
	Title   string           `json:"title"`
	Nodes   []DeploymentNode `json:"nodes"`
}

// DeploymentNode is a nestable C4 deployment node.
type DeploymentNode struct {
	Name                    string                   `json:"name"`
	Technology              string                   `json:"technology"`
	Description             string                   `json:"description"`
	Instances               int                      `json:"instances"`
	Tags                    []string                 `json:"tags"`
	Children                []DeploymentNode         `json:"children"`
	InfrastructureNodes     []InfrastructureNode     `json:"infrastructureNodes"`
	ContainerInstances      []ContainerInstance      `json:"containerInstances"`
	SoftwareSystemInstances []SoftwareSystemInstance `json:"softwareSystemInstances"`
}

// ContainerInstance instances a declared DeployContainer inside a node.
type ContainerInstance struct {
	ContainerKey string   `json:"containerKey"`
	Note         string   `json:"note"`
	Tags         []string `json:"tags"`
}

// InfrastructureNode is non-deployable infra (gateway, DB engine, broker).
type InfrastructureNode struct {
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// SoftwareSystemInstance is an external software system (GitHub, Anthropic, Keycloak).
type SoftwareSystemInstance struct {
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// StandardCheck mirrors the StandardCheck slot model.
type StandardCheck struct {
	Items []CheckItem `json:"items"`
}

// CheckItem is one App C design-standard row.
type CheckItem struct {
	Section       string `json:"section"`
	Guideline     string `json:"guideline"`
	Status        string `json:"status"`
	Justification string `json:"justification"`
}

// ---- the slot + Project envelope ----

// Slot is one populated artifact slot in the committed document. The model is held
// as raw JSON and decoded lazily into a typed model by Project's per-kind getters.
type Slot struct {
	Status int             `json:"status"`
	Kind   int             `json:"kind"`
	Model  json.RawMessage `json:"model,omitempty"`
}

// committed reports whether a slot is architect-approved (status == ReviewCommitted)
// AND carries a model — the exact (status, model-present) gate the server's
// validateProject `committed` helper applies.
func (s Slot) committed() bool {
	return s.Status == reviewCommitted && len(s.Model) > 0
}

// kind ordinals — mirror projectstate.ArtifactKind iota (Phase-1 set is all this
// validator reads; the Phase-2 ordinals are decoded-then-ignored).
const (
	kindMission              = 0
	kindGlossary             = 1
	kindScrubbedRequirements = 2
	kindVolatilities         = 3
	kindCoreUseCases         = 4
	kindSystem               = 5
	kindOperationalConcepts  = 6
	kindStandardCheck        = 7
)

// Project is the structural mirror of the committed project.json envelope. Only the
// fields the rules + alignment read are carried; the slot map is keyed by the
// decimal kind ordinal string ("0".."7", …) exactly as the server writes it.
type Project struct {
	ID    string          `json:"id"`
	Slots map[string]Slot `json:"slots"`

	// ServiceContracts mirrors the top-level `.serviceContracts` map (component key →
	// contract document) the projectstate RA owns. Nil until the first contract is
	// seeded. Decoded leniently — the STP rule family reads it to resolve each test
	// step's {component, operation} against the designed contract surface.
	ServiceContracts map[string]ServiceContract `json:"serviceContracts,omitempty"`

	// TestingState mirrors the top-level `.testingState` record. Nil until the first
	// testing activity produces output; the STP rule family reads
	// TestingState.SystemTestPlan.
	TestingState *TestingState `json:"testingState,omitempty"`
}

// ---- service-contract corpus (mirror projectstate/servicecontract.go) ----

// ServiceContract is the LIGHTWEIGHT structural mirror of one component's contract
// document stored in `.serviceContracts[component]`. It carries only the fields the
// STP rules read (identity, layer, the `$defs` schema map, and the interface's
// operation surface); the app-side owner also carries codegen metadata (goPackage,
// deps, infra, stub) the rules do not consult. Decoded omit-empty-tolerant.
type ServiceContract struct {
	Component string                     `json:"component"`
	Layer     string                     `json:"layer"`
	GoPackage string                     `json:"goPackage,omitempty"`
	Title     string                     `json:"title,omitempty"`
	Defs      map[string]json.RawMessage `json:"$defs,omitempty"`
	Interface ContractInterface          `json:"interface"`
}

// ContractInterface mirrors the contract document's `interface`: the RPC surface's
// name, its Method layer, and its operations.
type ContractInterface struct {
	Name       string              `json:"name"`
	Layer      string              `json:"layer"`
	Operations []ContractOperation `json:"operations"`
}

// ContractOperation is one method on the interface. A nil Params slice encodes as
// `null` — the never-detailed-designed STUB marker STP-STALE-CONTRACT keys on.
type ContractOperation struct {
	Name   string          `json:"name"`
	Params []ContractParam `json:"params"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  bool            `json:"error"`
}

// ContractParam is one operation parameter. Schema is a JSON Schema node (raw) —
// either a `$ref` into the contract's `$defs` or an inline schema. Pointer marks a
// nullable pointer parameter (an OPTIONAL param — absence is not a missing-required).
type ContractParam struct {
	Name    string          `json:"name"`
	Pointer bool            `json:"pointer,omitempty"`
	Schema  json.RawMessage `json:"schema"`
}

// ---- system-test-plan (mirror projectstate/phaseartifacts.go TestingState) ----

// TestingState mirrors the top-level `.testingState` record. Only the SystemTestPlan
// is carried — the STP rule family's sole input.
type TestingState struct {
	SystemTestPlan *SystemTestPlan `json:"systemTestPlan,omitempty"`
}

// SystemTestPlan mirrors `.testingState.systemTestPlan` — the N-STP output the STP
// rule family validates against the committed contracts + architecture.
type SystemTestPlan struct {
	UseCaseIndex []string       `json:"useCaseIndex,omitempty"`
	Entries      []string       `json:"entries,omitempty"`
	Scenarios    []TestScenario `json:"scenarios,omitempty"`
	Status       string         `json:"status,omitempty"`
}

// TestScenario is one black-box scenario tracing to a core use case.
type TestScenario struct {
	ID          string     `json:"id"`
	UseCase     string     `json:"useCase"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Cases       []TestCase `json:"cases,omitempty"`
}

// TestCase is one falsification attempt: happy | negative | boundary.
type TestCase struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Proves          string     `json:"proves,omitempty"`
	ExpectedOutcome string     `json:"expectedOutcome,omitempty"`
	Steps           []TestStep `json:"steps,omitempty"`
}

// TestStep is one manager-operation call with its inputs and expected outcome.
type TestStep struct {
	Seq       int        `json:"seq"`
	Component string     `json:"component"`
	Operation string     `json:"operation"`
	Status    string     `json:"status,omitempty"`
	Inputs    []TestArg  `json:"inputs,omitempty"`
	Expect    TestExpect `json:"expect"`
	Assertion string     `json:"assertion,omitempty"`
}

// TestArg is one concrete input argument to a step's operation call.
type TestArg struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	SchemaRef string `json:"schemaRef,omitempty"`
}

// TestExpect is a step's expected outcome: a result value OR an expected error.
type TestExpect struct {
	Result        string `json:"result,omitempty"`
	ErrorExpected bool   `json:"errorExpected"`
	ErrorCode     string `json:"errorCode,omitempty"`
}

// systemTestPlan returns the committed System Test Plan, or nil when absent.
func (p Project) systemTestPlan() *SystemTestPlan {
	if p.TestingState == nil {
		return nil
	}
	return p.TestingState.SystemTestPlan
}

// slotByKind returns the committed slot for a kind ordinal, or false when the slot
// is absent or not committed.
func (p Project) slotByKind(kind int) (Slot, bool) {
	for _, s := range p.Slots {
		if s.Kind == kind && s.committed() {
			return s, true
		}
	}
	return Slot{}, false
}

// Typed committed-model getters. Each returns (model, true, error): ok=false when
// the slot is not committed; error only on a malformed model payload.

func (p Project) mission() (MissionStatement, bool, error) {
	s, ok := p.slotByKind(kindMission)
	if !ok {
		return MissionStatement{}, false, nil
	}
	var m MissionStatement
	return m, true, decodeModel(s.Model, &m, "Mission")
}

func (p Project) glossary() (Glossary, bool, error) {
	s, ok := p.slotByKind(kindGlossary)
	if !ok {
		return Glossary{}, false, nil
	}
	var m Glossary
	return m, true, decodeModel(s.Model, &m, "Glossary")
}

func (p Project) scrubbedRequirements() (ScrubbedRequirements, bool, error) {
	s, ok := p.slotByKind(kindScrubbedRequirements)
	if !ok {
		return ScrubbedRequirements{}, false, nil
	}
	var m ScrubbedRequirements
	return m, true, decodeModel(s.Model, &m, "ScrubbedRequirements")
}

func (p Project) volatilities() (Volatilities, bool, error) {
	s, ok := p.slotByKind(kindVolatilities)
	if !ok {
		return Volatilities{}, false, nil
	}
	var m Volatilities
	return m, true, decodeModel(s.Model, &m, "Volatilities")
}

func (p Project) coreUseCases() (CoreUseCases, bool, error) {
	s, ok := p.slotByKind(kindCoreUseCases)
	if !ok {
		return CoreUseCases{}, false, nil
	}
	var m CoreUseCases
	return m, true, decodeModel(s.Model, &m, "CoreUseCases")
}

func (p Project) system() (System, bool, error) {
	s, ok := p.slotByKind(kindSystem)
	if !ok {
		return System{}, false, nil
	}
	var m System
	return m, true, decodeModel(s.Model, &m, "System")
}

func (p Project) operationalConcepts() (OperationalConcepts, bool, error) {
	s, ok := p.slotByKind(kindOperationalConcepts)
	if !ok {
		return OperationalConcepts{}, false, nil
	}
	var m OperationalConcepts
	return m, true, decodeModel(s.Model, &m, "OperationalConcepts")
}

func (p Project) standardCheck() (StandardCheck, bool, error) {
	s, ok := p.slotByKind(kindStandardCheck)
	if !ok {
		return StandardCheck{}, false, nil
	}
	var m StandardCheck
	return m, true, decodeModel(s.Model, &m, "StandardCheck")
}

// committedSlotCount returns how many of the Phase-1 slots are committed — used by
// Check to detect a vacuous "nothing validated" run.
func (p Project) committedSlotCount() int {
	n := 0
	for _, kind := range []int{
		kindMission, kindGlossary, kindScrubbedRequirements, kindVolatilities,
		kindCoreUseCases, kindSystem, kindOperationalConcepts, kindStandardCheck,
	} {
		if _, ok := p.slotByKind(kind); ok {
			n++
		}
	}
	return n
}

// decodeModel unmarshals a slot's raw model payload into dst.
func decodeModel(raw json.RawMessage, dst any, kind string) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("methodcheck: decode %s model: %w", kind, err)
	}
	return nil
}

// DecodeProject decodes a raw `.aiarch/state/project.json` document into the
// structural Project envelope. ok=false (with nil error) when the bytes are empty —
// mirroring projectstate.DecodeProjectJSON's empty-input contract. A malformed
// envelope is an error.
func DecodeProject(raw []byte) (Project, bool, error) {
	if len(raw) == 0 {
		return Project{}, false, nil
	}
	var p Project
	if err := json.Unmarshal(raw, &p); err != nil {
		return Project{}, false, fmt.Errorf("methodcheck: decode project.json: %w", err)
	}
	return p, true, nil
}

// ---- Slug (mirror projectstate.Slug) ----

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a human-readable name into the stable identity token the aiarch
// server assigns: lowercased, non-alphanumeric runs collapsed to single hyphens,
// leading/trailing hyphens trimmed. Byte-identical to projectstate.Slug so the
// name-as-identity uniqueness predicates collide on the same keys.
func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
