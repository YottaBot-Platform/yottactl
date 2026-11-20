// Package output renders API responses for the yottactl CLI in one of
// three formats: table (default, human), json, yaml. Plan #60
// Track B B1.
//
// Empty-state handling is first-class: an empty JSON array renders
// as "(no records found)" rather than a blank table, so the
// background agent (and humans) get a clean, recognizable empty
// signal instead of an error — the central goal of Phase 0.5's
// stub coverage.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Render writes body to w in the requested format. body is the raw
// response bytes; format is "table" | "json" | "yaml".
func Render(format string, body []byte, w io.Writer) error {
	trimmed := strings.TrimSpace(string(body))
	switch format {
	case "json":
		return renderJSON(body, w)
	case "yaml":
		return renderYAML(body, w)
	case "table", "":
		return renderTable(trimmed, w)
	default:
		return fmt.Errorf("unknown output format %q (want table|json|yaml)", format)
	}
}

func renderJSON(body []byte, w io.Writer) error {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		_, err := w.Write(body)
		return err
	}
	pretty.WriteByte('\n')
	_, err := w.Write(pretty.Bytes())
	return err
}

func renderYAML(body []byte, w io.Writer) error {
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		_, err := w.Write(body)
		return err
	}
	b, err := yaml.Marshal(yamlNumbers(v))
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// renderTable best-effort renders a JSON array of objects as aligned
// columns. Non-array or scalar bodies fall back to pretty JSON.
func renderTable(trimmed string, w io.Writer) error {
	// Empty array → the recognizable empty signal.
	if trimmed == "[]" {
		_, err := fmt.Fprintln(w, "(no records found)")
		return err
	}
	var arr []map[string]any
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&arr); err != nil {
		// Not an array of objects — render as pretty JSON.
		return renderJSON([]byte(trimmed), w)
	}
	if len(arr) == 0 {
		_, err := fmt.Fprintln(w, "(no records found)")
		return err
	}
	cols := columns(arr)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(upper(cols), "\t"))
	for _, row := range arr {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = scalar(row[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// columns picks a stable, readable column order: id/name first if
// present, then the remaining keys from every row sorted. Optional fields
// appearing only in later rows must remain visible.
func columns(arr []map[string]any) []string {
	seen := map[string]bool{}
	for _, row := range arr {
		for k := range row {
			seen[k] = true
		}
	}
	var rest []string
	for k := range seen {
		if k == "id" || k == "name" {
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	var out []string
	if seen["name"] {
		out = append(out, "name")
	}
	if seen["id"] {
		out = append(out, "id")
	}
	return append(out, rest...)
}

func upper(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(s)
	}
	return out
}

// scalar flattens a cell value to a single-line string; nested
// objects/arrays collapse to compact JSON so the table stays aligned.
func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		// JSON numbers; render integers without a trailing .0
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case bool:
		return fmt.Sprintf("%v", t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// yaml.v3 treats json.Number as a string. Give it a numeric scalar node so
// both the value and its type survive JSON → YAML without float64 rounding.
func yamlNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(x.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: x.String()}
	case map[string]any:
		for k, value := range x {
			x[k] = yamlNumbers(value)
		}
	case []any:
		for i, value := range x {
			x[i] = yamlNumbers(value)
		}
	}
	return v
}
