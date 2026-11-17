package storage

import "testing"

func TestPortablePathCharactersAndDevices(t *testing.T) {
	for _, name := range []string{"a/CONIN$", "CONOUT$.txt", "COM¹.png", "LPT²", "a/<b>", "a/b?", "a/b*", "a/b|", "a/\"b", "a/line\nbreak", "a/\x7f", "a/\xff"} {
		if err := ValidPath(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"chats/我的故事/media/图 片.png", "characters/__proto__/card.md", "COM10.txt", ".transaction/staged"} {
		if err := ValidPath(name); err != nil {
			t.Errorf("rejected %q: %v", name, err)
		}
	}
}
