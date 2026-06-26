package coerce

import "reflect"

// Partial carries a best-effort value parsed from an incomplete stream. As more
// tokens arrive the same target is re-parsed and re-emitted, with Complete
// flipping to true once the payload closes cleanly.
type Partial[T any] struct {
	Value    T
	Complete bool
	Err      error
}

// Stream consumes a channel of text chunks (e.g. server-sent tokens from a
// model) and emits a coerced snapshot of T after each chunk. The tolerant
// parser recovers a usable value from a half-written object, so callers can
// render progress before the model finishes. The output channel closes when
// the input channel closes.
func Stream[T any](chunks <-chan string) <-chan Partial[T] {
	out := make(chan Partial[T])
	t := reflect.TypeOf((*T)(nil)).Elem()
	go func() {
		defer close(out)
		var acc []byte
		for c := range chunks {
			acc = append(acc, c...)
			n, complete := parseTolerant(string(acc))
			rv, err := coerceNode(n, t)
			var v T
			if err == nil && rv.IsValid() {
				v, _ = rv.Interface().(T)
			}
			out <- Partial[T]{Value: v, Complete: complete, Err: err}
		}
	}()
	return out
}
