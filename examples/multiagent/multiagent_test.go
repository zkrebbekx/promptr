package multiagent

import (
	"context"
	"strings"
	"testing"

	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/fake"

	. "github.com/smartystreets/goconvey/convey"
)

func TestWriteBriefDelegatesToSubAgent(t *testing.T) {
	Convey("Given a fake provider scripting the orchestrator loop and the sub-agent", t, func() {
		// ToolReplies drives WriteBrief's agent loop: first request the sub-agent,
		// then return the final Brief. Replies drives the sub-agent's own Extract
		// when ResearchTopic runs — the two cursors are independent.
		p := &fake.Provider{
			ToolReplies: []fake.Reply{
				{Calls: []promptr.ToolCall{
					{ID: "c1", Name: "ResearchTopic", Arguments: `{"topic": "tidal energy"}`},
				}},
				{Text: `{"topic": "tidal energy", "recommendation": "pilot a 2MW array"}`},
			},
			Replies: []string{
				`{"summary": "predictable, capital-heavy", "sources": ["IEA", "DOE"]}`,
			},
		}

		Convey("When WriteBrief runs (no handlers supplied — the sub-agent is auto-wired)", func() {
			got, err := WriteBrief(context.Background(), p, "should we invest in tidal energy?")
			So(err, ShouldBeNil)

			Convey("Then the coordinator returns the typed Brief", func() {
				So(got.Topic, ShouldEqual, "tidal energy")
				So(got.Recommendation, ShouldEqual, "pilot a 2MW array")
			})

			Convey("Then the sub-agent ran its own extraction (Replies cursor advanced)", func() {
				So(p.Calls, ShouldNotBeEmpty)
				// The sub-agent's Research result was fed back as a tool result on the
				// loop's final turn.
				last := p.Calls[len(p.Calls)-1]
				var toolResults int
				for _, m := range last {
					if m.Role == "tool" {
						toolResults++
					}
				}
				So(toolResults, ShouldEqual, 1)
			})
		})
	})
}

func TestOrchestratorOptionsReachSubAgent(t *testing.T) {
	Convey("Given an orchestrator run with a system prompt option", t, func() {
		p := &fake.Provider{
			ToolReplies: []fake.Reply{
				{Calls: []promptr.ToolCall{
					{ID: "c1", Name: "ResearchTopic", Arguments: `{"topic": "tidal energy"}`},
				}},
				{Text: `{"topic": "tidal energy", "recommendation": "pilot a 2MW array"}`},
			},
			Replies: []string{
				`{"summary": "predictable, capital-heavy", "sources": ["IEA"]}`,
			},
		}

		Convey("When WriteBrief runs with WithSystem", func() {
			const sentinel = "RESPOND-IN-SPARTAN-PROSE"
			_, err := WriteBrief(context.Background(), p, "should we invest in tidal energy?", promptr.WithSystem(sentinel))
			So(err, ShouldBeNil)

			Convey("Then the option propagates down into the sub-agent's own call", func() {
				// Find the sub-agent's call (the one carrying its research prompt)
				// and assert the orchestrator's system prompt reached it — proof
				// that opt... threads through the delegation tree.
				var subAgentSawSystem bool
				for _, call := range p.Calls {
					isSubAgent, hasSystem := false, false
					for _, m := range call {
						if m.Role == "user" && strings.Contains(m.Content, "Research the topic") {
							isSubAgent = true
						}
						if m.Role == "system" && m.Content == sentinel {
							hasSystem = true
						}
					}
					if isSubAgent && hasSystem {
						subAgentSawSystem = true
					}
				}
				So(subAgentSawSystem, ShouldBeTrue)
			})
		})
	})
}

func TestResearchTopicRunsStandalone(t *testing.T) {
	Convey("Given the sub-agent invoked directly", t, func() {
		p := fake.New(`{"summary": "early but promising", "sources": ["arXiv"]}`)

		Convey("When ResearchTopic runs on its own", func() {
			got, err := ResearchTopic(context.Background(), p, "fusion startups")
			So(err, ShouldBeNil)

			Convey("Then it returns a typed Research value", func() {
				So(got.Summary, ShouldEqual, "early but promising")
				So(got.Sources, ShouldResemble, []string{"arXiv"})
			})
		})
	})
}
