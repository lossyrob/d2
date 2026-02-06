package d2widescreenlayout

import (
	"context"
	"fmt"
	"math"
	"sort"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/lib/geo"
	"oss.terrastruct.com/d2/lib/label"
	"oss.terrastruct.com/util-go/go2"
)

type ConfigurableOpts struct {
	Ratio       float64 `json:"-"`
	InnerEngine string  `json:"inner"`
	Gap         int     `json:"gap"`
}

var DefaultOpts = ConfigurableOpts{
	Ratio:       1.778,
	InnerEngine: "dagre",
	Gap:         60,
}

const (
	laneSpacing = 20.0
	minDetour   = 30.0
	maxExhaustiveNodes = 20
)

func Layout(ctx context.Context, g *d2graph.Graph, opts *ConfigurableOpts) error {
	if opts == nil {
		opts = &DefaultOpts
	}

	// Step 1: Delegate to inner engine
	if err := runInnerEngine(ctx, g, opts.InnerEngine); err != nil {
		return err
	}

	topLevel := g.Root.ChildrenArray
	if len(topLevel) <= 1 {
		return nil
	}

	gap := float64(opts.Gap)

	// Step 3: Compute bounding boxes for each top-level node
	bboxes := make([]*geo.Box, len(topLevel))
	for i, obj := range topLevel {
		bboxes[i] = ComputeSubtreeBBox(obj)
	}

	// Step 4: Find best row arrangement
	bestArrangement := findBestArrangement(topLevel, bboxes, gap, opts.Ratio)

	// Step 5+6: Reposition nodes and shift internal edges
	repositionNodes(g, topLevel, bboxes, bestArrangement, gap)

	// Step 7: Re-route cross-boundary edges
	rerouteCrossBoundaryEdges(g, topLevel)

	// Step 8: Set root dimensions
	setRootDimensions(g)

	return nil
}

func runInnerEngine(ctx context.Context, g *d2graph.Graph, engine string) error {
	switch engine {
	case "dagre":
		opts := d2dagrelayout.DefaultOpts
		return d2dagrelayout.Layout(ctx, g, &opts)
	case "elk":
		opts := d2elklayout.DefaultOpts
		return d2elklayout.Layout(ctx, g, &opts)
	default:
		return fmt.Errorf("unsupported inner layout engine: %q (supported: dagre, elk)", engine)
	}
}

// ComputeSubtreeBBox computes the bounding box enclosing an object and all its descendants.
func ComputeSubtreeBBox(obj *d2graph.Object) *geo.Box {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	var visit func(o *d2graph.Object)
	visit = func(o *d2graph.Object) {
		if o.TopLeft == nil {
			return
		}
		minX = math.Min(minX, o.TopLeft.X)
		minY = math.Min(minY, o.TopLeft.Y)
		maxX = math.Max(maxX, o.TopLeft.X+o.Width)
		maxY = math.Max(maxY, o.TopLeft.Y+o.Height)
		for _, child := range o.ChildrenArray {
			visit(child)
		}
	}
	visit(obj)

	if math.IsInf(minX, 1) {
		return geo.NewBox(geo.NewPoint(0, 0), 0, 0)
	}
	return geo.NewBox(geo.NewPoint(minX, minY), maxX-minX, maxY-minY)
}

// arrangement represents a row grouping: which nodes go in which row.
type arrangement struct {
	rows  [][]int // each row is a list of node indices
	score float64
}

func findBestArrangement(topLevel []*d2graph.Object, bboxes []*geo.Box, gap, targetRatio float64) arrangement {
	n := len(topLevel)

	// Try two orderings: original and width-descending
	originalOrder := make([]int, n)
	for i := range originalOrder {
		originalOrder[i] = i
	}

	widthSorted := make([]int, n)
	copy(widthSorted, originalOrder)
	sort.Slice(widthSorted, func(i, j int) bool {
		return bboxes[widthSorted[i]].Width > bboxes[widthSorted[j]].Width
	})

	orderings := [][]int{originalOrder, widthSorted}

	best := arrangement{score: math.Inf(1)}
	for _, order := range orderings {
		var candidate arrangement
		if n <= maxExhaustiveNodes {
			candidate = exhaustiveSearch(order, bboxes, gap, targetRatio)
		} else {
			candidate = heuristicSearch(order, bboxes, gap, targetRatio)
		}
		if candidate.score < best.score {
			best = candidate
		}
	}
	return best
}

