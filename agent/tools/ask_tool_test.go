package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	agent "github.com/alfredxw/denova/agent"
)

func TestAskDerivesFreeTextAndRetriesInvalidChoicesThroughAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call := func(id, arguments string) *agent.Message {
		return agent.AssistantMessage("", []agent.ToolCall{{ID: id, Type: "function", Function: agent.FunctionCall{Name: "ask", Arguments: arguments}}})
	}
	owner, err := agent.New(ctx, agent.Definition{
		Name: "ask-schema", ModelIdentity: agent.CapabilityIdentity{Kind: "test.ask.schema", Version: 1}, Tools: Ask(),
		Model: &taskModel{responses: []*agent.Message{
			call("invalid", `{"questions":[{"id":"choice","prompt":"Pick one","options":[{"value":"a","label":"A"},{"value":"b","label":"B"}]}]}`),
			call("valid", `{"questions":[{"id":"text","prompt":"Describe the scope"},{"id":"choice","prompt":"Pick one","options":[{"value":"a","label":"A","recommended":true},{"value":"b","label":"B"}]}]}`),
			agent.AssistantMessage("done", nil),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	run, err := owner.Run(ctx, agent.Text("ask"))
	if err != nil {
		t.Fatal(err)
	}
	interactions, failures := 0, 0
	for event := range run.Events() {
		switch payload := event.Payload.(type) {
		case agent.ToolFinished:
			if payload.IsError {
				failures++
				if !strings.Contains(payload.Result, "exactly one recommended") {
					t.Errorf("unhelpful validation feedback: %s", payload.Result)
				}
			}
		case agent.InteractionRequested:
			interactions++
			questions := payload.Request.Questions
			if len(questions) != 2 || !questions[0].AllowFreeText || questions[1].AllowFreeText || !payload.Request.AllowOther {
				t.Fatalf("host question types = %#v", payload.Request)
			}
			if err := run.Respond(ctx, payload.Request.ID, agent.InteractionResponse{Answers: []agent.InteractionAnswer{
				{QuestionID: "text", Text: "whole project"}, {QuestionID: "choice", Values: []string{"a"}},
			}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := run.Wait(ctx)
	if err != nil || result.Status != agent.ResultCompleted || interactions != 1 || failures != 1 {
		t.Fatalf("result=%#v interactions=%d failures=%d error=%v", result, interactions, failures, err)
	}
}
