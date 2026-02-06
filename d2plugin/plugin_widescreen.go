//go:build !nowidescreen

package d2plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2widescreenlayout"
	"oss.terrastruct.com/util-go/xmain"
)

var WidescreenPlugin = widescreenPlugin{}

func init() {
	plugins = append(plugins, &WidescreenPlugin)
}

type widescreenPlugin struct {
	mu   sync.Mutex
	opts *d2widescreenlayout.ConfigurableOpts
}

func (p *widescreenPlugin) Flags(context.Context) ([]PluginSpecificFlag, error) {
	return []PluginSpecificFlag{
		{
			Name:    "widescreen-ratio",
			Type:    "string",
			Default: fmt.Sprintf("%g", d2widescreenlayout.DefaultOpts.Ratio),
			Usage:   "target aspect ratio (width/height). Default 1.778 for 16:9.",
			Tag:     "ratio",
		},
		{
			Name:    "widescreen-inner",
			Type:    "string",
			Default: d2widescreenlayout.DefaultOpts.InnerEngine,
			Usage:   "inner layout engine for intra-container layout (dagre or elk).",
			Tag:     "inner",
		},
		{
			Name:    "widescreen-gap",
			Type:    "int64",
			Default: int64(d2widescreenlayout.DefaultOpts.Gap),
			Usage:   "gap in pixels between rearranged top-level nodes.",
			Tag:     "gap",
		},
		{
			Name:    "widescreen-hints",
			Type:    "string",
			Default: "",
			Usage:   "path to JSON hints file for agent-directed layout overrides. If empty, auto-discovers <input>.hints.json.",
			Tag:     "hints",
		},
		{
			Name:    "widescreen-layout-state",
			Type:    "string",
			Default: "",
			Usage:   "path to write layout state JSON (node positions, edge routes, quality metrics). If empty, auto-generates <output>.layout.json.",
			Tag:     "layoutState",
		},
	}, nil
}

// rawOpts is used for JSON unmarshaling since the ratio flag is a string.
type rawOpts struct {
	Ratio       string `json:"ratio"`
	Inner       string `json:"inner"`
	Gap         *int   `json:"gap"`
	Hints       string `json:"hints"`
	LayoutState string `json:"layoutState"`
}

func (p *widescreenPlugin) HydrateOpts(opts []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if opts != nil {
		var raw rawOpts
		if err := json.Unmarshal(opts, &raw); err != nil {
			return xmain.UsageErrorf("non-widescreen layout options given for widescreen")
		}

		cooked := d2widescreenlayout.DefaultOpts
		if raw.Ratio != "" {
			ratio, err := strconv.ParseFloat(raw.Ratio, 64)
			if err != nil {
				return xmain.UsageErrorf("invalid widescreen-ratio %q: must be a number", raw.Ratio)
			}
			if ratio <= 0 {
				return xmain.UsageErrorf("invalid widescreen-ratio %q: must be positive", raw.Ratio)
			}
			cooked.Ratio = ratio
		}
		if raw.Inner != "" {
			cooked.InnerEngine = raw.Inner
		}
		if raw.Gap != nil {
			cooked.Gap = *raw.Gap
		}
		if raw.Hints != "" {
			cooked.HintsPath = raw.Hints
		}
		if raw.LayoutState != "" {
			cooked.LayoutStatePath = raw.LayoutState
		}
		p.opts = &cooked
	}
	return nil
}

func (p *widescreenPlugin) Info(ctx context.Context) (*PluginInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	opts := xmain.NewOpts(nil, nil)
	flags, err := p.Flags(ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range flags {
		f.AddToOpts(opts)
	}

	return &PluginInfo{
		Name:     "widescreen",
		Type:     "bundled",
		Features: []PluginFeature{},
		ShortHelp: "Aspect-ratio-aware layout that wraps another engine",
		LongHelp: fmt.Sprintf(`widescreen rearranges top-level diagram nodes to approximate a target
aspect ratio (default 16:9). It delegates intra-container layout to an
inner engine (dagre by default), then repositions top-level groups into
rows that best fit the target ratio.

Flags:
%s
`, opts.Defaults()),
	}, nil
}

func (p *widescreenPlugin) Layout(ctx context.Context, g *d2graph.Graph) error {
	p.mu.Lock()
	optsCopy := *p.opts
	p.mu.Unlock()
	return d2widescreenlayout.Layout(ctx, g, &optsCopy)
}

func (p *widescreenPlugin) PostProcess(ctx context.Context, in []byte) ([]byte, error) {
	return in, nil
}
