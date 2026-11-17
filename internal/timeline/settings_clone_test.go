package timeline

import "testing"

func TestSettingsCloneDoesNotShareStoryMaps(t *testing.T) {
	original := DefaultSettings()
	original.Characters["alice"] = true
	original.Worldbook.Categories["world"] = true
	original.Worldbook.Entries["world/history"] = true
	copied := original.Clone()
	copied.Characters["alice"] = false
	copied.Worldbook.Categories["world"] = false
	copied.Worldbook.Entries["world/history"] = false
	if !original.Characters["alice"] || !original.Worldbook.Categories["world"] || !original.Worldbook.Entries["world/history"] {
		t.Fatal("cloned story maps share mutable state")
	}
	if (Settings{}).Clone().Characters != nil {
		t.Fatal("clone must preserve nil maps")
	}
}
