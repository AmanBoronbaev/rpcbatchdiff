package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const oneRequest = `[{"jsonrpc":"2.0","method":"eth_test","id":1}]`
const oneResult = `[{"jsonrpc":"2.0","id":1,"result":null}]`

func capture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func invoke(t *testing.T, requests, left, right string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"-json", capture(t, dir, "requests", requests),
		capture(t, dir, "left", left), capture(t, dir, "right", right),
	}
	var stdout, stderr bytes.Buffer
	code := run(files, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

type testReport struct {
	Calls         int                          `json:"calls"`
	Notifications int                          `json:"notifications"`
	Matched       int                          `json:"matched"`
	MatchedErrors int                          `json:"matched_errors"`
	Findings      []map[string]json.RawMessage `json:"findings"`
}

func requireReport(t *testing.T, wantCode int, req, left, right string) testReport {
	t.Helper()
	code, stdout, stderr := invoke(t, req, left, right)
	if code != wantCode || stderr != "" {
		t.Fatalf("exit %d, want %d; stderr %q; stdout %s", code, wantCode, stderr, stdout)
	}
	var out testReport
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func requireFields(t *testing.T, f map[string]json.RawMessage, fields map[string]string) {
	t.Helper()
	for name, want := range fields {
		got := bytes.Buffer{}
		if value, ok := f[name]; ok {
			if err := json.Compact(&got, value); err != nil {
				t.Fatal(err)
			}
		}
		if got.String() != want {
			t.Errorf("%s = %s, want %s", name, got.String(), want)
		}
	}
}

func TestTypedIDsAndExactNumbers(t *testing.T) {
	req := `[
		{"jsonrpc":"2.0","id":9007199254740992,"method":"a"},
		{"jsonrpc":"2.0","id":9007199254740993,"method":"b"},
		{"jsonrpc":"2.0","id":"9007199254740993","method":"c"},
		{"jsonrpc":"2.0","id":null,"method":"d"},
		{"jsonrpc":"2.0","method":"notify"}
	]`
	left := `[
		{"jsonrpc":"2.0","id":9007199254740992,"result":{"b":1.25,"a":9007199254740993}},
		{"jsonrpc":"2.0","id":9007199254740993,"result":null},
		{"jsonrpc":"2.0","id":"9007199254740993","error":{"code":-32601,"message":"Unavailable"}},
		{"jsonrpc":"2.0","id":null,"result":["0x01",false]}
	]`
	right := `[
		{"jsonrpc":"2.0","id":null,"result":["0x01",false]},
		{"jsonrpc":"2.0","id":"9007199254740993","error":{"message":"Unavailable","code":-326010e-1}},
		{"jsonrpc":"2.0","id":90071992547409930e-1,"result":null},
		{"jsonrpc":"2.0","id":9007199254740992,"result":{"a":9007199254740993.0,"b":125e-2}}
	]`
	out := requireReport(t, 0, req, left, right)
	if out.Calls != 4 || out.Matched != 4 || out.Notifications != 1 ||
		out.MatchedErrors != 1 || out.Findings == nil || len(out.Findings) != 0 {
		t.Fatalf("unexpected report: %+v", out)
	}
}

func TestStructuralFindings(t *testing.T) {
	req := `[
		{"jsonrpc":"2.0","method":"a","id":1},
		{"jsonrpc":"2.0","method":"b","id":"1"}
	]`
	left := `[
		{"jsonrpc":"2.0","id":1,"result":"a"},
		{"jsonrpc":"2.0","id":1.0,"result":"b"},
		{"jsonrpc":"2.0","id":"extra","result":null}
	]`
	right := `[
		{"jsonrpc":"2.0","id":"1","result":"b"},
		{"jsonrpc":"2.0","id":null,"result":null},
		{"jsonrpc":"2.0","id":null,"result":true}
	]`
	out := requireReport(t, 1, req, left, right)
	if out.Matched != 0 || len(out.Findings) != 5 {
		t.Fatalf("unexpected report: %+v", out)
	}
	wants := []map[string]string{
		{"kind": `"duplicate"`, "side": `"left"`, "id": "1", "count": "2"},
		{"kind": `"missing"`, "side": `"right"`, "id": "1"},
		{"kind": `"missing"`, "side": `"left"`, "id": `"1"`},
		{"kind": `"unexpected"`, "side": `"left"`, "id": `"extra"`, "count": "1"},
		{"kind": `"unexpected"`, "side": `"right"`, "id": "null", "count": "2"},
	}
	for i, want := range wants {
		requireFields(t, out.Findings[i], want)
	}
}

func TestChangedValues(t *testing.T) {
	cases := []struct {
		name, left, right string
		want              map[string]string
	}{
		{"large integers", `"result":9007199254740992`, `"result":9007199254740993`,
			map[string]string{"kind": `"result_changed"`, "path": `"/result"`, "left": "9007199254740992", "right": "9007199254740993"}},
		{"missing key and escaped pointer", `"result":{"a/b~c":[null]}`, `"result":{"a/b~c":[]}`,
			map[string]string{"path": `"/result/a~1b~0c/0"`, "left": "null", "right": ""}},
		{"absent object member", `"result":{}`, `"result":{"n":null}`,
			map[string]string{"path": `"/result/n"`, "left": "", "right": "null"}},
		{"first key", `"result":{"z":0,"a":1}`, `"result":{"a":2,"z":3}`,
			map[string]string{"path": `"/result/a"`, "left": "1", "right": "2"}},
		{"ordered array", `"result":[1,2]`, `"result":[2,1]`,
			map[string]string{"path": `"/result/0"`, "left": "1", "right": "2"}},
		{"error data", `"error":{"code":-1,"message":"fail","data":0}`, `"error":{"code":-1,"message":"fail","data":1}`,
			map[string]string{"kind": `"error_changed"`, "path": `"/error/data"`, "left": "0", "right": "1"}},
		{"change of outcome", `"result":null`, `"error":{"code":-1,"message":"fail"}`,
			map[string]string{"kind": `"outcome_changed"`, "left": `{"result":null}`, "right": `{"error":{"code":-1,"message":"fail"}}`}},
		{"literal strings", `"result":"0x01"`, `"result":"0x1"`,
			map[string]string{"path": `"/result"`, "left": `"0x01"`, "right": `"0x1"`}},
	}
	responseWith := func(field string) string {
		return `[{"jsonrpc":"2.0","id":1,` + field + `}]`
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := requireReport(t, 1, oneRequest, responseWith(tc.left), responseWith(tc.right))
			if len(out.Findings) != 1 {
				t.Fatalf("unexpected findings: %+v", out.Findings)
			}
			requireFields(t, out.Findings[0], tc.want)
		})
	}
}

