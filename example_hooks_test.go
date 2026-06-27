package promptr_test

import (
	"context"
	"fmt"

	"github.com/zkrebbekx/promptr"
)

// spanHook is the shape an OpenTelemetry exporter would take: open a span in
// BeforeCall, close it in the returned AfterFunc. Here it just prints, to keep
// the example dependency-free and deterministic.
type spanHook struct{}

func (spanHook) BeforeCall(_ context.Context, info promptr.CallInfo) promptr.AfterFunc {
	fmt.Printf("start %s\n", info.Kind)
	return func(o promptr.Outcome) {
		fmt.Printf("end   %s err=%v\n", info.Kind, o.Err)
	}
}

func ExampleWithHooks() {
	// Any Provider; here a trivial inline one.
	base := promptr.ProviderFunc(func(context.Context, []promptr.Message) (string, error) {
		return "ok", nil
	})

	p := promptr.WithHooks(base, spanHook{})
	_, _ = p.Complete(context.Background(), nil)

	// Output:
	// start complete
	// end   complete err=<nil>
}
