package handlers

import (
	"strings"
	"testing"
)

func TestApproveCallbackReturnsVisibleEditedConfirmation(t *testing.T) {
	h := newApprovalHarness(t, 99, -100123, nil, nil)
	review := h.addReview(t, "visible-approval", 42, 551)
	resp, err := h.handler.Handle(h.context(review, 99, -100123, 7))
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || !resp.Edit || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("response=%+v, want visible edited confirmation", resp)
	}
	if h.moviePilotPosts.Load() != 1 {
		t.Fatalf("MoviePilot posts=%d, want 1", h.moviePilotPosts.Load())
	}
}

func TestDuplicateShortApproveCallbackReturnsVisibleEditedConfirmation(t *testing.T) {
	h := newApprovalHarness(t, 99, -100123, nil, nil)
	review := h.addReview(t, "visible-duplicate", 42, 552)
	first, err := h.handler.Handle(h.context(review, 99, -100123, 8))
	if err != nil || first == nil {
		t.Fatalf("first response=%+v err=%v", first, err)
	}
	before := h.moviePilotPosts.Load()
	dup, err := h.handler.Handle(h.shortContext(review, 99, -100123, 8))
	if err != nil {
		t.Fatal(err)
	}
	if dup == nil || !dup.Edit || strings.TrimSpace(dup.Text) == "" {
		t.Fatalf("duplicate response=%+v, want visible edited confirmation", dup)
	}
	if h.moviePilotPosts.Load() != before {
		t.Fatalf("duplicate created another subscription")
	}
}

func TestCommunityApproveResponseContainsNoRequesterPrivateDetails(t *testing.T) {
	h := newApprovalHarness(t, 99, -100123, nil, nil)
	review := h.addReview(t, "community-privacy", 6161183404, 553)
	resp, err := h.handler.Handle(h.context(review, 99, -100123, 9))
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("response=%+v, want visible private-safe text", resp)
	}
	if strings.Contains(resp.Text, "6161183404") || strings.Contains(resp.Text, review.TelegramName) {
		t.Fatalf("requester details leaked: %q", resp.Text)
	}
}