// exhaustiveSearch tries all 2^(n-1) cut combinations for small n.
func exhaustiveSearch(order []int, bboxes []*geo.Box, gap, targetRatio float64) arrangement {
	n := len(order)
	best := arrangement{score: math.Inf(1)}

	// Each bit in mask represents whether there's a row break after position i
	combinations := 1 << (n - 1)
	for mask := 0; mask < combinations; mask++ {
		rows := buildRowsFromMask(order, mask)
		score := ScoreArrangement(rows, bboxes, gap, targetRatio)
		if score < best.score {
			best = arrangement{rows: rows, score: score}
		}
	}
	return best
}

// heuristicSearch for large n: try evenly-spaced and greedy-fill for each row count.
func heuristicSearch(order []int, bboxes []*geo.Box, gap, targetRatio float64) arrangement {
	n := len(order)
	best := arrangement{score: math.Inf(1)}

	totalWidth := 0.0
	for _, idx := range order {
		totalWidth += bboxes[idx].Width
	}

	for numRows := 1; numRows <= n; numRows++ {
		// Strategy A: evenly spaced
		rows := evenSplit(order, numRows)
		score := ScoreArrangement(rows, bboxes, gap, targetRatio)
		if score < best.score {
			best = arrangement{rows: rows, score: score}
		}

		// Strategy B: greedy fill constrained to numRows
		targetRowWidth := (totalWidth + float64(n-1)*gap) / float64(numRows)
		rows = greedyFill(order, bboxes, gap, targetRowWidth, numRows)
		score = ScoreArrangement(rows, bboxes, gap, targetRatio)
		if score < best.score {
			best = arrangement{rows: rows, score: score}
		}
	}
	return best
}

func buildRowsFromMask(order []int, mask int) [][]int {
	var rows [][]int
	currentRow := []int{order[0]}
	for i := 1; i < len(order); i++ {
		if mask&(1<<(i-1)) != 0 {
			rows = append(rows, currentRow)
			currentRow = []int{order[i]}
		} else {
			currentRow = append(currentRow, order[i])
		}
	}
	rows = append(rows, currentRow)
	return rows
}

func evenSplit(order []int, numRows int) [][]int {
	n := len(order)
	perRow := int(math.Ceil(float64(n) / float64(numRows)))
	var rows [][]int
	for i := 0; i < n; i += perRow {
		end := i + perRow
		if end > n {
			end = n
		}
		row := make([]int, end-i)
		copy(row, order[i:end])
		rows = append(rows, row)
	}
	return rows
}

func greedyFill(order []int, bboxes []*geo.Box, gap, targetRowWidth float64, maxRows int) [][]int {
	var rows [][]int
	currentRow := []int{order[0]}
	currentWidth := bboxes[order[0]].Width

	for i := 1; i < len(order); i++ {
		w := bboxes[order[i]].Width
		newWidth := currentWidth + gap + w

		// Start new row if exceeding target, unless we'd exceed maxRows
		if newWidth > targetRowWidth && len(rows)+1 < maxRows {
			rows = append(rows, currentRow)
			currentRow = []int{order[i]}
			currentWidth = w
		} else {
			currentRow = append(currentRow, order[i])
			currentWidth = newWidth
		}
	}
	rows = append(rows, currentRow)
	return rows
}

