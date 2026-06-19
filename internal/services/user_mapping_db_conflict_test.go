package services

import "testing"

func TestDetectUserMappingConflictsByMoviePilotID(t *testing.T) {
	mappings := []UserMapping{
		{TelegramID: 1001, MPUserID: 42, MPUsername: "alice"},
		{TelegramID: 1002, MPUserID: 42, MPUsername: "alice_alt"},
	}

	conflicts := detectUserMappingConflicts(mappings)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %#v", len(conflicts), conflicts)
	}
	if conflicts[0].Kind != "mp_user_id" || conflicts[0].Value != "42" {
		t.Fatalf("unexpected conflict: %#v", conflicts[0])
	}
}

func TestDetectUserMappingConflictsByMoviePilotUsername(t *testing.T) {
	mappings := []UserMapping{
		{TelegramID: 1001, MPUserID: 42, MPUsername: "alice"},
		{TelegramID: 1002, MPUserID: 43, MPUsername: "alice"},
	}

	conflicts := detectUserMappingConflicts(mappings)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %#v", len(conflicts), conflicts)
	}
	if conflicts[0].Kind != "mp_username" || conflicts[0].Value != "alice" {
		t.Fatalf("unexpected conflict: %#v", conflicts[0])
	}
}

func TestDetectUserMappingConflictsAllowsUniqueMappings(t *testing.T) {
	mappings := []UserMapping{
		{TelegramID: 1001, MPUserID: 42, MPUsername: "alice"},
		{TelegramID: 1002, MPUserID: 43, MPUsername: "bob"},
	}

	conflicts := detectUserMappingConflicts(mappings)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %#v", conflicts)
	}
}
