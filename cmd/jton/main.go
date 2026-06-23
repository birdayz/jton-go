// Command jton converts between JSON and JTON (JSON Tabular Object Notation),
// the token-efficient Zen Grid encoding.
//
// Usage:
//
//	jton [flags] [file]      # encode JSON -> JTON (Zen Grid), reads stdin if no file
//	jton --decode [file]     # decode JTON -> JSON
//	jton --hint [style]      # print an LLM system-prompt hint and exit
//	jton --version
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/birdayz/jton-go"
)

const version = "1.0.3-go"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "jton:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		decode       = flag.Bool("decode", false, "decode JTON to JSON instead of encoding")
		noZenGrid    = flag.Bool("no-zen-grid", false, "disable Zen Grid; emit compact JSON")
		noRowCount   = flag.Bool("no-row-count", false, "omit the [N: ...] row count prefix")
		tab          = flag.Bool("tab", false, "use tab as the Zen Grid delimiter")
		pipe         = flag.Bool("pipe", false, "use pipe ( | ) as the Zen Grid delimiter")
		bareStrings  = flag.Bool("bare-strings", false, "write identifier string values without quotes")
		implicitNull = flag.Bool("implicit-null", false, "write null Zen Grid cells as empty")
		unquotedKeys = flag.Bool("unquoted-keys", false, "write identifier object keys without quotes")
		multiline    = flag.Bool("multiline", false, "emit the multi-line Zen Grid format")
		indent       = flag.Int("indent", 0, "pretty-print with this many spaces per level")
		output       = flag.String("o", "", "write output to this file instead of stdout")
		showVersion  = flag.Bool("version", false, "print version and exit")
		hint         = flag.String("hint", "", "print an LLM format hint (style: zen_grid, zen_grid_rowcount, multiline, tab) and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("jton", version)
		return nil
	}
	if isFlagSet("hint") {
		fmt.Println(formatHint(*hint))
		return nil
	}

	input, err := readInput(flag.Arg(0))
	if err != nil {
		return err
	}

	var out []byte
	if *decode {
		out, err = jton.ToJSON(input)
		if err != nil {
			return err
		}
		out = append(out, '\n')
	} else {
		opts := jton.Options{
			NoZenGrid:    *noZenGrid,
			NoRowCount:   *noRowCount,
			BareStrings:  *bareStrings,
			ImplicitNull: *implicitNull,
			UnquotedKeys: *unquotedKeys,
			MultilineZen: *multiline,
			Indent:       *indent,
		}
		switch {
		case *tab:
			opts.Delimiter = jton.DelimiterTab
		case *pipe:
			opts.Delimiter = jton.DelimiterPipe
		}
		v, err := jton.Parse(input)
		if err != nil {
			return err
		}
		out, err = jton.MarshalOptions(v, opts)
		if err != nil {
			return err
		}
		out = append(out, '\n')
	}

	if *output != "" {
		return os.WriteFile(*output, out, 0o644)
	}
	_, err = os.Stdout.Write(out)
	return err
}

func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func isFlagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func formatHint(style string) string {
	switch style {
	case "multiline":
		return "Data is in JTON Multiline Zen Grid format (TOON-compatible).\n" +
			"Header line: [N]{col1,col2,col3}: where N is the row count.\n" +
			"Each following indented line is one row with comma-separated values.\n" +
			"Example:\n  [3]{id,name,score}:\n    1,Alice,95\n    2,Bob,87\n    3,Carol,92"
	case "zen_grid_rowcount":
		return "Data is in JTON Zen Grid format with explicit row count.\n" +
			"Format: [N: col1, col2, col3; row1val1, row1val2, row1val3; ... ]\n" +
			"N is the number of data rows. The first segment lists field names.\n" +
			"Example: [3: id, name, score; 1, Alice, 95; 2, Bob, 87; 3, Carol, 92 ]"
	case "tab":
		return "Data is in JTON Zen Grid tab-delimited format.\n" +
			"Fields and values are separated by tab characters.\n" +
			"Each semicolon-separated segment after the headers is one record."
	default:
		return "Data is in JTON Zen Grid format.\n" +
			"Format: [N: col1, col2, col3; row1val1, row1val2, row1val3; row2val1, ... ]\n" +
			"N is the optional row count. The first semicolon-separated segment is the header.\n" +
			"Each subsequent segment is one record; values are comma-separated in header order.\n" +
			"Example: [3: id, name, score; 1, Alice, 95; 2, Bob, 87; 3, Carol, 92 ]"
	}
}
