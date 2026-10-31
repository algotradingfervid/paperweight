package docscreen

import (
	"fmt"
	"maps"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// stepHook is the module's custom step i of dt, the zero Step when it has
// none. Every Steps key must name one of steps other than check; any other
// key is a plain internal error, whichever step is asked for (CD-256 (8)).
func (d Deps) stepHook(dt *doctype.DocType, steps []FormStep, i int) (Step, error) {
	hooks := d.modOf(dt).Steps
	for slug := range hooks {
		if j, ok := stepOf(steps, slug); !ok || j == len(steps)-1 {
			return Step{}, fmt.Errorf("docscreen: %s module has a custom step %q, which is not one of its wizard steps", dt.Name, slug)
		}
	}
	return hooks[steps[i].Slug], nil
}

// labelled is steps with the module's StepLabels shown instead of their
// labels; slugs and order are unchanged.
func (d Deps) labelled(dt *doctype.DocType, steps []FormStep) []FormStep {
	labels := d.modOf(dt).StepLabels
	out := slices.Clone(steps)
	for i, s := range out {
		if l, ok := labels[s.Slug]; ok {
			out[i].Label = l
		}
	}
	return out
}

// mergeLines is the lines a custom save sends (CD-256 (3)): the loaded ones
// when saved is nil; otherwise each saved line, one with an id merged over
// the loaded line with that id, its own keys winning. An id not among the
// loaded lines is a plain internal error.
func mergeLines(loaded, saved []map[string]string) ([]map[string]string, error) {
	if saved == nil {
		return loaded, nil
	}
	out := make([]map[string]string, 0, len(saved))
	for _, l := range saved {
		id, ok := l["id"]
		if !ok {
			out = append(out, l)
			continue
		}
		j := slices.IndexFunc(loaded, func(s map[string]string) bool { return s["id"] == id })
		if j < 0 {
			return nil, fmt.Errorf("docscreen: a custom save sent line id %q, which is not on the record", id)
		}
		m := maps.Clone(loaded[j])
		maps.Copy(m, l)
		out = append(out, m)
	}
	return out, nil
}
