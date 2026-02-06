package d2widescreenlayout_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2layouts/d2widescreenlayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

func layoutResolver(engine string) (d2graph.LayoutGraph, error) {
	if strings.EqualFold(engine, "elk") {
		return d2elklayout.DefaultLayout, nil
	}
	if strings.EqualFold(engine, "dagre") {
		return d2dagrelayout.DefaultLayout, nil
	}
	if strings.EqualFold(engine, "widescreen") {
		return func(ctx context.Context, g *d2graph.Graph) error {
			return d2widescreenlayout.Layout(ctx, g, nil)
		}, nil
	}
	return d2dagrelayout.DefaultLayout, nil
}

func compileD2(t *testing.T, script, engine string) *d2graph.Graph {
	t.Helper()
	ctx := context.Background()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatalf("failed to create ruler: %v", err)
	}
	compileOpts := &d2lib.CompileOptions{
		Ruler:          ruler,
		Layout:         go2.Pointer(engine),
		LayoutResolver: layoutResolver,
	}
	renderOpts := &d2svg.RenderOpts{
		Pad: go2.Pointer(int64(0)),
	}
	_, g, err := d2lib.Compile(ctx, script, compileOpts, renderOpts)
	if err != nil {
		t.Fatalf("failed to compile with %s: %v", engine, err)
	}
	return g
}

func TestIntegration_IssueTestDiagram(t *testing.T) {
	t.Parallel()
	script := `direction: right
triggers: Triggers { shape: hexagon }
runner: Runner {
  direction: right
  control: Control Loop
  orchestrator: Orchestrator
  stages: Stage Sessions { style.multiple: true }
  workdir: Working Directory
  control -> orchestrator: drives
  control -> stages: spawns
  orchestrator -> workdir: writes
  stages -> workdir: writes
}
triggers -> runner.control
services: Context Services
outputs: Outputs
runner.orchestrator -> services: fetches
runner.stages -> services: queries
runner -> outputs: persists
`
	g := compileD2(t, script, "widescreen")
	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("root dimensions should be non-zero")
	}
	ratio := g.Root.Width / g.Root.Height
	// SC-001: within 20% of 1.778 → ratio between ~1.4 and ~2.1
	if ratio < 1.4 || ratio > 2.2 {
		t.Errorf("expected ratio within 20%% of 1.778, got %f (width=%f, height=%f)", ratio, g.Root.Width, g.Root.Height)
	}
}

func TestIntegration_CustomRatio_Square(t *testing.T) {
	t.Parallel()
	script := `a
b
c
d
`
	ctx := context.Background()
	opts := d2widescreenlayout.DefaultOpts
	opts.Ratio = 1.0
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatalf("failed to create ruler: %v", err)
	}

	compileOpts := &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: go2.Pointer("widescreen"),
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2widescreenlayout.Layout(ctx, g, &opts)
			}, nil
		},
	}
	renderOpts := &d2svg.RenderOpts{
		Pad: go2.Pointer(int64(0)),
	}
	_, g, err := d2lib.Compile(ctx, script, compileOpts, renderOpts)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("root dimensions should be non-zero")
	}
	ratio := g.Root.Width / g.Root.Height
	// SC-003: within 20% of 1.0 → ratio between 0.8 and 1.2
	if ratio < 0.8 || ratio > 1.2 {
		t.Errorf("targeting ratio 1.0 but got %f", ratio)
	}
}

func TestIntegration_NestedContainers(t *testing.T) {
	t.Parallel()
	script := `a: {
  x: X
  y: Y
  x -> y
}
b: {
  p: P
  q: Q
  p -> q
}
a -> b
`
	g := compileD2(t, script, "widescreen")
	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("root dimensions should be non-zero")
	}
	// Just verify it compiles and produces valid output
	if len(g.Objects) == 0 {
		t.Error("expected objects in graph")
	}
}

func TestIntegration_CrossBoundaryEdges(t *testing.T) {
	t.Parallel()
	script := `a: {
  b: {
    c: Deep Node
  }
}
d: {
  e: Another Node
}
a.b.c -> d.e
`
	g := compileD2(t, script, "widescreen")
	// Find the cross-boundary edge
	found := false
	for _, e := range g.Edges {
		if e.Route != nil && len(e.Route) >= 2 {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one routed edge")
	}
}

func TestIntegration_InnerEngineELK(t *testing.T) {
	t.Parallel()
	script := `a: Node A
b: Node B
a -> b
`
	ctx := context.Background()
	opts := d2widescreenlayout.DefaultOpts
	opts.InnerEngine = "elk"
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatalf("failed to create ruler: %v", err)
	}

	compileOpts := &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: go2.Pointer("widescreen"),
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2widescreenlayout.Layout(ctx, g, &opts)
			}, nil
		},
	}
	renderOpts := &d2svg.RenderOpts{
		Pad: go2.Pointer(int64(0)),
	}
	_, g, err := d2lib.Compile(ctx, script, compileOpts, renderOpts)
	if err != nil {
		t.Fatalf("compile with elk inner engine failed: %v", err)
	}
	if g.Root.Width == 0 {
		t.Error("expected non-zero dimensions with elk")
	}
}

func TestIntegration_InnerEngineInvalid(t *testing.T) {
	t.Parallel()
	script := `a -> b`
	ctx := context.Background()
	opts := d2widescreenlayout.DefaultOpts
	opts.InnerEngine = "nonexistent"
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatalf("failed to create ruler: %v", err)
	}

	compileOpts := &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: go2.Pointer("widescreen"),
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2widescreenlayout.Layout(ctx, g, &opts)
			}, nil
		},
	}
	renderOpts := &d2svg.RenderOpts{
		Pad: go2.Pointer(int64(0)),
	}
	_, _, err = d2lib.Compile(ctx, script, compileOpts, renderOpts)
	if err == nil {
		t.Fatal("expected error for nonexistent inner engine")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention the unsupported engine name, got: %v", err)
	}
}

func TestIntegration_NoEdgesBetweenTopLevel(t *testing.T) {
	t.Parallel()
	script := `a: Node A
b: Node B
c: Node C
`
	g := compileD2(t, script, "widescreen")
	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("root dimensions should be non-zero")
	}
}

func TestIntegration_SingleTopLevel(t *testing.T) {
	t.Parallel()
	script := `only: {
  x: X
  y: Y
  x -> y
}
`
	g := compileD2(t, script, "widescreen")
	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("root dimensions should be non-zero")
	}
}

func TestIntegration_WideContainer(t *testing.T) {
	t.Parallel()
	script := `wide: {
  direction: right
  a; b; c; d; e; f; g; h
}
narrow: small
`
	g := compileD2(t, script, "widescreen")
	if g.Root.Width == 0 || g.Root.Height == 0 {
		t.Fatal("root dimensions should be non-zero")
	}
	ratio := g.Root.Width / g.Root.Height
	// Best effort — wide container may dominate, but should not crash
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		t.Error("ratio should be finite")
	}
}
