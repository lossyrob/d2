---
name: widescreen-qa
description: >
  Visual QA skill for the D2 widescreen layout engine. Generates test
  diagrams, renders them, inspects output, generates hints, and iterates
  until all diagrams meet presentation quality. Copies PNGs to the
  working directory for user monitoring.
---

# Widescreen Layout QA

You are a visual QA agent for D2 widescreen diagrams. Your job is to generate architecturally realistic test diagrams, render them, and iteratively improve layout quality through the render → inspect → hint → re-render loop until ALL diagrams pass quality thresholds.

## When to Use This Skill

- After creating or modifying a D2 diagram that uses `--layout widescreen`
- When a user asks to "improve" or "fix" a widescreen diagram's layout
- As a quality gate before finalizing diagrams for presentation slides
- When asked to "test the widescreen layout" or "QA the layout engine"
- When asked to generate N test diagrams and iterate until all are good

## Prerequisites

The D2 binary must be built with widescreen layout support:
```bash
cd <d2-repo-root> && go build -o /tmp/d2-test .
```

## Invocation Modes

### Mode 1: QA a Specific Diagram
User provides a `.d2` file → run QA loop on that single file.

### Mode 2: Generate and QA N Diagrams
User asks for a number of test diagrams (e.g., "generate 5 diagrams and QA them all"). You:
1. Generate N architecturally distinct diagrams (see Diagram Generation Guide)
2. Run the QA loop on each one
3. Continue iterating until ALL N diagrams pass quality thresholds
4. Report final status for each diagram

## Output: PNGs in Working Directory

**CRITICAL**: Always copy rendered PNGs into the current working directory so the user can monitor progress visually:

```bash
# After each render, copy to working directory with descriptive names
cp /tmp/diagramN.png ./diagramN-widescreen-iter1.png

# After final acceptance, copy the final version with a clean name
cp /tmp/diagramN.png ./diagramN-widescreen-final.png
```

**Naming convention**: `<name>-widescreen-iter<N>.png` during iteration, `<name>-widescreen-final.png` when accepted.

Remove intermediate iteration PNGs after acceptance to keep the directory clean — only keep the final versions.

---

## Diagram Generation Guide

When generating test diagrams, create architecturally realistic systems that exercise different layout challenges. Each diagram should be **distinct** in structure and purpose.

### Diversity Requirements

Generate diagrams that vary across these dimensions:

| Dimension | Variations to Cover |
|-----------|-------------------|
| **Topology** | Hub-and-spoke, pipeline/chain, mesh, tree, layered |
| **Container depth** | Flat (no containers), 1 level, 2+ levels nested |
| **Edge patterns** | Fan-out (1→many), fan-in (many→1), cross-boundary, self-loops |
| **Node count** | Small (3-4), medium (5-8), large (9-12) top-level nodes |
| **Edge density** | Sparse (N edges), moderate (1.5N), dense (2N+) |
| **Label presence** | Labeled edges, unlabeled edges, mix |

### Architectural Archetypes

Use these as starting points — adapt and combine them for realistic architectures:

1. **Microservices API** — API gateway → multiple services → shared database + cache. Tests fan-out and shared dependencies.
2. **CI/CD Pipeline** — Source → Build → Test → Deploy stages with feedback loops. Tests linear flow with branches.
3. **Event-Driven System** — Producers → Message Queue → Multiple consumers → Storage. Tests hub-and-spoke.
4. **Cloud Infrastructure** — VPC with subnets containing compute/storage, load balancers, and cross-VPC peering. Tests deep nesting.
5. **Data Pipeline** — Ingestion → Processing (multiple parallel stages) → Aggregation → Output. Tests parallel lanes merging.
6. **Auth/Identity System** — Client → Auth Server → Identity Provider, Token Store, User DB, Audit Log. Tests many cross-boundary edges.
7. **Monitoring Stack** — Services emitting to collectors → Aggregators → Storage → Dashboards/Alerts. Tests layered flow.

### Diagram Generation Rules

1. **Use real-world names** — "API Gateway", "PostgreSQL", "Kafka Broker", not "Node A", "Node B"
2. **Include meaningful edge labels** — "authenticates", "queries", "publishes", not "connects"
3. **Use D2 features** — containers, shapes (hexagon, cylinder, queue), `style.multiple: true` for replicas
4. **Set `direction: right`** — all diagrams should use horizontal flow for widescreen
5. **Include at least one container** with internal nodes and edges to test nested layout preservation
6. **Include at least 2 cross-boundary edges** — these are what the widescreen engine re-routes
7. **Vary complexity** — don't make all diagrams the same size/density

### Example Generated Diagram

```d2
direction: right

ingestion: Data Ingestion {
  kafka: Kafka Broker { shape: queue }
  schema: Schema Registry
  kafka -> schema: validates
}

processing: Stream Processing {
  flink: Flink Cluster { style.multiple: true }
  state: State Store { shape: cylinder }
  flink -> state: checkpoints
}

storage: Data Lake {
  s3: Object Storage { shape: cylinder }
  catalog: Data Catalog
  s3 -> catalog: registers
}

ingestion.kafka -> processing.flink: streams
processing.flink -> storage.s3: writes
processing.state -> storage.catalog: updates metadata
```

---

## The QA Loop

Run this loop for EACH diagram. Track progress across all diagrams.

### Step 1: Render

```bash
/tmp/d2-test --layout widescreen /tmp/diagramN.d2 /tmp/diagramN.png
cp /tmp/diagramN.png ./diagramN-widescreen-iter1.png
```

This automatically produces:
- `/tmp/diagramN.png` — the rendered diagram image
- `/tmp/diagramN.png.layout.json` — layout state with positions, routes, and quality metrics

### Step 2: Assess Quality from Layout State

Read the layout state JSON and check:

