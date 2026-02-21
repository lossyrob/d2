#!/usr/bin/env python3
"""Check quality of all rendered test diagrams.

Usage:
  cd /home/rob/proj/others/d2
  /tmp/d2-widescreen --layout widescreen feedback-v2/test-diagrams/system-context.d2 feedback-v2/test-diagrams/system-context.png
  # ... render all 4 ...
  python3 feedback-v2/test-diagrams/check-quality.py
"""
import json, glob, sys, os

script_dir = os.path.dirname(os.path.abspath(__file__))
pattern = os.path.join(script_dir, "*.png.layout.json")

results = []
for path in sorted(glob.glob(pattern)):
    name = os.path.basename(path).replace(".png.layout.json", "")
    d = json.load(open(path))
    q = d.get("quality", {})
    dims = d.get("dimensions", {})
    ratio = dims.get("ratio", 0)
    crossings = q.get("edgeCrossings", -1)
    backward = q.get("backwardEdges", -1)
    total = q.get("totalEdges", -1)

    # Check children horizontal (x-spread > y-spread for multi-child containers)
    children_issues = []
    for nname, node in d.get("nodes", {}).items():
        children = node.get("children", {})
        if len(children) >= 2:
            centers = [c.get("center", [0, 0]) for c in children.values()]
            ys = [c[1] for c in centers]
            xs = [c[0] for c in centers]
            y_spread = max(ys) - min(ys)
            x_spread = max(xs) - min(xs)
            if y_spread > x_spread and x_spread < 50:
                children_issues.append(f"{nname}: VERTICAL (x={x_spread:.0f}, y={y_spread:.0f})")

    # Check for visually-backward edges (x decreasing > 50px in route)
    visual_backward = []
    for ename, edge in d.get("edges", {}).items():
        route = edge.get("route", [])
        if len(route) >= 2:
            start_x = route[0][0]
            end_x = route[-1][0]
            if end_x < start_x - 50:
                visual_backward.append(f"{ename} (x: {start_x:.0f} -> {end_x:.0f})")

    # Check for micro-segments (< 15px) that create SVG Bézier artifacts
    micro_segments = []
    for ename, edge in d.get("edges", {}).items():
        route = edge.get("route", [])
        for j in range(len(route) - 1):
            dx = abs(route[j+1][0] - route[j][0])
            dy = abs(route[j+1][1] - route[j][1])
            seg_len = (dx**2 + dy**2)**0.5
            if seg_len < 15 and len(route) > 2:
                micro_segments.append(f"{ename} seg[{j}] len={seg_len:.0f}")

    # Check for route segments inside container bboxes (arrow through interior)
    # Only flag cross-boundary edges, not intra-container edges
    interior_routes = []
    nodes = d.get("nodes", {})
    for ename, edge in d.get("edges", {}).items():
        route = edge.get("route", [])
        if len(route) < 3:
            continue
        # Parse edge name to check if both endpoints share a container
        parts = ename.split(" -> ")
        if len(parts) == 2:
            src_parts = parts[0].split(".")
            dst_parts = parts[1].split(".")
        else:
            src_parts = dst_parts = []
        for nname, node in nodes.items():
            nw = node.get("width", 0)
            nh = node.get("height", 0)
            nc = node.get("center", [0, 0])
            if nw < 100 or nh < 50:
                continue  # skip small nodes
            # Skip if both endpoints are children of this container
            if src_parts and dst_parts and src_parts[0] == nname and dst_parts[0] == nname:
                continue
            nleft = nc[0] - nw / 2
            nright = nc[0] + nw / 2
            ntop = nc[1] - nh / 2
            nbottom = nc[1] + nh / 2
            # Check if any intermediate route point is inside this container
            for j in range(1, len(route) - 1):
                px, py = route[j]
                if nleft + 10 < px < nright - 10 and ntop + 10 < py < nbottom - 10:
                    interior_routes.append(f"{ename} pt[{j}] inside {nname}")
                    break

    metrics_pass = (
        1.3 <= ratio <= 4.0
        and crossings == 0
        and backward == 0
    )
    children_ok = len(children_issues) == 0
    visual_ok = len(visual_backward) == 0 and len(micro_segments) == 0 and len(interior_routes) == 0

    results.append({
        "name": name,
        "ratio": ratio,
        "crossings": crossings,
        "backward": backward,
        "visual_backward": visual_backward,
        "micro_segments": micro_segments,
        "interior_routes": interior_routes,
        "children_issues": children_issues,
        "metrics_pass": metrics_pass,
        "children_ok": children_ok,
        "visual_ok": visual_ok,
        "all_pass": metrics_pass and children_ok and visual_ok,
    })

    # Print results
    status = "PASS" if metrics_pass else "FAIL"
    visual_status = "VISUAL-OK" if visual_ok else "VISUAL-ISSUES"
    print(f"{status}  {visual_status}  {name}")
    print(f"      ratio={ratio:.2f}  crossings={crossings}  backward={backward}  edges={total}")

    if children_issues:
        for ci in children_issues:
            print(f"      children-issue: {ci}")

    if visual_backward:
        for vb in visual_backward:
            print(f"      visual-backward: {vb}")

    if micro_segments:
        for ms in micro_segments:
            print(f"      micro-segment: {ms}")

    if interior_routes:
        for ir in interior_routes:
            print(f"      interior-route: {ir}")

    print()

# Summary
metrics_passed = sum(1 for r in results if r["metrics_pass"])
visual_passed = sum(1 for r in results if r["visual_ok"])
all_passed = sum(1 for r in results if r["all_pass"])
total = len(results)

print(f"Metrics:  {metrics_passed}/{total} passed")
print(f"Visual:   {visual_passed}/{total} passed")
print(f"All:      {all_passed}/{total} passed")

sys.exit(0 if all(r["all_pass"] for r in results) else 1)
