package coerce

import (
	"fmt"
	"reflect"
)

// Union resolves loosely-structured output into one of several candidate types
// — the discriminated-union / "classify into one of N shapes" case that JSON
// schemas express with oneOf and that sumx expresses with a sealed interface.
// Register the candidate shapes once, then resolve repeatedly.
//
//	u := coerce.NewUnion(Search{}, Reply{}, Escalate{})
//	act, err := coerce.ResolveInto[Action](modelText, u)
type Union struct {
	candidates []reflect.Type
}

// NewUnion builds a resolver from zero values of each candidate type.
func NewUnion(samples ...any) *Union {
	u := &Union{}
	for _, s := range samples {
		u.candidates = append(u.candidates, reflect.TypeOf(s))
	}
	return u
}

// ResolveInto parses raw, picks the best-fitting candidate, coerces into it,
// and returns it as the union interface I.
func ResolveInto[I any](raw string, u *Union) (I, error) {
	var zero I
	n, _ := parseTolerant(raw)
	rt, ok := u.bestFit(n)
	if !ok {
		return zero, fmt.Errorf("coerce: no union candidate matched output")
	}
	rv, err := coerceNode(n, rt)
	if err != nil {
		return zero, err
	}
	if out, ok := rv.Interface().(I); ok {
		return out, nil
	}
	// The interface may be satisfied by the pointer receiver set.
	pv := reflect.New(rt)
	pv.Elem().Set(rv)
	if out, ok := pv.Interface().(I); ok {
		return out, nil
	}
	return zero, fmt.Errorf("coerce: %s does not implement the target union interface", rt)
}

// bestFit chooses the candidate type whose shape most closely matches the
// parsed object. An explicit discriminator field (type/kind/action) naming a
// candidate short-circuits the scoring.
func (u *Union) bestFit(n node) (reflect.Type, bool) {
	if len(u.candidates) == 0 {
		return nil, false
	}
	if n.kind != kObj {
		return u.candidates[0], true // nothing to score against; first wins
	}

	keys := make(map[string]bool, len(n.obj))
	disc := ""
	for _, f := range n.obj {
		nk := normKey(f.key)
		keys[nk] = true
		if nk == "type" || nk == "kind" || nk == "action" {
			disc = normKey(f.val.asString())
		}
	}

	best := -1.0
	var bestT reflect.Type
	for _, ct := range u.candidates {
		st := ct
		if st.Kind() == reflect.Pointer {
			st = st.Elem()
		}
		if st.Kind() != reflect.Struct {
			continue
		}
		if disc != "" && normKey(st.Name()) == disc {
			return ct, true
		}
		if s := scoreStruct(st, keys); s > best {
			best, bestT = s, ct
		}
	}
	if bestT == nil {
		return nil, false
	}
	return bestT, true
}

// scoreStruct rewards a candidate both for explaining the object's keys
// (coverage) and for having its own fields satisfied (presence), so the type
// that best mutually fits the data wins.
func scoreStruct(t reflect.Type, keys map[string]bool) float64 {
	total, matched := 0, 0
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		total++
		if keys[normKey(fieldName(sf))] || keys[normKey(sf.Name)] {
			matched++
		}
	}
	if total == 0 {
		return 0
	}
	present := float64(matched) / float64(total)
	coverage := 0.0
	if len(keys) > 0 {
		coverage = float64(matched) / float64(len(keys))
	}
	return present + coverage
}
