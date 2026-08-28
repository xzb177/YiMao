package services

import "testing"

func TestBuildStartKeyboardOptionsHonorsWishAndAdminFlags(t *testing.T) {
	withoutWish := BuildStartKeyboardWithOptions(false, false)
	for _, row := range withoutWish.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "start_wish" || button.CallbackData == "admin_menu" {
				t.Fatalf("non-admin/no-wish keyboard leaked %q", button.CallbackData)
			}
		}
	}
	admin := BuildStartKeyboardWithOptions(true, true)
	var adminFound bool
	for _, row := range admin.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "admin_menu" {
				adminFound = true
			}
		}
	}
	if !adminFound {
		t.Fatal("admin keyboard omitted admin_menu")
	}
}

func TestBuildStartKeyboardOptionsRowsStayAtMostThreeWide(t *testing.T) {
	for _, tc := range []struct {
		admin, wish bool
	}{
		{false, false}, {false, true}, {true, false}, {true, true},
	} {
		keyboard := BuildStartKeyboardWithOptions(tc.admin, tc.wish)
		for _, row := range keyboard.InlineKeyboard {
			if len(row) == 0 || len(row) > 3 {
				t.Fatalf("options admin=%v wish=%v produced row width %d", tc.admin, tc.wish, len(row))
			}
		}
	}
}
