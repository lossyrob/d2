package d2widescreenlayout_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"oss.terrastruct.com/d2/d2layouts/d2widescreenlayout"
)

func TestWriteLayoutState(t *testing.T) {
	t.Parallel()

	state := &d2widescreenlayout.LayoutState{
		Dimensions: d2widescreenlayout.DimensionState{
			Width:  1920,
			Height: 1080,
			Ratio:  1.7778,
		},
		Nodes: map[string]*d2widescreenlayout.NodeState{
			"api": {
				X: 100, Y: 50, Width: 200, Height: 100,
				Center: [2]float64{200, 100},
				Row:    0,
			},
		},
		Edges: map[string]*d2widescreenlayout.EdgeState{
			"api -> db": {
				Route: [][]float64{{200, 150}, {200, 300}, {400, 300}},
				Label: &d2widescreenlayout.LabelState{
					Text:     "queries",
					Position: [2]float64{200, 300},
				},
			},
		},
		Arrangement: d2widescreenlayout.ArrangementState{
			Rows: [][]string{{"api", "db"}},
		},
		Quality: d2widescreenlayout.QualityMetrics{
			Ratio:         1.7778,
			EdgeCrossings: 0,
		},
	}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "test.layout.json")

	err := d2widescreenlayout.WriteLayoutState(state, outPath)
	if err != nil {
		t.Fatalf("WriteLayoutState: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var loaded d2widescreenlayout.LayoutState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parsing output: %v", err)
	}

	if loaded.Dimensions.Width != 1920 {
		t.Errorf("expected width 1920, got %v", loaded.Dimensions.Width)
	}
	if loaded.Dimensions.Ratio != 1.7778 {
		t.Errorf("expected ratio 1.7778, got %v", loaded.Dimensions.Ratio)
	}
	if ns, ok := loaded.Nodes["api"]; !ok {
		t.Error("missing node 'api'")
	} else if ns.X != 100 || ns.Y != 50 {
		t.Errorf("expected api at (100,50), got (%v,%v)", ns.X, ns.Y)
	}
	if es, ok := loaded.Edges["api -> db"]; !ok {
		t.Error("missing edge 'api -> db'")
	} else if len(es.Route) != 3 {
		t.Errorf("expected 3 route points, got %d", len(es.Route))
	}
	if loaded.Quality.EdgeCrossings != 0 {
		t.Errorf("expected 0 crossings, got %d", loaded.Quality.EdgeCrossings)
	}
}
