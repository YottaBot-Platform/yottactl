package output

import (
	"bytes"
	"strings"
	"testing"
)

// The central Phase 0.5 goal: an empty array renders as a clean,
// recognizable empty signal — never an error — so the agent handles
// "no records" gracefully.
func TestRender_EmptyArray(t *testing.T) {
	for _, format := range []string{"table", "json", "yaml"} {
		var buf bytes.Buffer
		if err := Render(format, []byte("[]"), &buf); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		out := buf.String()
		if format == "table" && !strings.Contains(out, "no records found") {
			t.Errorf("table empty render = %q, want the no-records signal", out)
		}
	}
}

func TestRender_TableColumns(t *testing.T) {
	body := []byte(`[{"id":"a1","name":"repo-auditor","status":"available"},
	                 {"id":"a2","name":"pr-drafter","status":"draft"}]`)
	var buf bytes.Buffer
	if err := Render("table", body, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// name + id lead; header upper-cased; both rows present.
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "ID") || !strings.Contains(out, "STATUS") {
		t.Errorf("missing headers: %q", out)
	}
	if !strings.Contains(out, "repo-auditor") || !strings.Contains(out, "pr-drafter") {
		t.Errorf("missing rows: %q", out)
	}
	// name column comes before id (column ordering rule).
	if strings.Index(out, "NAME") > strings.Index(out, "ID") {
		t.Errorf("NAME should precede ID: %q", out)
	}
}

func TestRender_JSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render("json", []byte(`{"a":1}`), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\"a\": 1") {
		t.Errorf("json not pretty-printed: %q", buf.String())
	}
}

func TestRender_UnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Render("toml", []byte("[]"), &buf); err == nil {
		t.Error("unknown format should error")
	}
}

func TestLargeNumbersRetainValueAndType(t *testing.T) {
	for _, format := range []string{"json", "yaml", "table"} {
		var out bytes.Buffer
		err := Render(format, []byte(`[{"id":9007199254740993,"decimal":0.1234567890123456789}]`), &out)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "9007199254740993") || !strings.Contains(out.String(), "0.1234567890123456789") {
			t.Fatalf("%s rounded numbers: %s", format, out.String())
		}
		if format == "yaml" && strings.Contains(out.String(), `"9007199254740993"`) {
			t.Fatal("numeric ID became string")
		}
	}
}

func TestTableKeepsOptionalFieldsInLaterRows(t *testing.T) {
	var out bytes.Buffer
	if err := Render("table", []byte(`[{"id":"one"},{"id":"two","error":"permission denied"}]`), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ERROR") || !strings.Contains(out.String(), "permission denied") {
		t.Fatalf("hid later row error: %s", out.String())
	}
}
