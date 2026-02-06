package d2widescreenlayout

import (
	"context"
	"fmt"
	"math"
	"os"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/lib/geo"
	"oss.terrastruct.com/d2/lib/label"
	"oss.terrastruct.com/util-go/go2"
)

type ConfigurableOpts struct {
	Ratio           float64 `json:"-"`
	InnerEngine     string  `json:"inner"`
	Gap             int     `json:"gap"`
	HintsPath       string  `json:"hints,omitempty"`
	LayoutStatePath string  `json:"layoutState,omitempty"`
}

var DefaultOpts = ConfigurableOpts{
	Ratio:       1.778,
	InnerEngine: "dagre",
	Gap:         60,
}

const (
	laneSpacing        = 20.0
	minDetour          = 30.0
	maxExhaustiveNodes = 20
)

func Layout(ctx context.Context, g *d2graph.Graph, opts *ConfigurableOpts) error {
	if opts == nil {
		opts = &DefaultOpts
	}

	// Load hints if path is set
	var hints *LayoutHints
	if opts.HintsPath != "" {
		var err error
		hints, err = LoadHints(opts.HintsPath)
		if err != nil {
			return err
		}
	}

	// Apply spacing override from hints
	gap := float64(opts.Gap)
	if hints != nil && hints.Spacing != nil && hints.Spacing.Gap != nil {
		gap = float64(*hints.Spacing.Gap)
	}

	// Step 1: Delegate to inner engine
	if err := runInnerEngine(ctx, g, opts.InnerEngine); err != nil {
		return err
	}

	topLevel := g.Root.ChildrenArray
	if len(topLevel) <= 1 {
		setRootDimensions(g)
		return nil
	}

	// Step 3: Compute bounding boxes for each top-level node
	bboxes := make([]*geo.Box, len(topLevel))
	for i, obj := range topLevel {
		bboxes[i] = ComputeSubtreeBBox(obj)
	}

	// Step 4: Find best row arrangement (hints can override)
	var bestArrangement arrangement
	if hints != nil && hints.Arrangement != nil && len(hints.Arrangement.Rows) > 0 {
		bestArrangement = applyArrangementHints(topLevel, hints.Arrangement)
	} else {
		bestArrangement = findBestArrangement(topLevel, bboxes, gap, opts.Ratio)
	}

	// Step 5+6: Reposition nodes and shift internal edges
	layout := repositionNodes(g, topLevel, bboxes, bestArrangement, gap, hints)

	// Step 7: Re-route cross-boundary edges
	rerouteCrossBoundaryEdges(g, topLevel, layout, hints)

	// Step 8: Set root dimensions
	setRootDimensions(g)

	// Step 9: Export layout state if path is set
	if opts.LayoutStatePath != "" {
		state := ExportLayoutState(g, layout)
		if err := WriteLayoutState(state, opts.LayoutStatePath); err != nil {
			fmt.Fprintf(os.Stderr, "widescreen: warning: failed to write layout state: %v\n", err)
		}
	}

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

// applyArrangementHints builds an arrangement from agent-provided row assignments.
// Unknown node IDs are warned and skipped; unlisted nodes go to the last row.
func applyArrangementHints(topLevel []*d2graph.Object, ah *ArrangementHints) arrangement {
	// Build name→index map
	nameToIdx := make(map[string]int, len(topLevel))
	for i, obj := range topLevel {
		nameToIdx[obj.ID] = i
	}

	placed := make(map[int]bool)
	var rows [][]int

	for _, rowNames := range ah.Rows {
		var row []int
		for _, name := range rowNames {
			idx, ok := nameToIdx[name]
			if !ok {
				fmt.Fprintf(os.Stderr, "widescreen: warning: unknown node %q in arrangement hints, skipping\n", name)
				continue
			}
			if placed[idx] {
				continue
			}
			row = append(row, idx)
			placed[idx] = true
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}

	// Append any unlisted nodes to the last row
	for i := range topLevel {
		if !placed[i] {
			if len(rows) == 0 {
				rows = append(rows, nil)
			}
			rows[len(rows)-1] = append(rows[len(rows)-1], i)
		}
	}

	return arrangement{rows: rows}
}

func findBestArrangement(topLevel []*d2graph.Object, bboxes []*geo.Box, gap, targetRatio float64) arrangement {
	n := len(topLevel)

	originalOrder := make([]int, n)
	for i := range originalOrder {
		originalOrder[i] = i
	}

	// Only use original ordering — preserves dagre's logical node placement
	if n <= maxExhaustiveNodes {
		return exhaustiveSearch(originalOrder, bboxes, gap, targetRatio)
	}
	return heuristicSearch(originalOrder, bboxes, gap, targetRatio)
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
	totalArea := 0.0

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
		totalArea += rowWidth * rowHeight
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
	ratioError := math.Abs(actualRatio - targetRatio)

	// Soft ratio error: once within ~40% of target, diminishing penalty
	// This allows compactness to dominate when ratio is "close enough"
	softRatio := math.Log1p(ratioError)

	// Compactness: penalize wasted space (bounding rect area vs actual content area)
	boundingArea := totalWidth * totalHeight
	wasteRatio := 1.0 - totalArea/boundingArea

	// Penalize having many rows with single small items
	rowCountPenalty := 0.0
	if len(rows) > 1 {
		for _, row := range rows {
			if len(row) == 1 {
				// Single-item row: penalize if the item is much smaller than the widest row
				rowWidth := bboxes[row[0]].Width
				if totalWidth > 0 && rowWidth < totalWidth*0.3 {
					rowCountPenalty += 0.5
				}
			}
		}
	}

	return softRatio + wasteRatio*1.5 + rowCountPenalty
}

// gridLayout stores the computed row/column structure after repositioning.
type gridLayout struct {
	rows       [][]int           // node indices per row
	rowTops    []float64         // Y coordinate of top of each row
	rowBottoms []float64         // Y coordinate of bottom of each row
	gap        float64           // gap between rows/columns
	nodeRow    map[*d2graph.Object]int // which row each top-level node is in
	nodeCol    map[*d2graph.Object]int // which column (position in row) each node is in
	bboxes     map[*d2graph.Object]*geo.Box // post-reposition bboxes
}

func repositionNodes(g *d2graph.Graph, topLevel []*d2graph.Object, bboxes []*geo.Box, arr arrangement, gap float64, hints *LayoutHints) *gridLayout {
	// Pre-compute deltas for each top-level node
	cursorY := 0.0
	deltas := make(map[*d2graph.Object][2]float64)
	rowWidths := make([]float64, len(arr.rows))
	rowHeights := make([]float64, len(arr.rows))
	maxRowWidth := 0.0

	for rowIdx, row := range arr.rows {
		rowHeight := 0.0
		rowWidth := 0.0
		for _, idx := range row {
			if bboxes[idx].Height > rowHeight {
				rowHeight = bboxes[idx].Height
			}
			rowWidth += bboxes[idx].Width
		}
		if len(row) > 1 {
			rowWidth += gap * float64(len(row)-1)
		}
		rowWidths[rowIdx] = rowWidth
		rowHeights[rowIdx] = rowHeight
		if rowWidth > maxRowWidth {
			maxRowWidth = rowWidth
		}
	}

	for rowIdx, row := range arr.rows {
		rowHeight := rowHeights[rowIdx]
		cursorX := (maxRowWidth - rowWidths[rowIdx]) / 2
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

	// Apply node position hints: override computed positions with agent-specified x/y
	if hints != nil && len(hints.Nodes) > 0 {
		idToObj := make(map[string]*d2graph.Object, len(topLevel))
		idToIdx := make(map[string]int, len(topLevel))
		for i, obj := range topLevel {
			idToObj[obj.ID] = obj
			idToIdx[obj.ID] = i
		}
		for nodeID, nh := range hints.Nodes {
			obj, ok := idToObj[nodeID]
			if !ok {
				fmt.Fprintf(os.Stderr, "widescreen: warning: node hint for unknown node %q\n", nodeID)
				continue
			}
			idx := idToIdx[nodeID]
			bbox := bboxes[idx]
			d := deltas[obj]
			if nh.X != nil {
				d[0] = *nh.X - bbox.TopLeft.X
			}
			if nh.Y != nil {
				d[1] = *nh.Y - bbox.TopLeft.Y
			}
			deltas[obj] = d

			// Apply MinWidth: expand node container if needed
			if nh.MinWidth != nil && obj.Width < *nh.MinWidth {
				obj.Width = *nh.MinWidth
			}
		}
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

	// Build grid layout info
	gl := &gridLayout{
		rows:    arr.rows,
		gap:     gap,
		nodeRow: make(map[*d2graph.Object]int),
		nodeCol: make(map[*d2graph.Object]int),
		bboxes:  make(map[*d2graph.Object]*geo.Box),
	}

	curY := 0.0
	for rowIdx, row := range arr.rows {
		rh := rowHeights[rowIdx]
		gl.rowTops = append(gl.rowTops, curY)
		gl.rowBottoms = append(gl.rowBottoms, curY+rh)
		for colIdx, idx := range row {
			obj := topLevel[idx]
			gl.nodeRow[obj] = rowIdx
			gl.nodeCol[obj] = colIdx
			gl.bboxes[obj] = ComputeSubtreeBBox(obj)
		}
		curY += rh + gap
	}

	return gl
}

func rerouteCrossBoundaryEdges(g *d2graph.Graph, topLevel []*d2graph.Object, gl *gridLayout, hints *LayoutHints) {
	// Build lookup: object -> top-level ancestor
	ancestorMap := make(map[*d2graph.Object]*d2graph.Object)
	for _, tl := range topLevel {
		ancestorMap[tl] = tl
		tl.IterDescendants(func(_, child *d2graph.Object) {
			ancestorMap[child] = tl
		})
	}

	// Collect cross-boundary edges
	type crossEdge struct {
		edge         *d2graph.Edge
		srcTL, dstTL *d2graph.Object
	}
	var crossEdges []crossEdge

	for _, e := range g.Edges {
		srcTL := ancestorMap[e.Src]
		dstTL := ancestorMap[e.Dst]
		if srcTL == nil || dstTL == nil || srcTL == dstTL {
			continue
		}
		crossEdges = append(crossEdges, crossEdge{e, srcTL, dstTL})
	}

	if len(crossEdges) == 0 {
		return
	}

	// Compute horizontal channel Y-coordinates (midpoints between rows)
	// hChannel[i] = Y midpoint of gap between row i and row i+1
	hChannels := make([]float64, len(gl.rows)-1)
	for i := 0; i < len(gl.rows)-1; i++ {
		hChannels[i] = (gl.rowBottoms[i] + gl.rowTops[i+1]) / 2
	}

	// Build edge hint lookup: match edges to their hints
	edgeHints := make(map[*d2graph.Edge]*EdgeHint)
	if hints != nil && len(hints.Edges) > 0 {
		// Track duplicate edge counts for index matching
		type edgePairKey struct{ src, dst string }
		edgePairCounts := make(map[edgePairKey]int)
		for _, ce := range crossEdges {
			srcAbs := ce.edge.Src.AbsID()
			dstAbs := ce.edge.Dst.AbsID()
			pk := edgePairKey{srcAbs, dstAbs}
			idx := edgePairCounts[pk]
			edgePairCounts[pk]++

			// Try matching with index first, then without
			keyWithIdx := fmt.Sprintf("%s -> %s[%d]", srcAbs, dstAbs, idx)
			keyWithout := fmt.Sprintf("%s -> %s", srcAbs, dstAbs)
			if h, ok := hints.Edges[keyWithIdx]; ok {
				edgeHints[ce.edge] = h
			} else if h, ok := hints.Edges[keyWithout]; ok && idx == 0 {
				edgeHints[ce.edge] = h
			}
		}
		// Warn about unmatched hint keys
		matched := make(map[string]bool)
		for _, h := range edgeHints {
			for k, v := range hints.Edges {
				if v == h {
					matched[k] = true
				}
			}
		}
		for k := range hints.Edges {
			if !matched[k] {
				fmt.Fprintf(os.Stderr, "widescreen: warning: unmatched edge hint key %q\n", k)
			}
		}
	}

	// Group edges by channel they'll use, to assign lane offsets
	type channelKey struct {
		channelIdx int
		direction  int // 0=horizontal channel, 1=vertical detour left, 2=vertical detour right
	}
	channelUsers := make(map[channelKey]int) // count of edges per channel

	// First pass: determine routing for each edge and count channel usage
	type edgeRouting struct {
		sameRow     bool
		srcRow      int
		dstRow      int
		channelIdx  int // which hChannel to use (-1 if same row)
		needsDetour bool
	}
	routings := make([]edgeRouting, len(crossEdges))

	for i, ce := range crossEdges {
		srcRow := gl.nodeRow[ce.srcTL]
		dstRow := gl.nodeRow[ce.dstTL]
		r := edgeRouting{srcRow: srcRow, dstRow: dstRow}

		if srcRow == dstRow {
			r.sameRow = true
			r.channelIdx = -1
		} else {
			// Use the channel between the two rows
			minRow := srcRow
			maxRow := dstRow
			if srcRow > dstRow {
				minRow, maxRow = dstRow, srcRow
			}
			// For multi-row spanning, use the channel closest to the source row
			r.channelIdx = minRow
			if minRow >= len(hChannels) {
				r.channelIdx = len(hChannels) - 1
			}

			// Check if the Z-route would cross an obstacle
			srcCenter := ce.edge.Src.Center()
			dstCenter := ce.edge.Dst.Center()
			midY := hChannels[r.channelIdx]
			testRoute := []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(srcCenter.X, midY),
				geo.NewPoint(dstCenter.X, midY),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}

			// Check if horizontal segment crosses any intermediate bboxes
			for _, tl := range topLevel {
				if tl == ce.srcTL || tl == ce.dstTL {
					continue
				}
				bbox := gl.bboxes[tl]
				if routeSegmentCrossesBBox(testRoute, bbox) {
					r.needsDetour = true
					break
				}
			}

			// Multi-row spanning: check intermediate rows too
			if maxRow-minRow > 1 {
				for midRowIdx := minRow + 1; midRowIdx < maxRow; midRowIdx++ {
					for _, nodeIdx := range gl.rows[midRowIdx] {
						bbox := gl.bboxes[topLevel[nodeIdx]]
						if routeSegmentCrossesBBox(testRoute, bbox) {
							r.needsDetour = true
							break
						}
					}
				}
			}
		}

		routings[i] = r
		if !r.sameRow {
			key := channelKey{r.channelIdx, 0}
			channelUsers[key]++
		}
	}

	// Second pass: assign lane offsets and build routes
	// Sort cross-row edges within each channel by source X to spread them cleanly
	channelLaneIdx := make(map[channelKey]int)

	for i, ce := range crossEdges {
		r := routings[i]
		e := ce.edge
		srcCenter := e.Src.Center()
		dstCenter := e.Dst.Center()
		eh := edgeHints[e]

		var route []*geo.Point

		if eh != nil && len(eh.Waypoints) > 0 {
			// Waypoint hints override all routing — use exact coordinates.
			// Prepend src center and append dst center so TraceToShape can clip properly.
			route = make([]*geo.Point, 0, len(eh.Waypoints)+2)
			route = append(route, geo.NewPoint(srcCenter.X, srcCenter.Y))
			for _, wp := range eh.Waypoints {
				route = append(route, geo.NewPoint(wp.X, wp.Y))
			}
			route = append(route, geo.NewPoint(dstCenter.X, dstCenter.Y))
		} else if r.sameRow {
			// Same-row edges: route through horizontal gap between bboxes
			srcBBox := gl.bboxes[ce.srcTL]
			dstBBox := gl.bboxes[ce.dstTL]
			srcMaxX := srcBBox.TopLeft.X + srcBBox.Width
			dstMinX := dstBBox.TopLeft.X
			if dstBBox.TopLeft.X+dstBBox.Width < srcBBox.TopLeft.X {
				srcMaxX = dstBBox.TopLeft.X + dstBBox.Width
				dstMinX = srcBBox.TopLeft.X
			}
			midX := (srcMaxX + dstMinX) / 2

			dy := dstCenter.Y - srcCenter.Y
			if math.Abs(dy) < 1 {
				route = []*geo.Point{
					geo.NewPoint(srcCenter.X, srcCenter.Y),
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
		} else if r.needsDetour || (eh != nil && eh.Side != "") {
			// Route around obstacles (or forced by side hint): go to the side
			var forceSide int
			if eh != nil && eh.Side != "" {
				switch eh.Side {
				case "left":
					forceSide = -1
				case "right":
					forceSide = 1
				default:
					fmt.Fprintf(os.Stderr, "widescreen: warning: invalid side %q for edge hint, expected \"left\" or \"right\"\n", eh.Side)
				}
			}
			route = routeAroundObstacles(srcCenter, dstCenter, 0, forceSide, collectObstacleBBoxes(topLevel, gl, ce.srcTL, ce.dstTL))
		} else {
			// Normal cross-row route through horizontal channel
			key := channelKey{r.channelIdx, 0}
			totalInChannel := channelUsers[key]
			laneIdx := channelLaneIdx[key]
			channelLaneIdx[key]++

			var laneOffset float64
			if eh := edgeHints[e]; eh != nil && eh.LaneIndex != nil {
				laneOffset = float64(*eh.LaneIndex) * laneSpacing
			} else {
				laneOffset = (float64(laneIdx) - float64(totalInChannel-1)/2) * laneSpacing
			}

			midY := hChannels[r.channelIdx] + laneOffset
			// Clamp to stay within the gap
			gapTop := gl.rowBottoms[r.channelIdx]
			gapBottom := gl.rowTops[r.channelIdx+1]
			if gapBottom-gapTop > 2 {
				midY = clamp(midY, gapTop+1, gapBottom-1)
			}

			// Spread vertical segments: offset departure/arrival X to avoid overlap
			// Use laneOffset applied horizontally to the vertical segments
			departX := srcCenter.X + laneOffset
			arriveX := dstCenter.X + laneOffset

			route = []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(departX, srcCenter.Y),
				geo.NewPoint(departX, midY),
				geo.NewPoint(arriveX, midY),
				geo.NewPoint(arriveX, dstCenter.Y),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}

			// Simplify: remove zero-length segments
			route = simplifyRoute(route)
		}

		e.Route = route
		e.IsCurve = false
		if len(e.Route) >= 2 {
			start, end := e.TraceToShape(e.Route, 0, len(e.Route)-1)
			if start <= end && end < len(e.Route) {
				e.Route = e.Route[start : end+1]
			}
		}

		if e.Label.Value != "" {
			labelPos := label.InsideMiddleCenter.String()
			if eh := edgeHints[e]; eh != nil && eh.LabelPosition != "" {
				labelPos = eh.LabelPosition
			}
			e.LabelPosition = go2.Pointer(labelPos)

			// Apply label percentage: position label along the route at a given fraction
			eh := edgeHints[e]
			if eh != nil && eh.LabelPercentage != nil && len(e.Route) >= 2 {
				pct := *eh.LabelPercentage
				if pct < 0 {
					pct = 0
				} else if pct > 1 {
					pct = 1
				}
				lp := pointAlongRoute(e.Route, pct)
				if eh.LabelOffset != nil {
					lp.X += eh.LabelOffset.X
					lp.Y += eh.LabelOffset.Y
				}
				e.LabelPosition = go2.Pointer(labelPos)
			}
		}
	}
}

// pointAlongRoute returns the point at a given fraction (0.0 to 1.0) along the route's total length.
func pointAlongRoute(route []*geo.Point, fraction float64) *geo.Point {
	if len(route) == 0 {
		return geo.NewPoint(0, 0)
	}
	if len(route) == 1 || fraction <= 0 {
		return geo.NewPoint(route[0].X, route[0].Y)
	}

	// Compute total route length
	totalLen := 0.0
	for i := 1; i < len(route); i++ {
		dx := route[i].X - route[i-1].X
		dy := route[i].Y - route[i-1].Y
		totalLen += math.Sqrt(dx*dx + dy*dy)
	}

	targetLen := fraction * totalLen
	accumulated := 0.0
	for i := 1; i < len(route); i++ {
		dx := route[i].X - route[i-1].X
		dy := route[i].Y - route[i-1].Y
		segLen := math.Sqrt(dx*dx + dy*dy)
		if accumulated+segLen >= targetLen {
			t := (targetLen - accumulated) / segLen
			return geo.NewPoint(
				route[i-1].X+t*dx,
				route[i-1].Y+t*dy,
			)
		}
		accumulated += segLen
	}
	last := route[len(route)-1]
	return geo.NewPoint(last.X, last.Y)
}

// simplifyRoute removes redundant points (zero-length segments and collinear points).
func simplifyRoute(route []*geo.Point) []*geo.Point {
	if len(route) <= 2 {
		return route
	}
	result := []*geo.Point{route[0]}
	for i := 1; i < len(route); i++ {
		prev := result[len(result)-1]
		curr := route[i]
		// Skip zero-length segments
		if math.Abs(prev.X-curr.X) < 0.5 && math.Abs(prev.Y-curr.Y) < 0.5 {
			continue
		}
		// Merge collinear segments
		if len(result) >= 2 {
			pprev := result[len(result)-2]
			sameX := math.Abs(pprev.X-prev.X) < 0.5 && math.Abs(prev.X-curr.X) < 0.5
			sameY := math.Abs(pprev.Y-prev.Y) < 0.5 && math.Abs(prev.Y-curr.Y) < 0.5
			if sameX || sameY {
				result[len(result)-1] = curr
				continue
			}
		}
		result = append(result, curr)
	}
	return result
}

// collectObstacleBBoxes returns bboxes of all top-level nodes except src and dst.
func collectObstacleBBoxes(topLevel []*d2graph.Object, gl *gridLayout, srcTL, dstTL *d2graph.Object) []*geo.Box {
	var obstacles []*geo.Box
	for _, tl := range topLevel {
		if tl != srcTL && tl != dstTL {
			obstacles = append(obstacles, gl.bboxes[tl])
		}
	}
	return obstacles
}

// routeSegmentCrossesBBox checks if any segment of a route passes through a bbox.
func routeSegmentCrossesBBox(route []*geo.Point, bbox *geo.Box) bool {
	obsMinX, obsMinY, obsMaxX, obsMaxY := bboxBounds(bbox)
	for i := 0; i < len(route)-1; i++ {
		p1, p2 := route[i], route[i+1]
		// Horizontal segment
		if math.Abs(p1.Y-p2.Y) < 1 {
			y := p1.Y
			if y > obsMinY && y < obsMaxY {
				segMinX := math.Min(p1.X, p2.X)
				segMaxX := math.Max(p1.X, p2.X)
				if segMaxX > obsMinX && segMinX < obsMaxX {
					return true
				}
			}
		}
		// Vertical segment
		if math.Abs(p1.X-p2.X) < 1 {
			x := p1.X
			if x > obsMinX && x < obsMaxX {
				segMinY := math.Min(p1.Y, p2.Y)
				segMaxY := math.Max(p1.Y, p2.Y)
				if segMaxY > obsMinY && segMinY < obsMaxY {
					return true
				}
			}
		}
	}
	return false
}

// routeAroundObstacles computes a side-route that avoids all obstacles between src and dst.
func routeAroundObstacles(srcCenter, dstCenter *geo.Point, laneOffset float64, forceSide int, obstacles []*geo.Box) []*geo.Point {
	minSrcY := math.Min(srcCenter.Y, dstCenter.Y)
	maxDstY := math.Max(srcCenter.Y, dstCenter.Y)

	// Find combined extents of obstacles in the vertical corridor
	obsMinX, obsMaxX := math.Inf(1), math.Inf(-1)
	for _, obs := range obstacles {
		oMinX, oMinY, oMaxX, oMaxY := bboxBounds(obs)
		if oMaxY <= minSrcY || oMinY >= maxDstY {
			continue
		}
		obsMinX = math.Min(obsMinX, oMinX)
		obsMaxX = math.Max(obsMaxX, oMaxX)
	}

	if math.IsInf(obsMinX, 1) {
		if forceSide == 0 {
			// No obstacles, no forced side — direct route
			return []*geo.Point{
				geo.NewPoint(srcCenter.X, srcCenter.Y),
				geo.NewPoint(dstCenter.X, dstCenter.Y),
			}
		}
		// No obstacles but forced side — use src/dst extent as reference
		minX := math.Min(srcCenter.X, dstCenter.X)
		maxX := math.Max(srcCenter.X, dstCenter.X)
		var sideX float64
		if forceSide < 0 {
			sideX = minX - minDetour + laneOffset
		} else {
			sideX = maxX + minDetour + laneOffset
		}
		return []*geo.Point{
			geo.NewPoint(srcCenter.X, srcCenter.Y),
			geo.NewPoint(sideX, srcCenter.Y),
			geo.NewPoint(sideX, dstCenter.Y),
			geo.NewPoint(dstCenter.X, dstCenter.Y),
		}
	}

	// Choose left or right side — minimize total horizontal travel
	leftX := obsMinX - minDetour
	rightX := obsMaxX + minDetour
	var sideX float64
	if forceSide < 0 {
		sideX = leftX + laneOffset
	} else if forceSide > 0 {
		sideX = rightX + laneOffset
	} else {
		leftTravel := math.Abs(srcCenter.X-leftX) + math.Abs(leftX-dstCenter.X)
		rightTravel := math.Abs(srcCenter.X-rightX) + math.Abs(rightX-dstCenter.X)
		sideX = leftX + laneOffset
		if rightTravel < leftTravel {
			sideX = rightX + laneOffset
		}
	}

	return []*geo.Point{
		geo.NewPoint(srcCenter.X, srcCenter.Y),
		geo.NewPoint(sideX, srcCenter.Y),
		geo.NewPoint(sideX, dstCenter.Y),
		geo.NewPoint(dstCenter.X, dstCenter.Y),
	}
}

func bboxBounds(b *geo.Box) (minX, minY, maxX, maxY float64) {
	minX = b.TopLeft.X
	minY = b.TopLeft.Y
	maxX = minX + b.Width
	maxY = minY + b.Height
	return minX, minY, maxX, maxY
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
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
	srcCenter := e.Src.Center()
	dstCenter := e.Dst.Center()
	e.Route = BuildOrthogonalRoute(srcCenter, dstCenter, laneOffset)
	e.IsCurve = false
	start, end := e.TraceToShape(e.Route, 0, len(e.Route)-1)
	e.Route = e.Route[start : end+1]
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
	g.Root.TopLeft = geo.NewPoint(minX, minY)
	g.Root.Width = maxX - minX
	g.Root.Height = maxY - minY
}
