// Package multiagent shows promptr's multi-agent orchestration: one typed
// function delegates to another as a self-contained sub-agent.
//
// WriteBrief lists ResearchTopic in its tools block. Because ResearchTopic is a
// function (not a Go-backed tool), the compiler auto-wires it: WriteBrief takes
// no handlers struct, and its generated agent loop calls ResearchTopic directly
// — threading the same provider — whenever the model requests it. The sub-agent
// runs its own typed extraction and its Research result is marshalled back into
// the loop, so the coordinator ends with a typed Brief. See brief.promptr.
package multiagent
