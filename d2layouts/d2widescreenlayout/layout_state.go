package d2widescreenlayout

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/lib/geo"
)

// LayoutState is the complete layout output for programmatic consumption by agents.
// Written as JSON alongside the rendered output to enable the agent feedback loop.
type LayoutState struct {
	EngineVersion  string                   `json:"engineVersion"`
	Dimensions     DimensionState           `json:"dimensions"`
	Nodes          map[string]*NodeState    `json:"nodes"`
	Edges          map[string]*EdgeState    `json:"edges"`
	Arrangement    ArrangementState         `json:"arrangement"`
	Quality        QualityMetrics           `json:"quality"`
	HintsApplied   *HintsAppliedState       `json:"hintsApplied,omitempty"`
	SuggestedHints *SuggestedHintsState     `json:"suggestedHints,omitempty"`
}

// EngineVersionString is the current version of the widescreen layout engine.
// Agents should check this to verify binary freshness.
const EngineVersionString = "0.8.0"

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
	Route      [][]float64 `json:"route"`
	SourceSide string      `json:"sourceSide,omitempty"`
	TargetSide string      `json:"targetSide,omitempty"`
	Type       string      `json:"type,omitempty"`
	Label      *LabelState `json:"label,omitempty"`
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
	TotalEdges    int     `json:"totalEdges"`
	BackwardEdges int     `json:"backwardEdges"`
}

// HintsAppliedState reports which hints were read and whether they had effect.
type HintsAppliedState struct {
	Arrangement *HintEffect            `json:"arrangement,omitempty"`
	Spacing     *HintEffect            `json:"spacing,omitempty"`
	Edges       map[string]*HintEffect `json:"edges,omitempty"`
	Nodes       map[string]*HintEffect `json:"nodes,omitempty"`
}

type HintEffect struct {
	Status  string `json:"status"`            // "applied", "no_effect", "error"
	Details string `json:"details,omitempty"`
}

// SuggestedHintsState provides a pre-populated hints template agents can modify.
type SuggestedHintsState struct {
	Arrangement []string                      `json:"arrangement"`
	Spacing     map[string]int                `json:"spacing"`
	Edges       map[string]*SuggestedEdgeHint `json:"edges,omitempty"`
}

type SuggestedEdgeHint struct {
	SourceSide string `json:"sourceSide"`
	TargetSide string `json:"targetSide"`
}

