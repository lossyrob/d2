package d2widescreenlayout

import (
	"encoding/json"
	"fmt"
	"os"

	"oss.terrastruct.com/d2/d2graph"
)

// LayoutState is the complete layout output for programmatic consumption by agents.
// Written as JSON alongside the rendered output to enable the agent feedback loop.
type LayoutState struct {
	Dimensions DimensionState           `json:"dimensions"`
	Nodes      map[string]*NodeState    `json:"nodes"`
	Edges      map[string]*EdgeState    `json:"edges"`
	Arrangement ArrangementState        `json:"arrangement"`
	Quality    QualityMetrics           `json:"quality"`
}

type DimensionState struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Ratio  float64 `json:"ratio"`
}

type NodeState struct {
	X        float64                `json:"x"`
	Y        float64                `json:"y"`
	Width    float64                `json:"width"`
	Height   float64                `json:"height"`
	Center   [2]float64             `json:"center"`
	Row      int                    `json:"row,omitempty"`
	Children map[string]*NodeState  `json:"children,omitempty"`
}

type EdgeState struct {
	Route [][]float64 `json:"route"`
	Label *LabelState `json:"label,omitempty"`
}

type LabelState struct {
	Text     string     `json:"text"`
	Position [2]float64 `json:"position"`
}

type ArrangementState struct {
	Rows [][]string `json:"rows"`
}

type QualityMetrics struct {
	Ratio         float64 `json:"ratio"`
	EdgeCrossings int     `json:"edgeCrossings"`
}

// ExportLayoutState builds a LayoutState from the graph after layout is complete.
func ExportLayoutState(g *d2graph.Graph, gl *gridLayout) *LayoutState {
	state := &LayoutState{
		Nodes: make(map[string]*NodeState),
		Edges: make(map[string]*EdgeState),
	}

	// Dimensions
	if g.Root.Width > 0 && g.Root.Height > 0 {
		state.Dimensions = DimensionState{
			Width:  g.Root.Width,
			Height: g.Root.Height,
			Ratio:  g.Root.Width / g.Root.Height,
		}
		state.Quality.Ratio = state.Dimensions.Ratio
	}

	// Nodes — top-level with children
	for _, obj := range g.Root.ChildrenArray {
		ns := buildNodeState(obj)
		if gl != nil {
			if row, ok := gl.nodeRow[obj]; ok {
				ns.Row = row
			}
		}
		state.Nodes[obj.AbsID()] = ns
	}

	// Arrangement from gridLayout
	if gl != nil {
		state.Arrangement.Rows = make([][]string, len(gl.rows))
		for i, row := range gl.rows {
			state.Arrangement.Rows[i] = make([]string, len(row))
			for j, idx := range row {
				state.Arrangement.Rows[i][j] = g.Root.ChildrenArray[idx].ID
			}
		}
	}

	// Edges — all cross-boundary edges with routes
	ancestorMap := make(map[*d2graph.Object]*d2graph.Object)
	for _, tl := range g.Root.ChildrenArray {
		ancestorMap[tl] = tl
		tl.IterDescendants(func(_, child *d2graph.Object) {
			ancestorMap[child] = tl
		})
	}

	edgeCounts := make(map[string]int)
	for _, e := range g.Edges {
		srcTL := ancestorMap[e.Src]
		dstTL := ancestorMap[e.Dst]
		if srcTL == nil || dstTL == nil || srcTL == dstTL {
			continue
		}

		baseKey := fmt.Sprintf("%s -> %s", e.Src.AbsID(), e.Dst.AbsID())
		idx := edgeCounts[baseKey]
		edgeCounts[baseKey]++

		key := baseKey
		if idx > 0 {
			key = fmt.Sprintf("%s[%d]", baseKey, idx)
		}

		es := &EdgeState{}
		if len(e.Route) > 0 {
			es.Route = make([][]float64, len(e.Route))
			for i, p := range e.Route {
				es.Route[i] = []float64{p.X, p.Y}
			}
		}
		if e.Label.Value != "" {
			ls := &LabelState{Text: e.Label.Value}
			// Compute label center from route midpoint
			if len(e.Route) >= 2 {
				mid := len(e.Route) / 2
				ls.Position = [2]float64{e.Route[mid].X, e.Route[mid].Y}
			}
			es.Label = ls
		}

		state.Edges[key] = es
	}

	// Count edge crossings (simplified: check pairwise horizontal segment intersections)
	state.Quality.EdgeCrossings = countEdgeCrossings(g, ancestorMap)

	return state
}

func buildNodeState(obj *d2graph.Object) *NodeState {
	ns := &NodeState{}
	if obj.TopLeft != nil {
		ns.X = obj.TopLeft.X
		ns.Y = obj.TopLeft.Y
	}
	ns.Width = obj.Width
	ns.Height = obj.Height
	center := obj.Center()
	ns.Center = [2]float64{center.X, center.Y}

	if len(obj.ChildrenArray) > 0 {
		ns.Children = make(map[string]*NodeState)
		for _, child := range obj.ChildrenArray {
			ns.Children[child.AbsID()] = buildNodeState(child)
		}
	}

	return ns
}

func countEdgeCrossings(g *d2graph.Graph, ancestorMap map[*d2graph.Object]*d2graph.Object) int {
	// Collect cross-boundary edge routes
	type segment struct {
		x1, y1, x2, y2 float64
	}
	var segments []segment

	for _, e := range g.Edges {
		srcTL := ancestorMap[e.Src]
		dstTL := ancestorMap[e.Dst]
		if srcTL == nil || dstTL == nil || srcTL == dstTL {
			continue
		}
		for i := 0; i < len(e.Route)-1; i++ {
			segments = append(segments, segment{
				e.Route[i].X, e.Route[i].Y,
				e.Route[i+1].X, e.Route[i+1].Y,
			})
		}
	}

	crossings := 0
	for i := 0; i < len(segments); i++ {
		for j := i + 1; j < len(segments); j++ {
			if segmentsIntersect(segments[i], segments[j]) {
				crossings++
			}
		}
	}
	return crossings
}

func segmentsIntersect(a, b struct{ x1, y1, x2, y2 float64 }) bool {
	// Standard cross-product segment intersection test
	d1 := direction(b.x1, b.y1, b.x2, b.y2, a.x1, a.y1)
	d2 := direction(b.x1, b.y1, b.x2, b.y2, a.x2, a.y2)
	d3 := direction(a.x1, a.y1, a.x2, a.y2, b.x1, b.y1)
	d4 := direction(a.x1, a.y1, a.x2, a.y2, b.x2, b.y2)

	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
		((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return true
	}
	return false
}

func direction(ax, ay, bx, by, cx, cy float64) float64 {
	return (bx-ax)*(cy-ay) - (by-ay)*(cx-ax)
}

// WriteLayoutState serializes the layout state to a JSON file.
func WriteLayoutState(state *LayoutState, path string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling layout state: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing layout state to %s: %w", path, err)
	}
	return nil
}