func TestEmptyBodies(t *testing.T) {
	out := requireReport(t, 0, `[{"jsonrpc":"2.0","method":"notify"}]`, "", "\r\n")
	if out.Notifications != 1 || len(out.Findings) != 0 {
		t.Fatalf("unexpected report: %+v", out)
	}
	out = requireReport(t, 1, oneRequest, "", "")
	if len(out.Findings) != 2 {
		t.Fatalf("both missing answers must be reported: %+v", out)
	}
}

func TestInvalidInput(t *testing.T) {
	tests := []struct{ name, req, left, note string }{
		{"empty requests", "[]", oneResult, "nonempty"},
		{"object requests", "{}", oneResult, "nonempty"},
		{"duplicate IDs", `[{"jsonrpc":"2.0","method":"x","id":-0},{"jsonrpc":"2.0","method":"y","id":0.0}]`, oneResult, "duplicate request id"},
		{"bool ID", `[{"jsonrpc":"2.0","method":"x","id":false}]`, oneResult, "id must"},
		{"params null", `[{"jsonrpc":"2.0","method":"x","params":null}]`, oneResult, "params must"},
		{"wrong version", `[{"jsonrpc":"1.0","method":"x"}]`, oneResult, "jsonrpc"},
		{"missing method", `[{"jsonrpc":"2.0","id":1}]`, oneResult, "method must"},
		{"empty response array", oneRequest, "[]", "nonempty"},
		{"trailing value", oneRequest, oneResult + " 0", "trailing JSON"},
		{"malformed", oneRequest, `[{"id":1`, "left ("},
		{"repeated object name", oneRequest, `[{"jsonrpc":"2.0","id":1,"result":{"x":1,"x":2}}]`, "duplicate object member"},
		{"missing response ID", oneRequest, `[{"jsonrpc":"2.0","result":null}]`, "missing id"},
		{"both outcome members", oneRequest, `[{"jsonrpc":"2.0","id":1,"error":null,"result":null}]`, "exactly one"},
		{"no outcome members", oneRequest, `[{"jsonrpc":"2.0","id":1}]`, "exactly one"},
		{"bad code", oneRequest, `[{"jsonrpc":"2.0","id":1,"error":{"code":0.1,"message":"failed"}}]`, "code must"},
		{"bad message", oneRequest, `[{"jsonrpc":"2.0","id":1,"error":{"code":1}}]`, "message must"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := invoke(t, tc.req, tc.left, oneResult)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tc.note) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestExampleAndUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"examples/requests.json", "examples/left.json", "examples/right.json"}, &stdout, &stderr)
	want := "calls=3 notifications=0 matched=1 matched_errors=0 findings=2\n" +
		`result_changed id=9007199254740993 method="eth_feeHistory" path="/result/reward/1/0" left="0x3" right="0x4"` + "\n" +
		`outcome_changed id="receipts" method="eth_getBlockReceipts" left={"result":[]} right={"error":{"code":-32601,"message":"Method not found"}}` + "\n"
	if code != 1 || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	for _, tc := range []struct {
		args []string
		code int
	}{{nil, 2}, {[]string{"-h"}, 0}, {[]string{"-unknown"}, 2}} {
		stderr.Reset()
		if got := run(tc.args, io.Discard, &stderr); got != tc.code || !strings.Contains(stderr.String(), "Usage:") {
			t.Errorf("args %v: exit %d, stderr %q", tc.args, got, stderr.String())
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

func TestOutputError(t *testing.T) {
	for _, args := range [][]string{
		{"examples/requests.json", "examples/left.json", "examples/right.json"},
		{"-json", "examples/requests.json", "examples/left.json", "examples/right.json"},
	} {
		var stderr bytes.Buffer
		if got := run(args, failingWriter{}, &stderr); got != 2 || !strings.Contains(stderr.String(), "writer failed") {
			t.Errorf("exit %d, stderr %q", got, stderr.String())
		}
	}
}

func TestResponseOrderDoesNotChangeReport(t *testing.T) {
	left := `[{"jsonrpc":"2.0","id":"z","result":null},{"jsonrpc":"2.0","id":"a","result":null},{"jsonrpc":"2.0","id":1,"result":42}]`
	reversed := `[{"jsonrpc":"2.0","id":1,"result":42},{"jsonrpc":"2.0","id":"a","result":null},{"jsonrpc":"2.0","id":"z","result":null}]`
	if a, b := requireReport(t, 1, oneRequest, left, oneResult), requireReport(t, 1, oneRequest, reversed, oneResult); !reflect.DeepEqual(a, b) {
		t.Fatalf("reports differ: %+v / %+v", a, b)
	}
}