// ExportLayoutState builds a LayoutState from the graph after layout is complete.
func ExportLayoutState(g *d2graph.Graph, gl *gridLayout, hints *LayoutHints) *LayoutState {
	state := &LayoutState{
		EngineVersion: EngineVersionString,
		Nodes:         make(map[string]*NodeState),
		Edges:         make(map[string]*EdgeState),
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

	// Edges — ALL edges with routes (cross-boundary and intra-container)
	ancestorMap := make(map[*d2graph.Object]*d2graph.Object)
	for _, tl := range g.Root.ChildrenArray {
		ancestorMap[tl] = tl
		tl.IterDescendants(func(_, child *d2graph.Object) {
			ancestorMap[child] = tl
		})
	}

	edgeCounts := make(map[string]int)
	for _, e := range g.Edges {
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

		// Determine source/target sides from route endpoints
		if len(e.Route) >= 2 {
			es.SourceSide = inferSide(e.Src, *e.Route[0])
			es.TargetSide = inferSide(e.Dst, *e.Route[len(e.Route)-1])
		}

		// Classify edge type
		srcTL := ancestorMap[e.Src]
		dstTL := ancestorMap[e.Dst]
		if srcTL != nil && dstTL != nil && srcTL != dstTL {
			es.Type = "cross-boundary"
		} else {
			es.Type = "internal"
		}

		if e.Label.Value != "" {
			ls := &LabelState{Text: e.Label.Value}
			if len(e.Route) >= 2 {
				mid := len(e.Route) / 2
				ls.Position = [2]float64{e.Route[mid].X, e.Route[mid].Y}
			}
			es.Label = ls
		}

		state.Edges[key] = es
	}

	state.Quality.TotalEdges = len(g.Edges)

	// Count edge crossings (cross-boundary edges only)
	state.Quality.EdgeCrossings = countEdgeCrossings(g, ancestorMap)

	// Count backward edges: cross-boundary edges where source is to the right of target
	if gl != nil {
		for _, e := range g.Edges {
			srcTL := ancestorMap[e.Src]
			dstTL := ancestorMap[e.Dst]
			if srcTL == nil || dstTL == nil || srcTL == dstTL {
				continue
			}
			srcRow := gl.nodeRow[srcTL]
			dstRow := gl.nodeRow[dstTL]
			srcCol := gl.nodeCol[srcTL]
			dstCol := gl.nodeCol[dstTL]
			if srcRow != dstRow && srcCol > dstCol {
				state.Quality.BackwardEdges++
			}
		}
	}

	// Populate hintsApplied: report what was provided and whether it took effect
	if hints != nil {
		ha := &HintsAppliedState{}
		if hints.Arrangement != nil && len(hints.Arrangement.Rows) > 0 {
			ha.Arrangement = &HintEffect{Status: "applied", Details: fmt.Sprintf("%d rows specified", len(hints.Arrangement.Rows))}
		}
		if hints.Spacing != nil {
			ha.Spacing = &HintEffect{Status: "applied"}
			parts := []string{}
			if hints.Spacing.Horizontal != nil {
				parts = append(parts, fmt.Sprintf("horizontal=%d", *hints.Spacing.Horizontal))
			}
			if hints.Spacing.Vertical != nil {
				parts = append(parts, fmt.Sprintf("vertical=%d", *hints.Spacing.Vertical))
			}
			if hints.Spacing.Gap != nil {
				parts = append(parts, fmt.Sprintf("gap=%d", *hints.Spacing.Gap))
			}
			if len(parts) > 0 {
				ha.Spacing.Details = strings.Join(parts, ", ")
			}
		}
		if len(hints.Edges) > 0 {
			ha.Edges = make(map[string]*HintEffect)
			for key, eh := range hints.Edges {
				if len(eh.Waypoints) > 0 {
					ha.Edges[key] = &HintEffect{Status: "applied", Details: fmt.Sprintf("%d waypoints", len(eh.Waypoints))}
				} else {
					ha.Edges[key] = &HintEffect{Status: "applied"}
				}
			}
		}
		if len(hints.Nodes) > 0 {
			ha.Nodes = make(map[string]*HintEffect)
			for key := range hints.Nodes {
				ha.Nodes[key] = &HintEffect{Status: "applied"}
			}
		}
		state.HintsApplied = ha
	}

	// Populate suggestedHints: template agents can copy and modify
	suggested := &SuggestedHintsState{
		Spacing: map[string]int{"horizontal": 60, "vertical": 40},
	}
	// Suggest current arrangement order
	if gl != nil && len(gl.rows) > 0 {
		for _, row := range gl.rows {
			for _, idx := range row {
				suggested.Arrangement = append(suggested.Arrangement, g.Root.ChildrenArray[idx].ID)
			}
		}
	}
	// Suggest edge hints for cross-boundary edges
	if len(state.Edges) > 0 {
		suggested.Edges = make(map[string]*SuggestedEdgeHint)
		for key, es := range state.Edges {
			if es.Type == "cross-boundary" && es.SourceSide != "" {
				suggested.Edges[key] = &SuggestedEdgeHint{
					SourceSide: es.SourceSide,
					TargetSide: es.TargetSide,
				}
			}
		}
	}
	state.SuggestedHints = suggested

	return state
}

// inferSide determines which side of a node an edge endpoint is on.
func inferSide(obj *d2graph.Object, point geo.Point) string {
	if obj.TopLeft == nil {
		return ""
	}
	cx := obj.TopLeft.X + obj.Width/2
	cy := obj.TopLeft.Y + obj.Height/2
	dx := point.X - cx
	dy := point.Y - cy

	// Normalize by dimensions to compare relative position
	ndx := dx / (obj.Width / 2)
	ndy := dy / (obj.Height / 2)

	if abs(ndx) > abs(ndy) {
		if dx > 0 {
			return "right"
		}
		return "left"
	}
	if dy > 0 {
		return "bottom"
	}
	return "top"
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
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
