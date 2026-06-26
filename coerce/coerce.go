package coerce

import (
	"encoding"
	"fmt"
	"reflect"
)

// CoerceError reports that the parsed output could not be shaped into the
// target type — for example, bare prose where a struct was expected. The
// runtime treats it as the signal to re-ask the model.
type CoerceError struct {
	Target string // the Go type that was expected
	Got    string // the shape that was actually parsed
}

func (e *CoerceError) Error() string {
	return fmt.Sprintf("coerce: cannot fit %s value into %s", e.Got, e.Target)
}

// Into parses possibly-messy model output and coerces it into a value of type
// T. It is tolerant of malformed-but-present input — fences, trailing commas,
// loose scalars, truncation — recovering what it can on a best-effort basis.
// It returns a *CoerceError only when the output cannot be shaped into T at
// all, e.g. bare prose where a struct was expected; the runtime uses that as
// its cue to re-ask the model.
func Into[T any](raw string) (T, error) {
	var out T
	t := reflect.TypeOf((*T)(nil)).Elem()
	n, _ := parseTolerant(raw)
	rv, err := coerceNode(n, t)
	if err != nil {
		return out, err
	}
	if rv.IsValid() {
		out, _ = rv.Interface().(T)
	}
	return out, nil
}

// Value is the non-generic form of Into, coercing into the supplied
// reflect.Type. It is what the streaming and code-generated paths use.
func Value(raw string, t reflect.Type) (any, error) {
	n, _ := parseTolerant(raw)
	rv, err := coerceNode(n, t)
	if err != nil {
		return nil, err
	}
	return rv.Interface(), nil
}

var textUnmarshaler = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

// coerceNode is the reflection-driven core: it walks the target type alongside
// the parsed node, applying tolerant conversions.
func coerceNode(n node, t reflect.Type) (reflect.Value, error) {
	if t.Kind() == reflect.Pointer {
		ev, err := coerceNode(n, t.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		p := reflect.New(t.Elem())
		p.Elem().Set(ev)
		return p, nil
	}

	// Types that know how to parse themselves from text (time.Time, and the
	// fuzzy enums promptr generates) get first refusal.
	if t.Kind() != reflect.Interface && reflect.PointerTo(t).Implements(textUnmarshaler) {
		p := reflect.New(t)
		if err := p.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(n.asString())); err == nil {
			return p.Elem(), nil
		}
		// fall through to default handling on failure
	}

	switch t.Kind() {
	case reflect.String:
		if members, ok := membersOf(t); ok {
			if m, ok := fuzzyEnum(n.asString(), members); ok {
				return reflect.ValueOf(m).Convert(t), nil
			}
		}
		return reflect.ValueOf(n.asString()).Convert(t), nil
	case reflect.Bool:
		v := reflect.New(t).Elem()
		v.SetBool(n.asBool())
		return v, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v := reflect.New(t).Elem()
		v.SetInt(int64(n.asNumber()))
		return v, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v := reflect.New(t).Elem()
		v.SetUint(uint64(n.asNumber()))
		return v, nil
	case reflect.Float32, reflect.Float64:
		v := reflect.New(t).Elem()
		v.SetFloat(n.asNumber())
		return v, nil
	case reflect.Slice:
		return coerceSlice(n, t)
	case reflect.Map:
		return coerceMap(n, t)
	case reflect.Struct:
		return coerceStruct(n, t)
	case reflect.Interface:
		av := n.asAny()
		if av == nil {
			return reflect.Zero(t), nil
		}
		rv := reflect.ValueOf(av)
		if rv.Type().AssignableTo(t) {
			return rv, nil
		}
		return reflect.Zero(t), nil
	}
	return reflect.Zero(t), nil
}

func coerceStruct(n node, t reflect.Type) (reflect.Value, error) {
	out := reflect.New(t).Elem()
	if n.kind != kObj {
		if n.kind == kNull {
			return out, nil // absent/null: tolerate as zero value
		}
		// A present, non-object value (bare prose, a scalar) cannot fill a
		// struct. This is the structural failure the runtime retries on:
		// "the model did not return the requested shape".
		return out, &CoerceError{Target: t.String(), Got: n.kind.String()}
	}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue // unexported
		}
		fn := findField(n, fieldName(sf), sf.Name)
		if fn == nil {
			continue
		}
		fv, err := coerceNode(*fn, sf.Type)
		if err != nil {
			return out, err
		}
		if fv.IsValid() {
			out.Field(i).Set(fv)
		}
	}
	return out, nil
}

func coerceSlice(n node, t reflect.Type) (reflect.Value, error) {
	et := t.Elem()
	if n.kind == kNull {
		return reflect.Zero(t), nil
	}
	if n.kind != kArr {
		// Tolerate a single value where a list was expected.
		ev, err := coerceNode(n, et)
		if err != nil {
			return reflect.Zero(t), err
		}
		s := reflect.MakeSlice(t, 1, 1)
		s.Index(0).Set(ev)
		return s, nil
	}
	s := reflect.MakeSlice(t, len(n.arr), len(n.arr))
	for i, e := range n.arr {
		ev, err := coerceNode(e, et)
		if err != nil {
			return s, err
		}
		if ev.IsValid() {
			s.Index(i).Set(ev)
		}
	}
	return s, nil
}

func coerceMap(n node, t reflect.Type) (reflect.Value, error) {
	if t.Key().Kind() != reflect.String || n.kind != kObj {
		return reflect.MakeMap(t), nil
	}
	m := reflect.MakeMap(t)
	for i := range n.obj {
		ev, err := coerceNode(n.obj[i].val, t.Elem())
		if err != nil {
			return m, err
		}
		k := reflect.ValueOf(n.obj[i].key).Convert(t.Key())
		m.SetMapIndex(k, ev)
	}
	return m, nil
}

// fieldName returns the JSON name for a struct field, honouring the `json`
// tag and falling back to the Go field name.
func fieldName(sf reflect.StructField) string {
	if tag := sf.Tag.Get("json"); tag != "" {
		if c := indexByte(tag, ','); c >= 0 {
			tag = tag[:c]
		}
		if tag != "" && tag != "-" {
			return tag
		}
	}
	return sf.Name
}

// findField locates the object member matching either the JSON name or the Go
// field name, tolerant of case and of _/-/space separators (so snake_case
// output binds to a CamelCase Go field).
func findField(n node, jsonName, goName string) *node {
	jn, gn := normKey(jsonName), normKey(goName)
	for i := range n.obj {
		k := normKey(n.obj[i].key)
		if k == jn || k == gn {
			return &n.obj[i].val
		}
	}
	return nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