func ScoreArrangement(rows [][]int, bboxes []*geo.Box, gap, targetRatio float64) float64 {
	totalWidth := 0.0
	totalHeight := 0.0
	for i, row := range rows {
		rowWidth := 0.0
		rowHeight := 0.0
		for j, idx := range row {
			rowWidth += bboxes[idx].Width
			if j > 0 {
				rowWidth += gap
			}
			if bboxes[idx].Height > rowHeight {
				rowHeight = bboxes[idx].Height
			}
		}
		if rowWidth > totalWidth {
			totalWidth = rowWidth
		}
		totalHeight += rowHeight
		if i > 0 {
			totalHeight += gap
		}
	}
	if totalHeight == 0 {
		return math.Inf(1)
	}
	actualRatio := totalWidth / totalHeight
	return math.Abs(actualRatio - targetRatio)
}

func repositionNodes(g *d2graph.Graph, topLevel []*d2graph.Object, bboxes []*geo.Box, arr arrangement, gap float64) {
	// Pre-compute deltas for each top-level node
	cursorY := 0.0
	deltas := make(map[*d2graph.Object][2]float64)

	for _, row := range arr.rows {
		rowHeight := 0.0
		for _, idx := range row {
			if bboxes[idx].Height > rowHeight {
				rowHeight = bboxes[idx].Height
			}
		}

		cursorX := 0.0
		for _, idx := range row {
			obj := topLevel[idx]
			bbox := bboxes[idx]

			dx := cursorX - bbox.TopLeft.X
			dy := cursorY - bbox.TopLeft.Y
			deltas[obj] = [2]float64{dx, dy}

			cursorX += bbox.Width + gap
		}
		cursorY += rowHeight + gap
	}

	// Apply deltas: move objects and their internal edges
	for _, obj := range topLevel {
		d, ok := deltas[obj]
		if !ok {
			continue
		}
		dx, dy := d[0], d[1]
		if dx == 0 && dy == 0 {
			continue
		}

		obj.MoveWithDescendants(dx, dy)

		// Move edges where both endpoints are descendants of this top-level node
		for _, e := range g.Edges {
			if e.Src.IsDescendantOf(obj) && e.Dst.IsDescendantOf(obj) {
				e.Move(dx, dy)
			}
		}
	}
}

func rerouteCrossBoundaryEdges(g *d2graph.Graph, topLevel []*d2graph.Object) {
	// Build lookup: object -> top-level ancestor
	ancestorMap := make(map[*d2graph.Object]*d2graph.Object)
	for _, tl := range topLevel {
		ancestorMap[tl] = tl
		tl.IterDescendants(func(_, child *d2graph.Object) {
			ancestorMap[child] = tl
		})
	}

	// Group cross-boundary edges by (srcTopLevel, dstTopLevel) pair
	type edgePairKey struct{ src, dst *d2graph.Object }
	edgeGroups := make(map[edgePairKey][]*d2graph.Edge)

	for _, e := range g.Edges {
		srcTL := ancestorMap[e.Src]
		dstTL := ancestorMap[e.Dst]
		if srcTL == nil || dstTL == nil || srcTL == dstTL {
			continue
		}
		key := edgePairKey{srcTL, dstTL}
		edgeGroups[key] = append(edgeGroups[key], e)
	}

	// Route each group
	for _, edges := range edgeGroups {
		for laneIdx, e := range edges {
			laneOffset := float64(laneIdx) * laneSpacing
			routeOrthogonal(e, laneOffset)
		}
	}
}

func routeOrthogonal(e *d2graph.Edge, laneOffset float64) {
	srcCenter := e.Src.Center()
	dstCenter := e.Dst.Center()

	dx := dstCenter.X - srcCenter.X
	dy := dstCenter.Y - srcCenter.Y

	var route []*geo.Point

	if math.Abs(dx) > math.Abs(dy) {
		// Primarily horizontal: Z-shape H→V→H
		midX := (srcCenter.X + dstCenter.X) / 2 + laneOffset
		if math.Abs(dy) < 1 {
			// Degenerate: same row. Force vertical detour.
			detourY := srcCenter.Y - (minDetour + laneOffset)
			route = []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(srcCenter.X, detourY),
				geo.NewPoint(dstCenter.X, detourY),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}
		} else {
			route = []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(midX, srcCenter.Y),
				geo.NewPoint(midX, dstCenter.Y),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}
		}
	} else {
		// Primarily vertical: Z-shape V→H→V
		midY := (srcCenter.Y + dstCenter.Y) / 2 + laneOffset
		if math.Abs(dx) < 1 {
			// Degenerate: same column. Force horizontal detour.
			detourX := srcCenter.X - (minDetour + laneOffset)
			route = []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(detourX, srcCenter.Y),
				geo.NewPoint(detourX, dstCenter.Y),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}
		} else {
			route = []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(srcCenter.X, midY),
				geo.NewPoint(dstCenter.X, midY),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}
		}
	}

	e.Route = route
	e.IsCurve = false
	e.TraceToShape(e.Route, 0, len(e.Route)-1)

	if e.Label.Value != "" {
		e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
	}
}

