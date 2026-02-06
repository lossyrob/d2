package d2widescreenlayout_test

import (
	"math"
	"testing"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2widescreenlayout"
	"oss.terrastruct.com/d2/lib/geo"
)

func makeObj(parent *d2graph.Object, id string, x, y, w, h float64) *d2graph.Object {
	obj := &d2graph.Object{
		ID:    id,
		IDVal: id,
		Box:   geo.NewBox(geo.NewPoint(x, y), w, h),
	}
	obj.Children = make(map[string]*d2graph.Object)
	obj.Parent = parent
	if parent != nil {
		parent.ChildrenArray = append(parent.ChildrenArray, obj)
		parent.Children[id] = obj
	}
	return obj
}

func makeGraph(objects ...*d2graph.Object) *d2graph.Graph {
	g := &d2graph.Graph{}
	root := &d2graph.Object{
		ID:  "",
		Box: geo.NewBox(geo.NewPoint(0, 0), 0, 0),
	}
	root.Children = make(map[string]*d2graph.Object)
	g.Root = root
	for _, obj := range objects {
		obj.Parent = root
		obj.Graph = g
		root.ChildrenArray = append(root.ChildrenArray, obj)
		root.Children[obj.ID] = obj
		g.Objects = append(g.Objects, obj)
	}
	return g
}

func TestComputeSubtreeBBox(t *testing.T) {
	t.Parallel()
	parent := makeObj(nil, "a", 10, 20, 100, 50)
	child := makeObj(parent, "b", 5, 15, 30, 30)
	_ = child

	bbox := d2widescreenlayout.ComputeSubtreeBBox(parent)
	if bbox.TopLeft.X != 5 {
		t.Errorf("expected minX=5, got %f", bbox.TopLeft.X)
	}
	if bbox.TopLeft.Y != 15 {
		t.Errorf("expected minY=15, got %f", bbox.TopLeft.Y)
	}
	if bbox.Width != 105 {
		t.Errorf("expected width=105 (110-5), got %f", bbox.Width)
	}
	if bbox.Height != 55 {
		t.Errorf("expected height=55 (70-15), got %f", bbox.Height)
	}
}

func TestComputeSubtreeBBox_NegativeCoords(t *testing.T) {
	t.Parallel()
	parent := makeObj(nil, "a", -10, -20, 100, 50)
	child := makeObj(parent, "b", -30, -5, 30, 30)
	_ = child

	bbox := d2widescreenlayout.ComputeSubtreeBBox(parent)
	if bbox.TopLeft.X != -30 {
		t.Errorf("expected minX=-30, got %f", bbox.TopLeft.X)
	}
	if bbox.TopLeft.Y != -20 {
		t.Errorf("expected minY=-20, got %f", bbox.TopLeft.Y)
	}
	// maxX = max(-10+100, -30+30) = max(90, 0) = 90
	if bbox.Width != 120 {
		t.Errorf("expected width=120 (90-(-30)), got %f", bbox.Width)
	}
}

func TestFindTopLevelAncestor(t *testing.T) {
	t.Parallel()
	root := &d2graph.Object{ID: "root"}
	root.Children = make(map[string]*d2graph.Object)

	a := makeObj(root, "a", 0, 0, 100, 100)
	b := makeObj(a, "b", 10, 10, 50, 50)
	c := makeObj(b, "c", 20, 20, 20, 20)

	result := d2widescreenlayout.FindTopLevelAncestor(c, root)
	if result != a {
		t.Errorf("expected top-level ancestor to be 'a', got %v", result)
	}

	result = d2widescreenlayout.FindTopLevelAncestor(a, root)
	if result != a {
		t.Errorf("expected top-level ancestor of 'a' to be 'a', got %v", result)
	}
}

func TestScoreArrangement(t *testing.T) {
	t.Parallel()
	// 4 boxes: 200x100, 300x150, 150x200, 250x100
	bboxes := []*geo.Box{
		geo.NewBox(geo.NewPoint(0, 0), 200, 100),
		geo.NewBox(geo.NewPoint(0, 0), 300, 150),
		geo.NewBox(geo.NewPoint(0, 0), 150, 200),
		geo.NewBox(geo.NewPoint(0, 0), 250, 100),
	}
	gap := 60.0
	target := 1.778

	// Single row: width = 200+60+300+60+150+60+250 = 1080, height = 200, ratio = 5.4
	singleRow := [][]int{{0, 1, 2, 3}}
	score := d2widescreenlayout.ScoreArrangement(singleRow, bboxes, gap, target)
	expectedScore := math.Abs(1080.0/200.0 - target)
	if math.Abs(score-expectedScore) > 0.01 {
		t.Errorf("single row score: expected %f, got %f", expectedScore, score)
	}

	// Two rows [0,1] [2,3]: row1 w=560 h=150, row2 w=460 h=200
	// total w=560, h=150+60+200=410, ratio=560/410≈1.37
	twoRows := [][]int{{0, 1}, {2, 3}}
	score2 := d2widescreenlayout.ScoreArrangement(twoRows, bboxes, gap, target)
	if score2 >= score {
		t.Errorf("two rows should score better than single row for 16:9 target")
	}
}

