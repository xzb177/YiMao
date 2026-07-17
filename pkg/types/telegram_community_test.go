package types

import (
	"encoding/json"
	"testing"
)

func TestTelegramMessageEphemeralAndCommunityCompatibility(t *testing.T) {
	payload := []byte(`{
		"message_id":0,
		"ephemeral_message_id":73,
		"receiver_user":{"id":42,"first_name":"Miao"},
		"chat":{"id":-1001,"type":"supergroup"},
		"date":1,
		"community_chat_added":{"community":{"id":9007199254740,"name":"云海影视社区"}}
	}`)
	var msg TelegramMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.EphemeralMessageID != 73 || msg.ReceiverUser == nil || msg.ReceiverUser.ID != 42 {
		t.Fatalf("ephemeral fields lost: %#v", msg)
	}
	if msg.CommunityChatAdded == nil || msg.CommunityChatAdded.Community.ID != 9007199254740 || msg.CommunityChatAdded.Community.Name != "云海影视社区" {
		t.Fatalf("community fields lost: %#v", msg.CommunityChatAdded)
	}
}

func TestTelegramMessageCommunityRemovalCompatibility(t *testing.T) {
	var msg TelegramMessage
	if err := json.Unmarshal([]byte(`{"message_id":1,"chat":{"id":-1,"type":"supergroup"},"community_chat_removed":{}}`), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.CommunityChatRemoved == nil {
		t.Fatal("community_chat_removed should be present")
	}
}
