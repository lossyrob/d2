package d2widescreenlayout

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// LayoutHints represents agent-provided hints for the widescreen layout engine.
// Loaded from a sidecar JSON file (<input>.d2.hints.json).
type LayoutHints struct {
	Arrangement *ArrangementHints      `json:"arrangement,omitempty"`
	Edges       map[string]*EdgeHint   `json:"edges,omitempty"`
	Nodes       map[string]*NodeHint   `json:"nodes,omitempty"`
	Spacing     *SpacingHints          `json:"spacing,omitempty"`
}

// ArrangementHints overrides the automatic row arrangement algorithm.
// Accepts two JSON formats:
//   - Flat array: ["A", "B", "C"] — treated as a single-row ordering
//   - Struct with rows: {"rows": [["A", "B"], ["C", "D"]]}
type ArrangementHints struct {
	// Rows specifies which top-level node IDs go in each row.
	// Nodes not listed are appended to the last row.
	Rows [][]string `json:"rows"`
	// ExplicitRows is true when the user specified explicit row groupings.
	// When false (flat array shorthand), the engine may auto-break extreme rows.
	ExplicitRows bool `json:"-"`
}

// UnmarshalJSON allows ArrangementHints to accept either a flat string array
// (single-row shorthand) or the full {"rows": [...]} struct format.
func (ah *ArrangementHints) UnmarshalJSON(data []byte) error {
	// Try flat array first: ["A", "B", "C"]
	var flat []string
	if err := json.Unmarshal(data, &flat); err == nil {
		ah.Rows = [][]string{flat}
		ah.ExplicitRows = false
		return nil
	}

	// Try struct format: {"rows": [["A", "B"], ["C", "D"]]}
	type rawArrangement struct {
		Rows [][]string `json:"rows"`
	}
	var raw rawArrangement
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("arrangement must be either [\"A\",\"B\",\"C\"] (flat array) or {\"rows\": [[\"A\",\"B\"],[\"C\"]]} (struct): %w", err)
	}
	ah.Rows = raw.Rows
	ah.ExplicitRows = true
	return nil
}

// EdgeHint provides per-edge routing overrides.
type EdgeHint struct {
	// Side forces obstacle-avoidance direction: "left" or "right".
	Side string `json:"side,omitempty"`
	// LaneIndex is an integer lane offset (multiplied by laneSpacing).
	// 0=center, -1=one lane left, 1=one lane right.
	LaneIndex *int `json:"laneIndex,omitempty"`
	// LabelPosition overrides the edge label position string.
	LabelPosition string `json:"labelPosition,omitempty"`
	// LabelPercentage overrides where along the route the label is placed (0.0=start, 1.0=end).
	LabelPercentage *float64 `json:"labelPercentage,omitempty"`
	// LabelOffset shifts the label from the route by the given x/y pixel amounts.
	LabelOffset *WaypointHint `json:"labelOffset,omitempty"`
	// Waypoints specifies exact route bend points as absolute coordinates.
	// When set, all other routing hints (side, laneIndex) are ignored.
	// TraceToShape clips endpoints to shape borders automatically.
	Waypoints []WaypointHint `json:"waypoints,omitempty"`
}

// NodeHint provides per-node position and size overrides.
type NodeHint struct {
	// X overrides the node's left X coordinate (absolute pixels).
	X *float64 `json:"x,omitempty"`
	// Y overrides the node's top Y coordinate (absolute pixels).
	Y *float64 `json:"y,omitempty"`
	// MinWidth sets a minimum width for the node container.
	MinWidth *float64 `json:"minWidth,omitempty"`
	// ChildDirection overrides how children inside this container are arranged.
	// Values: "horizontal" (side-by-side), "vertical" (stacked, default).
	// Applied before the inner engine runs by setting D2's direction attribute.
	ChildDirection string `json:"childDirection,omitempty"`
	// ChildOrder specifies explicit left-to-right ordering of children by ID.
	// Used with childDirection "horizontal" to override dagre's edge-based ranking.
	// Children not listed are appended at the end in their original order.
	ChildOrder []string `json:"childOrder,omitempty"`
}

// WaypointHint is an absolute coordinate point for edge routing.
type WaypointHint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// SpacingHints overrides layout spacing.
// Accepts "gap" for uniform spacing, or "horizontal"/"vertical" for independent control.
// If horizontal/vertical are set, they take precedence over gap.
type SpacingHints struct {
	Gap        *int `json:"gap,omitempty"`
	Horizontal *int `json:"horizontal,omitempty"`
	Vertical   *int `json:"vertical,omitempty"`
}

// LoadHints reads and parses a hints JSON file. Returns nil if the file does not exist.
func LoadHints(path string) (*LayoutHints, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading hints file: %w", err)
	}

	var hints LayoutHints
	if err := json.Unmarshal(data, &hints); err != nil {
		return nil, fmt.Errorf("parsing hints file %s: %w", path, err)
	}

	return &hints, nil
}

// edgeKeyPattern matches "src -> dst" or "src -> dst[index]"
var edgeKeyPattern = regexp.MustCompile(`^(.+?)\s*->\s*(.+?)(?:\[(\d+)\])?$`)

// ParseEdgeKey parses an edge hint key like "a.b -> c.d" or "a -> b[1]".
// Returns source path, destination path, and edge index (0 if not specified).
func ParseEdgeKey(key string) (src, dst string, index int, err error) {
	matches := edgeKeyPattern.FindStringSubmatch(strings.TrimSpace(key))
	if matches == nil {
		return "", "", 0, fmt.Errorf("invalid edge key %q: expected format \"src -> dst\" or \"src -> dst[index]\"", key)
	}

	src = strings.TrimSpace(matches[1])
	dst = strings.TrimSpace(matches[2])

	if matches[3] != "" {
		index, err = strconv.Atoi(matches[3])
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid edge index in %q: %w", key, err)
		}
	}

	return src, dst, index, nil
}

// GraphDataKeyHints is the key used in d2graph.Graph.Data to store layout hints.
const GraphDataKeyHints = "widescreen-hints"
