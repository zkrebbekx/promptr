package agent

import (
	"context"
	"testing"

	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/fake"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPlanTripRunsToolLoop(t *testing.T) {
	Convey("Given a fake provider scripted to call both tools then answer", t, func() {
		p := &fake.Provider{ToolReplies: []fake.Reply{
			{Calls: []promptr.ToolCall{
				{ID: "c1", Name: "GetWeather", Arguments: `{"city": "Reykjavik"}`},
				{ID: "c2", Name: "SearchFlights", Arguments: `{"from": "NYC", "to": "Reykjavik"}`},
			}},
			{Text: `{"destination": "Reykjavik", "summary": "chase the aurora", "packing": ["thermal layers", "camera"]}`},
		}}

		var gotWeatherCity string
		var gotFlightTo string
		handlers := PlanTripTools{
			GetWeather: func(_ context.Context, a GetWeatherArgs) (Weather, error) {
				gotWeatherCity = a.City
				return Weather{City: a.City, Conditions: "snow", HighC: -2}, nil
			},
			SearchFlights: func(_ context.Context, a SearchFlightsArgs) ([]Flight, error) {
				gotFlightTo = a.To
				return []Flight{{Carrier: "Icelandair", Price: 540}}, nil
			},
		}

		Convey("When PlanTrip runs the agent loop", func() {
			got, err := PlanTrip(context.Background(), p, "see the northern lights", handlers)
			So(err, ShouldBeNil)

			Convey("Then both tools were dispatched with decoded args", func() {
				So(gotWeatherCity, ShouldEqual, "Reykjavik")
				So(gotFlightTo, ShouldEqual, "Reykjavik")
			})

			Convey("Then the final typed Itinerary is returned", func() {
				So(got.Destination, ShouldEqual, "Reykjavik")
				So(got.Summary, ShouldEqual, "chase the aurora")
				So(got.Packing, ShouldResemble, []string{"thermal layers", "camera"})
			})

			Convey("Then the model saw the tool results fed back", func() {
				// Calls[1] is the second CompleteTools turn: system?/user + assistant
				// tool-call turn + two tool results.
				last := p.Calls[len(p.Calls)-1]
				var toolResults int
				for _, m := range last {
					if m.Role == "tool" {
						toolResults++
					}
				}
				So(toolResults, ShouldEqual, 2)
			})
		})
	})
}
