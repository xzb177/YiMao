package handlers

import (
	"testing"

	"github.com/xzb177/yimao/internal/callback"
)

func TestDuplicateSubscriptionFeedbackUsesOneVisibleChannelInRequesterDM(t *testing.T) {
	resp, sendSeparate := duplicateSubscriptionFeedback(&callback.Context{ChatID: 42}, 42)
	if sendSeparate {
		t.Fatal("requester DM must not receive a second rich message")
	}
	if resp.ShowAlert {
		t.Fatal("duplicate subscription feedback must not open a blocking alert")
	}
	if !resp.Edit || resp.CallbackMsg != "已有订阅" {
		t.Fatalf("response=%+v", resp)
	}
}

func TestDuplicateSubscriptionFeedbackKeepsRequesterNotificationForOtherChat(t *testing.T) {
	resp, sendSeparate := duplicateSubscriptionFeedback(&callback.Context{ChatID: 99}, 42)
	if !sendSeparate {
		t.Fatal("approval from another chat must still notify the requester")
	}
	if resp.ShowAlert || !resp.Edit {
		t.Fatalf("response=%+v", resp)
	}
}