| Metric | Target | How to Check |
|--------|--------|-------------|
| **Aspect ratio** | 1.5–2.2 | `quality.ratio` |
| **Edge crossings** | 0 | `quality.edgeCrossings` |
| **All edges routed** | Every cross-boundary edge has `route` ≥2 points | `edges` map |
| **Node overlap** | No two sibling nodes overlap | Compare `nodes[*].{x,y,width,height}` |
| **Balanced rows** | Row widths within 2x of each other | `arrangement.rows` + node widths |

Quick check script:
```bash
python3 -c "
import json, sys
s = json.load(open('/tmp/diagramN.png.layout.json'))
r = s['quality']['ratio']
c = s['quality']['edgeCrossings']
e = len(s['edges'])
routed = sum(1 for v in s['edges'].values() if len(v.get('route',[])) >= 2)
print(f'Ratio: {r:.2f} {\"✓\" if 1.5 <= r <= 2.2 else \"✗\"}')
print(f'Crossings: {c} {\"✓\" if c == 0 else \"✗\"}')
print(f'Edges routed: {routed}/{e} {\"✓\" if routed == e else \"✗\"}')
print(f'PASS' if (1.5 <= r <= 2.2 and c == 0 and routed == e) else 'NEEDS WORK')
"
```

### Step 3: Inspect the Image

Look at the PNG you copied to the working directory. Check for:
- **Overlapping arrows** — edges that cross or visually merge
- **Phantom arrows** — routing artifacts (lines going wrong direction before correcting)
- **Label overlap** — edge labels overlapping nodes or other labels
- **Wasted space** — large empty areas that could be filled by rearranging
- **Container boundary clipping** — edges routing through container borders
- **Professional appearance** — would this look good on a presentation slide?

### Step 4: Generate or Refine Hints

Create/update `/tmp/diagramN.d2.hints.json`. Available hints:

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
- Src center and dst center are prepended/appended automatically — don't include them
- Use orthogonal segments (horizontal or vertical) for clean routing
- Space parallel routes at least 30px apart to avoid visual overlap
- Read node positions from layout state to compute good waypoint coordinates
- Route waypoints through the gap region between rows, not through nodes

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
/tmp/d2-test --layout widescreen /tmp/diagramN.d2 /tmp/diagramN.png
cp /tmp/diagramN.png ./diagramN-widescreen-iter2.png
```

Check: Did metrics improve? Did the visual issue you targeted get better?

### Step 6: Iterate or Accept

- **Pass** → copy final PNG, clean up iteration files, move to next diagram
- **Improved but not passing** → go to Step 2 (increment iteration counter)
- **No improvement after 3 iterations on same issue** → escalate to user
- **Maximum 7 iterations per diagram** — if still failing, report what's wrong and move on

---

## Multi-Diagram Progress Tracking

When QA-ing multiple diagrams, maintain a status table and report it after each iteration:

```
| # | Diagram              | Ratio | Crossings | Routed | Visual | Status   | Iter |
|---|----------------------|-------|-----------|--------|--------|----------|------|
| 1 | microservices-api    | 1.82  | 0         | 4/4    | ✓      | ✅ PASS   | 3    |
| 2 | cicd-pipeline        | 2.15  | 1         | 3/3    | ✗      | 🔄 iter4 | 4    |
| 3 | event-driven         | 1.91  | 0         | 5/5    | ✓      | ✅ PASS   | 2    |
| 4 | cloud-infra          | 2.45  | 0         | 6/6    | ✗      | 🔄 iter2 | 2    |
| 5 | data-pipeline        | 1.78  | 0         | 4/4    | ✓      | ✅ PASS   | 1    |
```

**Work in rounds**: Each round, iterate on all non-passing diagrams. Continue until all pass or max iterations hit.

---

## Hint Strategy Guide

### Common Problems and Fixes

| Problem | Fix |
|---------|-----|
| Edges crossing each other | Add waypoints to separate routes by ≥30px |
| Wrong row arrangement | Set explicit `arrangement.rows` |
| Nodes too far apart | Reduce `spacing.gap` or set node position hints |
| Edge label overlapping node | Set `labelPercentage` to move label along route |
| Edge routing through container | Add waypoints that go around the container boundary |
| Ratio too tall/narrow | Rearrange nodes into fewer/more rows |
| Phantom arrows | Use waypoints to force clean orthogonal routing |
| Parallel edges merging | Assign different `laneIndex` values or use waypoints with offset X |

### Reading Layout State for Waypoint Computation

1. Find src and dst node centers from `nodes[id].center`
2. Identify the gap region between rows from `arrangement.rows` + node positions
3. Plan route: exit src node downward → horizontal through gap → enter dst from above
4. Place waypoints in the gap region using orthogonal (90°) segments only
5. For parallel edges to the same target, offset X coordinates by 30-50px

### Edge Key Format

Edge keys use the format: `"sourceAbsPath -> destAbsPath"`

- Nested nodes: `"runner.orchestrator -> services"`
- Duplicate edges: `"a -> b[1]"` (0-indexed, omit `[0]`)

---

## Quality Thresholds for Acceptance

A diagram passes QA when ALL of:
- [ ] Ratio between 1.5 and 2.2
- [ ] Zero edge crossings (from layout state)
- [ ] All cross-boundary edges have routes (≥2 points)
- [ ] No obvious visual defects (if vision inspection was done)
- [ ] Labels are readable and don't overlap nodes
- [ ] Would look professional on a presentation slide

## Final Deliverables

After all diagrams pass:
1. Final PNGs in working directory: `<name>-widescreen-final.png`
2. Clean up intermediate iteration PNGs
3. Report summary table with final metrics for each diagram
4. Note any diagrams that required >5 iterations or couldn't fully pass (potential engine improvements)
