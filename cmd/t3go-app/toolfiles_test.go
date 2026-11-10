package main

import "testing"

func TestToolChangedFiles(t *testing.T) {
	cases := []struct {
		title, detail string
		want          []string
	}{
		{"Edit", `{"file_path":"src/app.go","old_string":"a","new_string":"b"}`, []string{"src/app.go"}},
		{"Edit", `[{"path":"a.go"},{"path":"b.go"}]`, []string{"a.go", "b.go"}},
		{"Write", "docs/readme.md", []string{"docs/readme.md"}},
		{"Bash", `{"command":"ls","path":"not-a-change"}`, nil},
		{"Read", `{"file_path":"a.go"}`, nil},
	}
	for _, test := range cases {
		got := toolChangedFiles(test.title, test.detail)
		if len(got) != len(test.want) {
			t.Fatalf("%s %s = %#v, want %#v", test.title, test.detail, got, test.want)
		}
		for i := range got {
			if got[i] != test.want[i] {
				t.Fatalf("%s %s = %#v, want %#v", test.title, test.detail, got, test.want)
			}
		}
	}
}

func TestToolFileRelative(t *testing.T) {
	root := "/repo"
	if got := toolFileRelative(root, "/repo/src/a.go"); got != "src/a.go" {
		t.Fatalf("absolute = %q", got)
	}
	if got := toolFileRelative(root, "src/a.go"); got != "src/a.go" {
		t.Fatalf("relative = %q", got)
	}
	if got := toolFileRelative(root, "../escape"); got != "" {
		t.Fatalf("escape = %q", got)
	}
}
