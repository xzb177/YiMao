package main

import (
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/handlers"
)

func TestReviewWashCallbacksAreAcceptedRegisteredAndDispatched(t *testing.T) {
	registry := callback.NewRegistry()
	handler := handlers.NewReviewHandler(nil, nil, nil, nil, nil, nil, nil, 0)
	registerReviewCallbacks(registry, handler)
	parser := callback.NewParser()

	acceptedActions := []callback.Action{
		"review_approve",
		"review_reject",
		"review_cancel",
		"review_complete_wash",
		"review_claim_wash",
		"review_release_wash",
		"review_retry_wash",
		"review_detail_wash",
		"my_reviews",
		"review_list",
		"rv_a",
		"rv_r",
	}
	for _, action := range acceptedActions {
		t.Run(string(action), func(t *testing.T) {
			if _, err := parser.Parse(string(action)); err != nil {
				t.Fatalf("parser rejects review action %q: %v", action, err)
			}
			if _, ok := registry.Get(action); !ok {
				t.Fatalf("review action %q is not registered", action)
			}
			if !handler.Supports(action) {
				t.Fatalf("review handler does not dispatch action %q", action)
			}
		})
	}

	unknown := callback.Action("review_unknown")
	if _, err := parser.Parse(string(unknown)); err == nil {
		t.Fatalf("parser unexpectedly accepts unknown review action %q", unknown)
	}
	if _, ok := registry.Get(unknown); ok {
		t.Fatalf("unknown review action %q is registered", unknown)
	}
	if handler.Supports(unknown) {
		t.Fatalf("review handler unexpectedly dispatches unknown action %q", unknown)
	}
}
