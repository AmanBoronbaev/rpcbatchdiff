package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

type report struct {
	Calls         int       `json:"calls"`
	Notifications int       `json:"notifications"`
	Matched       int       `json:"matched"`
	MatchedErrors int       `json:"matched_errors"`
	Findings      []finding `json:"findings"`
}

type finding struct {
	Kind   string `json:"kind"`
	Side   string `json:"side,omitempty"`
	ID     any    `json:"id"`
	Method string `json:"method,omitempty"`
	Count  int    `json:"count,omitempty"`
	change
}

type change struct {
	Path  string `json:"path,omitempty"`
	Left  *any   `json:"left,omitempty"`
	Right *any   `json:"right,omitempty"`
}

func compare(req requests, left, right map[string]response) report {
	out := report{Calls: len(req.calls), Notifications: req.notifications, Findings: []finding{}}
	for _, call := range req.calls {
		l, r := left[call.key], right[call.key]
		for _, capture := range []struct {
			side  string
			count int
		}{{"left", l.count}, {"right", r.count}} {
			f := finding{ID: call.id, Method: call.method, Side: capture.side}
			switch capture.count {
			case 0:
				f.Kind = "missing"
			case 1:
				continue
			default:
				f.Kind, f.Count = "duplicate", capture.count
			}
			out.Findings = append(out.Findings, f)
		}
		if l.count != 1 || r.count != 1 {
			continue
		}
		f := finding{ID: call.id, Method: call.method}
		if l.kind != r.kind {
			f.Kind = "outcome_changed"
			var before any = map[string]any{l.kind: l.value}
			var after any = map[string]any{r.kind: r.value}
			f.Left, f.Right = &before, &after
		} else if delta := firstChange("/"+l.kind, l.value, r.value); delta != nil {
			f.Kind, f.change = l.kind+"_changed", *delta
		} else {
			out.Matched++
			if l.kind == "error" {
				out.MatchedErrors++
			}
			continue
		}
		out.Findings = append(out.Findings, f)
	}
	for _, capture := range []struct {
		side string
		byID map[string]response
	}{{"left", left}, {"right", right}} {
		var extras []string
		for key := range capture.byID {
			if !req.ids[key] {
				extras = append(extras, key)
			}
		}
		sort.Strings(extras)
		for _, key := range extras {
			r := capture.byID[key]
			f := finding{Kind: "unexpected", ID: r.id, Side: capture.side, Count: r.count}
			out.Findings = append(out.Findings, f)
		}
	}
	return out
}

// firstChange returns one differing location per call. A nil value pointer
// means an absent member or element. A pointer to a nil interface means JSON null.
func firstChange(path string, left, right any) *change {
	switch l := left.(type) {
	case map[string]any:
		if r, ok := right.(map[string]any); ok {
			keys := make([]string, 0, len(l)+len(r))
			for k := range l {
				keys = append(keys, k)
			}
			for k := range r {
				if _, exists := l[k]; !exists {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				child := path + "/" + strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
				lv, hasLeft := l[k]
				rv, hasRight := r[k]
				if !hasLeft {
					return &change{Path: child, Right: &rv}
				}
				if !hasRight {
					return &change{Path: child, Left: &lv}
				}
				if delta := firstChange(child, lv, rv); delta != nil {
					return delta
				}
			}
			return nil
		}
	case []any:
		if r, ok := right.([]any); ok {
			for i := 0; i < max(len(l), len(r)); i++ {
				child := path + "/" + strconv.Itoa(i)
				if i == len(l) {
					return &change{Path: child, Right: &r[i]}
				}
				if i == len(r) {
					return &change{Path: child, Left: &l[i]}
				}
				if delta := firstChange(child, l[i], r[i]); delta != nil {
					return delta
				}
			}
			return nil
		}
	case json.Number:
		if r, ok := right.(json.Number); ok && numberKey(l) == numberKey(r) {
			return nil
		}
	default:
		// The remaining JSON types are string, boolean, and null.
		if left == right {
			return nil
		}
	}
	return &change{Path: path, Left: &left, Right: &right}
}