// findTopLevelAncestor walks the Parent chain to find the direct child of root.
func FindTopLevelAncestor(obj *d2graph.Object, root *d2graph.Object) *d2graph.Object {
	if obj == nil || obj.Parent == nil {
		return nil
	}
	if obj.Parent == root {
		return obj
	}
	return FindTopLevelAncestor(obj.Parent, root)
}

// RouteOrthogonalExported is an exported wrapper for testing.
func RouteOrthogonalExported(e *d2graph.Edge, laneOffset float64) {
	routeOrthogonal(e, laneOffset)
}

// BuildOrthogonalRoute builds an orthogonal route between two center points, without
// applying TraceToShape. Exported for testing the routing algorithm independently.
func BuildOrthogonalRoute(srcCenter, dstCenter *geo.Point, laneOffset float64) []*geo.Point {
	dx := dstCenter.X - srcCenter.X
	dy := dstCenter.Y - srcCenter.Y

	if math.Abs(dx) > math.Abs(dy) {
		midX := (srcCenter.X+dstCenter.X)/2 + laneOffset
		if math.Abs(dy) < 1 {
			detourY := srcCenter.Y - (minDetour + laneOffset)
			return []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(srcCenter.X, detourY),
				geo.NewPoint(dstCenter.X, detourY),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}
		}
		return []*geo.Point{
			geo.NewPoint(srcCenter.X, srcCenter.Y),
			geo.NewPoint(midX, srcCenter.Y),
			geo.NewPoint(midX, dstCenter.Y),
			geo.NewPoint(dstCenter.X, dstCenter.Y),
		}
	}

	midY := (srcCenter.Y+dstCenter.Y)/2 + laneOffset
	if math.Abs(dx) < 1 {
		detourX := srcCenter.X - (minDetour + laneOffset)
		return []*geo.Point{
			geo.NewPoint(srcCenter.X, srcCenter.Y),
			geo.NewPoint(detourX, srcCenter.Y),
			geo.NewPoint(detourX, dstCenter.Y),
			geo.NewPoint(dstCenter.X, dstCenter.Y),
		}
	}
	return []*geo.Point{
		geo.NewPoint(srcCenter.X, srcCenter.Y),
		geo.NewPoint(srcCenter.X, midY),
		geo.NewPoint(dstCenter.X, midY),
		geo.NewPoint(dstCenter.X, dstCenter.Y),
	}
}

func setRootDimensions(g *d2graph.Graph) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)

	for _, obj := range g.Objects {
		if obj.TopLeft == nil {
			continue
		}
		minX = math.Min(minX, obj.TopLeft.X)
		minY = math.Min(minY, obj.TopLeft.Y)
		maxX = math.Max(maxX, obj.TopLeft.X+obj.Width)
		maxY = math.Max(maxY, obj.TopLeft.Y+obj.Height)
	}

	// Include edge route points
	for _, e := range g.Edges {
		for _, p := range e.Route {
			minX = math.Min(minX, p.X)
			minY = math.Min(minY, p.Y)
			maxX = math.Max(maxX, p.X)
			maxY = math.Max(maxY, p.Y)
		}
	}

	if math.IsInf(minX, 1) {
		return
	}
	g.Root.Width = maxX - minX
	g.Root.Height = maxY - minY
}
