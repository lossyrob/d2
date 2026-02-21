package d2widescreenlayout_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2widescreenlayout"
)

func TestIntegration_HintsArrangement(t *testing.T) {
	t.Parallel()
	script := `direction: right
triggers: Triggers { shape: hexagon }
runner: Runner {
  control: Control Loop
  orchestrator: Orchestrator
  control -> orchestrator: drives
}
services: Context Services
outputs: Outputs
triggers -> runner.control
runner.orchestrator -> services: fetches
runner -> outputs: persists`

	dir := t.TempDir()
	hintsPath := filepath.Join(dir, "test.hints.json")
	hints := `{
		"arrangement": {"rows": [["triggers","runner"],["services","outputs"]]},
		"spacing": {"gap": 130}
	}`
	if err := os.WriteFile(hintsPath, []byte(hints), 0644); err != nil {
		t.Fatal(err)
	}

	g := compileD2WithOpts(t, script, hintsPath, "")

	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("expected non-zero root dimensions")
	}

	triggers := findObjByID(g, "triggers")
	services := findObjByID(g, "services")
	if triggers == nil || services == nil {
		t.Fatal("missing expected objects")
	}
	if services.TopLeft.Y <= triggers.TopLeft.Y {
		t.Errorf("expected services below triggers, got services.Y=%v triggers.Y=%v",
			services.TopLeft.Y, triggers.TopLeft.Y)
	}
}

func TestIntegration_HintsWaypoints(t *testing.T) {
	t.Parallel()
	script := `direction: right
a: Node A
b: Node B
c: Node C { d: Inner }
a -> c.d: links
b -> c.d: connects`

	dir := t.TempDir()
	hintsPath := filepath.Join(dir, "test.hints.json")
	hints := `{
		"edges": {
			"a -> c.d": {
				"waypoints": [{"x": 200, "y": 50}, {"x": 200, "y": 150}]
			}
		}
	}`
	if err := os.WriteFile(hintsPath, []byte(hints), 0644); err != nil {
		t.Fatal(err)
	}

	g := compileD2WithOpts(t, script, hintsPath, "")

	for _, e := range g.Edges {
		if e.Src.AbsID() == "a" && e.Dst.AbsID() == "c.d" {
			if len(e.Route) < 2 {
				t.Errorf("expected waypoint edge to have route, got %d points", len(e.Route))
			}
			return
		}
	}
	t.Error("did not find edge a -> c.d")
}

func TestIntegration_LayoutStateExport(t *testing.T) {
	t.Parallel()
	script := `direction: right
api: API Gateway
db: Database
api -> db: queries`

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	g := compileD2WithOpts(t, script, "", statePath)
	_ = g

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading layout state: %v", err)
	}

	var state d2widescreenlayout.LayoutState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parsing layout state: %v", err)
	}

	if state.Dimensions.Width == 0 || state.Dimensions.Height == 0 {
		t.Error("expected non-zero dimensions in state")
	}
	if state.Dimensions.Ratio <= 0 {
		t.Error("expected positive ratio in state")
	}
	if _, ok := state.Nodes["api"]; !ok {
		t.Error("missing 'api' in layout state nodes")
	}
	if _, ok := state.Nodes["db"]; !ok {
		t.Error("missing 'db' in layout state nodes")
	}
	if state.Quality.Ratio <= 0 {
		t.Error("expected positive quality ratio")
	}
}

func TestIntegration_NodePositionHints(t *testing.T) {
	t.Parallel()
	script := `direction: right
a: Node A
b: Node B
a -> b: connects`

	dir := t.TempDir()
	hintsPath := filepath.Join(dir, "test.hints.json")
	statePath := filepath.Join(dir, "state.json")
	hints := `{"nodes": {"a": {"x": 50, "y": 100}}}`
	if err := os.WriteFile(hintsPath, []byte(hints), 0644); err != nil {
		t.Fatal(err)
	}

	compileD2WithOpts(t, script, hintsPath, statePath)

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	var state d2widescreenlayout.LayoutState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parsing state: %v", err)
	}

	aNode := state.Nodes["a"]
	if aNode == nil {
		t.Fatal("missing node 'a' in state")
	}
	if aNode.X != 50 || aNode.Y != 100 {
		t.Errorf("expected node 'a' at (50,100), got (%v,%v)", aNode.X, aNode.Y)
	}
}

func TestIntegration_InvalidInnerEngine(t *testing.T) {
	t.Parallel()
	script := `a: Node A`

	g := compileD2(t, script, "widescreen")
	_ = g
	// The widescreen engine uses dagre by default, so this should succeed.
	// Test explicit invalid engine through Layout() directly.
	opts := d2widescreenlayout.DefaultOpts
	opts.InnerEngine = "nonexistent"

	err := d2widescreenlayout.Layout(t.Context(), compileD2(t, script, "dagre"), &opts)
	if err == nil {
		t.Fatal("expected error for invalid inner engine")
	}
}

func TestIntegration_Diagram1E2E(t *testing.T) {
	t.Parallel()
	script, err := os.ReadFile("testdata/hints/diagram1.d2")
	if err != nil {
		t.Fatalf("reading test diagram: %v", err)
	}

	hintsPath, _ := filepath.Abs("testdata/hints/diagram1.d2.hints.json")
	g := compileD2WithOpts(t, string(script), hintsPath, "")

	ratio := g.Root.Width / g.Root.Height
	if ratio < 1.5 || ratio > 3.0 {
		t.Errorf("expected widescreen ratio in [1.5, 3.0], got %v", ratio)
	}

	crossEdges := 0
	for _, e := range g.Edges {
		srcTL := findTopLevel(g, e.Src)
		dstTL := findTopLevel(g, e.Dst)
		if srcTL != dstTL {
			crossEdges++
			if len(e.Route) < 2 {
				t.Errorf("cross-boundary edge %s -> %s has no route", e.Src.AbsID(), e.Dst.AbsID())
			}
		}
	}
	if crossEdges == 0 {
		t.Error("expected cross-boundary edges in diagram1")
	}
}

func TestIntegration_NoHintsFallback(t *testing.T) {
	t.Parallel()
	script, err := os.ReadFile("testdata/hints/no_hints.d2")
	if err != nil {
		t.Fatalf("reading test diagram: %v", err)
	}
	g := compileD2(t, string(script), "widescreen")
	if g.Root.Width == 0 {
		t.Error("expected non-zero width without hints")
	}
}

// compileD2WithOpts compiles with widescreen layout using hints and/or state paths.
func compileD2WithOpts(t *testing.T, script, hintsPath, statePath string) *d2graph.Graph {
	t.Helper()

	// Use the Layout function directly with opts
	g := compileD2(t, script, "dagre")

	opts := d2widescreenlayout.DefaultOpts
	if hintsPath != "" {
		opts.HintsPath = hintsPath
	}
	if statePath != "" {
		opts.LayoutStatePath = statePath
	}

	if err := d2widescreenlayout.Layout(t.Context(), g, &opts); err != nil {
		t.Fatalf("widescreen layout: %v", err)
	}
	return g
}

func findObjByID(g *d2graph.Graph, id string) *d2graph.Object {
	for _, obj := range g.Objects {
		if obj.ID == id {
			return obj
		}
	}
	return nil
}

func findTopLevel(g *d2graph.Graph, obj *d2graph.Object) *d2graph.Object {
	for _, tl := range g.Root.ChildrenArray {
		if obj == tl || obj.IsDescendantOf(tl) {
			return tl
		}
	}
	return nil
}
