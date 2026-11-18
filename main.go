package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("rpcbatchdiff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "write the report as JSON")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: rpcbatchdiff [-json] REQUESTS LEFT RIGHT")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err == flag.ErrHelp {
		return 0
	} else if err != nil {
		return 2
	}
	if flags.NArg() != 3 {
		flags.Usage()
		return 2
	}
	req, err := loadRequests(flags.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "requests (%s): %v\n", flags.Arg(0), err)
		return 2
	}
	left, err := loadResponses(flags.Arg(1))
	if err != nil {
		fmt.Fprintf(stderr, "left (%s): %v\n", flags.Arg(1), err)
		return 2
	}
	right, err := loadResponses(flags.Arg(2))
	if err != nil {
		fmt.Fprintf(stderr, "right (%s): %v\n", flags.Arg(2), err)
		return 2
	}
	out := compare(req, left, right)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err = enc.Encode(out)
	} else {
		_, err = io.WriteString(stdout, formatText(out))
	}
	if err != nil {
		fmt.Fprintln(stderr, "write report:", err)
		return 2
	}
	if len(out.Findings) != 0 {
		return 1
	}
	return 0
}

func formatText(out report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "calls=%d notifications=%d matched=%d matched_errors=%d findings=%d\n",
		out.Calls, out.Notifications, out.Matched, out.MatchedErrors, len(out.Findings))
	for _, f := range out.Findings {
		fmt.Fprintf(&b, "%s id=%s", f.Kind, preview(f.ID))
		if f.Method != "" {
			fmt.Fprintf(&b, " method=%q", f.Method)
		}
		if f.Side != "" {
			fmt.Fprintf(&b, " side=%s", f.Side)
		}
		if f.Count != 0 {
			fmt.Fprintf(&b, " count=%d", f.Count)
		}
		if f.Path != "" {
			fmt.Fprintf(&b, " path=%q", f.Path)
		}
		if f.Left != nil || f.Right != nil {
			fmt.Fprintf(&b, " left=%s right=%s", fieldPreview(f.Left), fieldPreview(f.Right))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func fieldPreview(value *any) string {
	if value == nil {
		return "<absent>"
	}
	return preview(*value)
}

func preview(value any) string {
	encoded, _ := json.Marshal(value) // all values originate in JSON input
	const limit = 100
	if len(encoded) <= limit {
		return string(encoded)
	}
	runes := []rune(string(encoded))
	if len(runes) <= limit {
		return string(encoded)
	}
	return string(runes[:limit]) + "…"
}