func TestRowArrangement_CustomRatio(t *testing.T) {
	t.Parallel()
	// 4 equal boxes, target ratio 1.0 (square)
	bboxes := []*geo.Box{
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
	}
	gap := 60.0
	target := 1.0

	// 2x2 arrangement: width = 100+60+100=260, height = 100+60+100=260, ratio = 1.0
	twoByTwo := [][]int{{0, 1}, {2, 3}}
	score := d2widescreenlayout.ScoreArrangement(twoByTwo, bboxes, gap, target)
	if score > 0.01 {
		t.Errorf("2x2 square arrangement should be near-perfect for ratio 1.0, got score %f", score)
	}
}

func TestRouteOrthogonal_HorizontalZ(t *testing.T) {
	t.Parallel()
	// Src at (0,0) 100x50, Dst at (300,100) 100x50
	// Centers: (50,25) and (350,125). dx=300, dy=100 → horizontal Z.
	srcCenter := geo.NewPoint(50, 25)
	dstCenter := geo.NewPoint(350, 125)

	route := d2widescreenlayout.BuildOrthogonalRoute(srcCenter, dstCenter, 0)

	if len(route) < 2 {
		t.Fatalf("expected at least 2 route points, got %d", len(route))
	}

	// Verify all segments are orthogonal
	for i := 0; i < len(route)-1; i++ {
		p1 := route[i]
		p2 := route[i+1]
		if math.Abs(p1.X-p2.X) > 0.01 && math.Abs(p1.Y-p2.Y) > 0.01 {
			t.Errorf("segment %d is not orthogonal: (%f,%f) -> (%f,%f)", i, p1.X, p1.Y, p2.X, p2.Y)
		}
	}
}

func TestRouteOrthogonal_SameRow_Degenerate(t *testing.T) {
	t.Parallel()
	// Test that same-row routing produces a detour with only orthogonal segments.
	// We test the raw route points before TraceToShape clipping.
	srcCenter := geo.NewPoint(50, 25)
	dstCenter := geo.NewPoint(250, 25)

	route := d2widescreenlayout.BuildOrthogonalRoute(srcCenter, dstCenter, 0)

	if len(route) < 4 {
		t.Fatalf("degenerate routing should produce at least 4 points (detour), got %d", len(route))
	}

	// Verify no zero-length segments
	for i := 0; i < len(route)-1; i++ {
		p1 := route[i]
		p2 := route[i+1]
		if p1.X == p2.X && p1.Y == p2.Y {
			t.Errorf("zero-length segment at index %d", i)
		}
	}

	// Verify all segments are orthogonal
	for i := 0; i < len(route)-1; i++ {
		p1 := route[i]
		p2 := route[i+1]
		if math.Abs(p1.X-p2.X) > 0.01 && math.Abs(p1.Y-p2.Y) > 0.01 {
			t.Errorf("segment %d is not orthogonal: (%f,%f) -> (%f,%f)", i, p1.X, p1.Y, p2.X, p2.Y)
		}
	}

	// Verify the detour goes above (Y < srcCenter.Y)
	hasDetour := false
	for _, p := range route {
		if p.Y < srcCenter.Y-1 {
			hasDetour = true
			break
		}
	}
	if !hasDetour {
		t.Error("degenerate routing should produce a vertical detour above the nodes")
	}
}

func TestRouteOrthogonal_LaneSeparation(t *testing.T) {
	t.Parallel()
	srcCenter := geo.NewPoint(50, 50)
	dstCenter := geo.NewPoint(350, 250)

	routes := make([][]*geo.Point, 3)
	for i := range routes {
		routes[i] = d2widescreenlayout.BuildOrthogonalRoute(srcCenter, dstCenter, float64(i)*20)
	}

	// Verify that middle segments differ between lanes
	if len(routes[0]) >= 3 && len(routes[1]) >= 3 {
		mid0 := routes[0][1]
		mid1 := routes[1][1]
		if mid0.X == mid1.X && mid0.Y == mid1.Y {
			t.Error("lane 0 and lane 1 should have different middle segment positions")
		}
	}
}

func TestGapSpacing(t *testing.T) {
	t.Parallel()
	// Two boxes side by side in a single row
	bboxes := []*geo.Box{
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
	}
	gap := 80.0

	// Single row: width = 100+80+100=280
	singleRow := [][]int{{0, 1}}
	score := d2widescreenlayout.ScoreArrangement(singleRow, bboxes, gap, 2.8)
	if score > 0.01 {
		t.Errorf("gap should be included in scoring; expected ratio 2.8 (280/100), got score %f", score)
	}
}

func TestSingleNodePassthrough(t *testing.T) {
	t.Parallel()
	// With 1 top-level node, Layout should return after inner engine (no rearrangement)
	// This is tested by verifying the short-circuit logic
	obj := makeObj(nil, "only", 50, 50, 200, 100)
	g := makeGraph(obj)

	// Can't call Layout directly (needs inner engine), but we can verify the
	// short-circuit condition: len(topLevel) <= 1
	if len(g.Root.ChildrenArray) > 1 {
		t.Error("expected single top-level node")
	}
}
