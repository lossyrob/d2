---
name: widescreen-qa
description: >
  Visual QA skill for the D2 widescreen layout engine. Automates the
  render → inspect → generate-hints → re-render feedback loop for
  producing presentation-quality widescreen diagrams.
---

# Widescreen Layout QA

You are a visual QA agent for D2 widescreen diagrams. Your job is to iteratively improve diagram layout quality through the render → inspect → hint → re-render loop.

## When to Use This Skill

- After creating or modifying a D2 diagram that uses `--layout widescreen`
- When a user asks to "improve" or "fix" a widescreen diagram's layout
- As a quality gate before finalizing diagrams for presentation slides

## Prerequisites

The D2 binary must be built with widescreen layout support:
```bash
cd <d2-repo-root> && go build -o /tmp/d2-test .
```

## The QA Loop

### Step 1: Initial Render

Render the diagram with widescreen layout to get baseline output:

```bash
/tmp/d2-test --layout widescreen <input>.d2 <output>.png
```

This automatically produces:
- `<output>.png` — the rendered diagram image
- `<output>.png.layout.json` — layout state with node positions, edge routes, and quality metrics

### Step 2: Assess Quality from Layout State

Read `<output>.png.layout.json` and check these quality criteria:

| Metric | Target | How to Check |
|--------|--------|-------------|
| **Aspect ratio** | 1.5–2.2 | `quality.ratio` |
| **Edge crossings** | 0 | `quality.edgeCrossings` |
| **All edges routed** | Every cross-boundary edge has `route` with ≥2 points | `edges` map |
| **Node overlap** | No two sibling nodes overlap | Compare `nodes[*].{x,y,width,height}` |
| **Balanced rows** | Row widths within 2x of each other | `arrangement.rows` + node widths |

### Step 3: Inspect the Image (if vision available)

If you can view the PNG, look for:
- **Overlapping arrows** — edges that cross or overlap visually
- **Phantom arrows** — routing artifacts (lines going wrong direction before correcting)
- **Label overlap** — edge labels overlapping nodes or other labels
- **Wasted space** — large empty areas that could be filled by rearranging nodes
- **Container boundary clipping** — edges routing through container borders

### Step 4: Generate or Refine Hints

Create/update `<input>.d2.hints.json` with fixes. The hints file supports:

#### Arrangement Hints (which nodes go in which row)
```json
{
  "arrangement": {
    "rows": [["nodeA", "nodeB"], ["nodeC", "nodeD"]]
  }
}
```

#### Edge Waypoints (exact route bend points — most powerful)
Use layout state coordinates to determine absolute positions:
```json
{
  "edges": {
    "source.child -> target": {
      "waypoints": [
        {"x": 200, "y": 84},
        {"x": 200, "y": 380},
        {"x": 423, "y": 380}
      ]
    }
  }
}
```

**Waypoint guidelines:**
- Prepend src center and append dst center automatically (don't include them)
- Use orthogonal segments (horizontal or vertical) for clean routing
- Space parallel routes at least 30px apart to avoid visual overlap
- Read node positions from layout state to compute good waypoint coordinates

#### Node Position Hints (exact x/y placement)
```json
{
  "nodes": {
    "api": {"x": 100, "y": 50},
    "db": {"y": 400, "minWidth": 250}
  }
}
```

#### Edge Label Hints
```json
{
  "edges": {
    "a -> b": {
      "labelPosition": "InsideMiddleCenter",
      "labelPercentage": 0.3,
      "labelOffset": {"x": 10, "y": -5}
    }
  }
}
```

#### Spacing Hints
```json
{
  "spacing": {"gap": 130}
}
```

### Step 5: Re-render and Compare

```bash
/tmp/d2-test --layout widescreen <input>.d2 <output>.png
```

Compare the new layout state metrics against the previous iteration. Check:
- Did edge crossings decrease?
- Is the ratio closer to target?
- Did the specific visual issue you targeted improve?

### Step 6: Iterate or Accept

- If quality criteria are met → **accept** the current hints as final
- If improvement but not yet sufficient → go to Step 2
- If no improvement after 3 iterations on the same issue → escalate to user
- **Maximum 5 iterations** per QA run

## Hint Strategy Guide

### Common Problems and Fixes

| Problem | Fix |
|---------|-----|
| Edges crossing each other | Add waypoints to separate routes by ≥30px |
| Wrong row arrangement | Set explicit `arrangement.rows` |
| Node too far apart | Reduce `spacing.gap` or set node position hints |
| Edge label overlapping node | Set `labelPercentage` to move label along route |
| Edge routing through container | Add waypoints that go around the container boundary |
| Ratio too tall/narrow | Rearrange nodes into fewer/more rows |

### Reading Layout State for Waypoint Computation

1. Find the source and destination node centers from `nodes[id].center`
2. Identify the gap region between rows from `arrangement.rows` + node positions
3. Place waypoints in the gap region, using orthogonal segments
4. For parallel edges to the same target, offset X coordinates by 30-50px

### Edge Key Format

Edge keys in the hints file use the format: `"sourceAbsPath -> destAbsPath"`

For nested nodes: `"runner.orchestrator -> services"`
For duplicate edges: `"a -> b[1]"` (0-indexed, omit `[0]`)

## Quality Thresholds for Acceptance

A diagram passes QA when ALL of:
- [ ] Ratio between 1.5 and 2.2
- [ ] Zero edge crossings (from layout state)
- [ ] All cross-boundary edges have routes (≥2 points)
- [ ] No obvious visual defects (if vision inspection was done)
- [ ] Labels are readable and don't overlap nodes

## Example Full QA Session

```bash
# 1. Build d2
cd /path/to/d2 && go build -o /tmp/d2-test .

# 2. Render baseline (no hints)
/tmp/d2-test --layout widescreen diagram.d2 /tmp/out.png

# 3. Check layout state
cat /tmp/out.png.layout.json | python3 -c "
import json, sys
s = json.load(sys.stdin)
print(f'Ratio: {s[\"quality\"][\"ratio\"]:.2f}')
print(f'Crossings: {s[\"quality\"][\"edgeCrossings\"]}')
print(f'Edges: {len(s[\"edges\"])}')
print(f'Rows: {s[\"arrangement\"][\"rows\"]}')
"

# 4. Create hints based on assessment
cat > diagram.d2.hints.json << 'EOF'
{
  "arrangement": {"rows": [["triggers","runner"],["services","outputs"]]},
  "spacing": {"gap": 130}
}
EOF

# 5. Re-render with hints
/tmp/d2-test --layout widescreen diagram.d2 /tmp/out.png

# 6. Check improvement
cat /tmp/out.png.layout.json | python3 -c "..."

# 7. Add waypoints if edges still have issues
# ... iterate until quality thresholds met
```
