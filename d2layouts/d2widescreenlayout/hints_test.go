package d2widescreenlayout_test

import (
	"os"
	"path/filepath"
	"testing"

	"oss.terrastruct.com/d2/d2layouts/d2widescreenlayout"
)

func TestLoadHints_FileNotFound(t *testing.T) {
	t.Parallel()
	hints, err := d2widescreenlayout.LoadHints("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if hints != nil {
		t.Fatal("expected nil hints for missing file")
	}
}

func TestLoadHints_ValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hints.json")
	content := `{
		"arrangement": {
			"rows": [["a", "b"], ["c"]]
		},
		"edges": {
			"a -> b": {"side": "left", "laneIndex": -1},
			"b -> c": {"labelPosition": "InsideMiddleRight"}
		},
		"spacing": {
			"gap": 100
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	hints, err := d2widescreenlayout.LoadHints(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hints == nil {
		t.Fatal("expected non-nil hints")
	}

	// Verify arrangement
	if hints.Arrangement == nil {
		t.Fatal("expected arrangement hints")
	}
	if len(hints.Arrangement.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(hints.Arrangement.Rows))
	}
	if hints.Arrangement.Rows[0][0] != "a" || hints.Arrangement.Rows[0][1] != "b" {
		t.Fatalf("unexpected row 0: %v", hints.Arrangement.Rows[0])
	}

	// Verify edges
	if len(hints.Edges) != 2 {
		t.Fatalf("expected 2 edge hints, got %d", len(hints.Edges))
	}
	ab := hints.Edges["a -> b"]
	if ab == nil {
		t.Fatal("missing edge hint for 'a -> b'")
	}
	if ab.Side != "left" {
		t.Fatalf("expected side 'left', got %q", ab.Side)
	}
	if ab.LaneIndex == nil || *ab.LaneIndex != -1 {
		t.Fatal("expected laneIndex -1")
	}

	bc := hints.Edges["b -> c"]
	if bc == nil {
		t.Fatal("missing edge hint for 'b -> c'")
	}
	if bc.LabelPosition != "InsideMiddleRight" {
		t.Fatalf("expected labelPosition 'InsideMiddleRight', got %q", bc.LabelPosition)
	}

	// Verify spacing
	if hints.Spacing == nil || hints.Spacing.Gap == nil {
		t.Fatal("expected spacing hints")
	}
	if *hints.Spacing.Gap != 100 {
		t.Fatalf("expected gap 100, got %d", *hints.Spacing.Gap)
	}
}

func TestLoadHints_MalformedJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := d2widescreenlayout.LoadHints(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoadHints_Waypoints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wp.hints.json")
	content := `{
		"edges": {
			"a -> b": {
				"waypoints": [
					{"x": 100, "y": 200},
					{"x": 300, "y": 200},
					{"x": 300, "y": 400}
				]
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	hints, err := d2widescreenlayout.LoadHints(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ab := hints.Edges["a -> b"]
	if ab == nil {
		t.Fatal("missing edge hint")
	}
	if len(ab.Waypoints) != 3 {
		t.Fatalf("expected 3 waypoints, got %d", len(ab.Waypoints))
	}
	if ab.Waypoints[0].X != 100 || ab.Waypoints[0].Y != 200 {
		t.Fatalf("unexpected waypoint[0]: %+v", ab.Waypoints[0])
	}
	if ab.Waypoints[2].X != 300 || ab.Waypoints[2].Y != 400 {
		t.Fatalf("unexpected waypoint[2]: %+v", ab.Waypoints[2])
	}
}

func TestLoadHints_NodeHints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.hints.json")
	content := `{
		"nodes": {
			"api": {"x": 100, "y": 50, "minWidth": 250},
			"db": {"y": 400}
		},
		"edges": {
			"api -> db": {
				"labelPercentage": 0.3,
				"labelOffset": {"x": 10, "y": -5}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	hints, err := d2widescreenlayout.LoadHints(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify node hints
	if len(hints.Nodes) != 2 {
		t.Fatalf("expected 2 node hints, got %d", len(hints.Nodes))
	}
	api := hints.Nodes["api"]
	if api == nil {
		t.Fatal("missing node hint for 'api'")
	}
	if api.X == nil || *api.X != 100 {
		t.Fatalf("expected api.X=100, got %v", api.X)
	}
	if api.Y == nil || *api.Y != 50 {
		t.Fatalf("expected api.Y=50, got %v", api.Y)
	}
	if api.MinWidth == nil || *api.MinWidth != 250 {
		t.Fatalf("expected api.MinWidth=250, got %v", api.MinWidth)
	}

	db := hints.Nodes["db"]
	if db.X != nil {
		t.Fatalf("expected db.X=nil, got %v", *db.X)
	}
	if db.Y == nil || *db.Y != 400 {
		t.Fatalf("expected db.Y=400, got %v", db.Y)
	}

	// Verify label hints
	edge := hints.Edges["api -> db"]
	if edge.LabelPercentage == nil || *edge.LabelPercentage != 0.3 {
		t.Fatalf("expected labelPercentage=0.3, got %v", edge.LabelPercentage)
	}
	if edge.LabelOffset == nil || edge.LabelOffset.X != 10 || edge.LabelOffset.Y != -5 {
		t.Fatalf("unexpected labelOffset: %+v", edge.LabelOffset)
	}
}

func TestParseEdgeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key     string
		src     string
		dst     string
		index   int
		wantErr bool
	}{
		{"a -> b", "a", "b", 0, false},
		{"runner.orchestrator -> services", "runner.orchestrator", "services", 0, false},
		{"a -> b[0]", "a", "b", 0, false},
		{"a -> b[3]", "a", "b", 3, false},
		{"  a.x  ->  b.y.z[1]  ", "a.x", "b.y.z", 1, false},
		{"invalid", "", "", 0, true},
		{"a - b", "", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			src, dst, idx, err := d2widescreenlayout.ParseEdgeKey(tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src != tt.src {
				t.Fatalf("src: got %q, want %q", src, tt.src)
			}
			if dst != tt.dst {
				t.Fatalf("dst: got %q, want %q", dst, tt.dst)
			}
			if idx != tt.index {
				t.Fatalf("index: got %d, want %d", idx, tt.index)
			}
		})
	}
}
