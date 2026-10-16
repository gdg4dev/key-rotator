package main

import "testing"

func TestPathListEdits(t *testing.T) {
	const dir = `C:\Tools\keyrotator\bin`
	cases := []struct {
		name, list, add, remove string
	}{
		{"empty", ``, dir, ``},
		{"appends once", `C:\Windows;C:\Go\bin`, `C:\Windows;C:\Go\bin;` + dir, `C:\Windows;C:\Go\bin`},
		{"already there, any case, trailing slash", `C:\Windows;c:\tools\KEYROTATOR\bin\`, `C:\Windows;c:\tools\KEYROTATOR\bin\`, `C:\Windows`},
		{"quoted entry", `"C:\Tools\keyrotator\bin";C:\Windows`, `"C:\Tools\keyrotator\bin";C:\Windows`, `C:\Windows`},
		{"trailing separator", `C:\Windows;`, `C:\Windows;` + dir, `C:\Windows`},
	}
	for _, c := range cases {
		if got := pathListAdd(c.list, dir, ";", true); got != c.add {
			t.Errorf("%s: add = %q, want %q", c.name, got, c.add)
		}
		if got := pathListRemove(c.list, dir, ";", true); got != c.remove {
			t.Errorf("%s: remove = %q, want %q", c.name, got, c.remove)
		}
	}
	// Unix paths are case-sensitive.
	if pathListHas("/usr/bin:/Home/me/.local/bin", "/home/me/.local/bin", ":", false) {
		t.Error("unix comparison must be case-sensitive")
	}
}
