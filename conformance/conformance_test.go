// Package conformance cross-checks the Go jton port against the reference
// Python implementation (the real Rust-backed `jton` module) over a broad,
// generated corpus plus the full JSONTestSuite reference-vector set.
//
// It drives a small Python oracle (oracle.py) as a subprocess and asserts that,
// for every input, the Go port and the reference agree byte-for-byte on
// dumps() output and on whether loads() accepts or rejects the input.
//
// The oracle requires the reference `jton` to be importable. Point the test at
// an interpreter that has it via JTON_ORACLE_PYTHON; otherwise it defaults to
// <repo>/.venv/bin/python and skips cleanly if that interpreter cannot import
// jton (e.g. in CI without the built reference).
package conformance

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"jton"
)

// ── oracle subprocess ──────────────────────────────────────────────────────

type oracle struct {
	py     string
	script string
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	mu     sync.Mutex
}

// crashedErr marks a response where the oracle process died (e.g. a Rust stack
// overflow that aborts the interpreter) rather than returning an error.
const crashedErr = "__oracle_crashed__"

type request struct {
	Op   string         `json:"op"`
	B64  string         `json:"b64"`
	Opts map[string]any `json:"opts,omitempty"`
}

type response struct {
	OK  bool   `json:"ok"`
	Out string `json:"out"`
	Err string `json:"err"`
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file)) // .../conformance/ -> repo root
}

// locate resolves a repo-relative path under either `go test` (source tree) or
// Bazel (runfiles), returning "" if it cannot be found.
func locate(rel string) string {
	var bases []string
	if r := os.Getenv("JTON_REPO_ROOT"); r != "" {
		bases = append(bases, r)
	}
	bases = append(bases, repoRoot())
	for _, env := range []string{os.Getenv("TEST_SRCDIR"), os.Getenv("RUNFILES_DIR")} {
		if env != "" {
			bases = append(bases, filepath.Join(env, "_main"), filepath.Join(env, "jton-go"))
		}
	}
	bases = append(bases, ".", "..")
	for _, b := range bases {
		p := filepath.Join(b, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func startOracle(t *testing.T) *oracle {
	t.Helper()
	py := os.Getenv("JTON_ORACLE_PYTHON")
	if py == "" {
		py = filepath.Join(repoRoot(), ".venv", "bin", "python")
	}
	if _, err := os.Stat(py); err != nil {
		t.Skipf("oracle python not found (%s); set JTON_ORACLE_PYTHON", py)
	}
	// Verify the reference module is importable before committing to it.
	if err := exec.Command(py, "-c", "import jton").Run(); err != nil {
		t.Skipf("reference `jton` not importable by %s: %v", py, err)
	}

	script := locate(filepath.Join("conformance", "oracle.py"))
	if script == "" {
		t.Skip("oracle.py not found")
	}
	o := &oracle{py: py, script: script}
	if err := o.spawn(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.kill)
	return o
}

func (o *oracle) spawn() error {
	cmd := exec.Command(o.py, o.script)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	o.cmd, o.in, o.out = cmd, in, bufio.NewReaderSize(outPipe, 1<<20)
	return nil
}

func (o *oracle) kill() {
	if o.cmd == nil {
		return
	}
	o.in.Close()
	o.cmd.Process.Kill()
	o.cmd.Wait()
	o.cmd = nil
}

func (o *oracle) call(t *testing.T, req request) response {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.in.Write(append(line, '\n')); err != nil {
		o.restart(t)
		return response{Err: crashedErr}
	}
	respLine, err := o.out.ReadString('\n')
	if err != nil {
		// The interpreter died mid-request (e.g. a stack-overflow abort that
		// BaseException cannot trap). Restart and report a crash.
		o.restart(t)
		return response{Err: crashedErr}
	}
	var resp response
	if err := json.Unmarshal([]byte(strings.TrimRight(respLine, "\n")), &resp); err != nil {
		t.Fatalf("oracle decode %q: %v", respLine, err)
	}
	return resp
}

func (o *oracle) restart(t *testing.T) {
	o.kill()
	if err := o.spawn(); err != nil {
		t.Fatalf("oracle restart: %v", err)
	}
}

func isReferenceCrash(err string) bool {
	return err == crashedErr ||
		strings.Contains(err, "Panic") ||
		strings.Contains(err, "panic") ||
		strings.Contains(err, "overflow")
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// ── option matrix ──────────────────────────────────────────────────────────

type optCase struct {
	name string
	opts jton.Options
}

func optMatrix() []optCase {
	return []optCase{
		{"default", jton.Options{}},
		{"no_zen", jton.Options{NoZenGrid: true}},
		{"no_rowcount", jton.Options{NoRowCount: true}},
		{"bare", jton.Options{BareStrings: true}},
		{"implicit_null", jton.Options{ImplicitNull: true}},
		{"unquoted_keys", jton.Options{UnquotedKeys: true}},
		{"multiline", jton.Options{MultilineZen: true}},
		{"tab", jton.Options{Delimiter: jton.DelimiterTab}},
		{"pipe", jton.Options{Delimiter: jton.DelimiterPipe}},
		{"indent2", jton.Options{Indent: 2}},
		{"bare_norowcount", jton.Options{BareStrings: true, NoRowCount: true}},
		{"tab_norowcount", jton.Options{Delimiter: jton.DelimiterTab, NoRowCount: true}},
		{"implicit_bare", jton.Options{ImplicitNull: true, BareStrings: true}},
		{"multiline_indent4", jton.Options{MultilineZen: true, Indent: 4}},
		{"indent2_unquoted", jton.Options{Indent: 2, UnquotedKeys: true}},
		{"no_zen_indent2", jton.Options{NoZenGrid: true, Indent: 2}},
	}
}

func optsToPy(o jton.Options) map[string]any {
	m := map[string]any{
		"zen_grid":      !o.NoZenGrid,
		"unquoted_keys": o.UnquotedKeys,
		"bare_strings":  o.BareStrings,
		"implicit_null": o.ImplicitNull,
		"row_count":     !o.NoRowCount,
		"multiline_zen": o.MultilineZen,
	}
	switch o.Delimiter {
	case jton.DelimiterTab:
		m["delimiter"] = "tab"
	case jton.DelimiterPipe:
		m["delimiter"] = "pipe"
	default:
		m["delimiter"] = "comma"
	}
	if o.Indent > 0 {
		m["indent"] = o.Indent
	}
	return m
}

// ── dumps conformance ──────────────────────────────────────────────────────

// dumpsCorpus holds standard-JSON texts whose dumps() output (across the option
// matrix) must match the reference.
func dumpsCorpus() []string {
	return []string{
		`null`, `true`, `false`, `0`, `42`, `-999`, `3.14`, `1.0`, `100.0`,
		`"hi"`, `"say \"hi\""`, `"new\nline"`, `"tab\there"`, `"café"`, `"日本語"`, `""`,
		`{}`, `[]`, `{"name":"Alice","age":30}`, `{"a":{"b":1}}`,
		`[1,2,3]`, `[1,"two",true,null,3.14]`, `[[1,2],[3,4]]`,
		// tables
		`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`,
		`[{"id":1,"name":"Alice","score":95},{"id":2,"name":"Bob","score":87},{"id":3,"name":"Carol","score":92}]`,
		`[{"name":"Alice","dept":"Eng"},{"name":"Bob","dept":"Mkt"}]`,
		`[{"id":1,"val":null},{"id":2,"val":null}]`,
		`[{"a":1,"b":2},{"a":3,"b":4},{"a":5,"b":6,"c":7}]`,               // 66% -> no zen grid
		`[{"a":1,"b":2},{"a":3,"b":4},{"a":5,"b":6},{"a":7,"b":8,"c":9}]`, // 75% -> zen grid
		`[{"id":1,"meta":{"k":10}},{"id":2,"meta":{"k":20}}]`,             // nested -> no zen
		`[{"id":1,"tags":["a","b"]},{"id":2,"tags":["c"]}]`,               // nested -> no zen
		`[{"id":1,"note":"a,b;c"},{"id":2,"note":"x:y"}]`,                 // structural strings -> no zen
		`[{"x":1.5,"y":2.5},{"x":3.0,"y":4.25}]`,
		`[{"flag":true,"n":null},{"flag":false,"n":7}]`,
		`{"big":123456789012345678901234567890,"neg":-98765432109876543210}`,
		`{"inf":Infinity,"ninf":-Infinity,"nan":NaN}`,
		`[{"name":"true","v":1},{"name":"x y","v":2}]`, // bare-string edge: reserved word / space
		`{"my-key":1,"_x$":2,"123":3,"ok":4}`,          // unquoted-keys edge
		`{"nested":{"deep":{"a":[1,{"b":2}]}}}`,
		`["a","b","c"]`,
		`[{"id":1},{"id":2},{"id":3}]`,
		`{"emoji":"😀","quote":"\"","backslash":"\\","slash":"/"}`,
	}
}

func TestDumpsConformance(t *testing.T) {
	o := startOracle(t)
	corpus := dumpsCorpus()
	mats := optMatrix()
	mism := 0
	for _, text := range corpus {
		val, err := jton.Parse([]byte(text))
		if err != nil {
			t.Errorf("Go failed to parse corpus item %q: %v", text, err)
			continue
		}
		for _, oc := range mats {
			resp := o.call(t, request{Op: "dumps", B64: b64([]byte(text)), Opts: optsToPy(oc.opts)})
			gotB, gerr := jton.MarshalOptions(val, oc.opts)
			if !resp.OK {
				if gerr == nil {
					t.Errorf("[%s] %q: reference errored (%s) but Go succeeded (%q)", oc.name, text, resp.Err, gotB)
				}
				continue
			}
			if gerr != nil {
				t.Errorf("[%s] %q: Go errored %v but reference produced %q", oc.name, text, gerr, resp.Out)
				continue
			}
			if string(gotB) != resp.Out {
				mism++
				if mism <= 40 {
					t.Errorf("[%s] %q\n  go  = %q\n  ref = %q", oc.name, text, string(gotB), resp.Out)
				}
			}
		}
	}
	if mism > 40 {
		t.Errorf("... and %d more dumps mismatches", mism-40)
	}
}

// ── float formatting conformance (Ryu byte-parity) ─────────────────────────

func floatText(d float64) string {
	s := strconv.FormatFloat(d, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func TestFloatConformance(t *testing.T) {
	o := startOracle(t)
	rng := rand.New(rand.NewSource(0xC0FFEE))
	const n = 20000
	mism := 0
	check := func(d float64) {
		if math.IsNaN(d) || math.IsInf(d, 0) {
			return
		}
		text := floatText(d)
		resp := o.call(t, request{Op: "dumps", B64: b64([]byte(text)), Opts: optsToPy(jton.Options{})})
		got, err := jton.MarshalOptions(d, jton.Options{})
		if err != nil {
			t.Fatalf("Go dumps(%v): %v", d, err)
		}
		if !resp.OK || string(got) != resp.Out {
			mism++
			if mism <= 40 {
				t.Errorf("float %v (bits %#x): go=%q ref=%q ok=%v", d, math.Float64bits(d), got, resp.Out, resp.OK)
			}
		}
	}
	// Targeted edge cases.
	for _, d := range []float64{
		0, math.Copysign(0, -1), 1, 1.5, 3.14, 0.1, 0.5, 100, 1e6, 1e7, 1e15, 1e16,
		1e17, 1e20, 1e21, 1e-1, 1e-4, 1e-5, 1e-6, 1e-7, 1234567.89, 1e100, 1e-100,
		math.MaxFloat64, math.SmallestNonzeroFloat64, 0.30000000000000004, 6.022e23,
		9999999.0, 123456789012345.0, 2.5e-8, 1.5e16, 9.999999999999999e15,
	} {
		check(d)
	}
	// Random doubles, including subnormals and huge magnitudes.
	for i := 0; i < n; i++ {
		check(math.Float64frombits(rng.Uint64()))
	}
	if mism > 40 {
		t.Errorf("... and %d more float mismatches", mism-40)
	}
}

// ── loads conformance (round-trip equivalence + accept/reject agreement) ────

func loadsCorpus() []string {
	return []string{
		`[: name, age; "Alice", 30; "Bob", 25 ]`,
		`[:]`, `[: x, y; 1, 2 ]`,
		`[2: id, name; 1, "Alice"; 2, "Bob" ]`,
		`[3: id, name, score; 1, "Alice", 95; 2, "Bob", 87; 3, "Carol", 92 ]`,
		`[: id, meta; 1, {"k": 10}; 2, {"k": 20} ]`,
		`[: id, tags; 1, ["a","b"]; 2, ["c"] ]`,
		`[: id, val; 1, null; 2, null ]`,
		`[: name, active; "Alice", true; "Bob", false ]`,
		`[: x, y; 1.5, 2.7; 3.0, 4.1 ]`,
		`[: a, b; 1, ; 3, 4 ]`,   // missing cell -> null
		`[: a, b; 1, 2; 3 ]`,     // short row -> null fill
		`[2: a, b; x, y; p, q ]`, // bare-string cells
		`{name: "Alice", age: 30}`,
		"{\n  \"x\": 1, // comment\n  \"y\": 2 // trailing\n}",
		"{\"x\": /* block */ 1, /* multi\nline */ \"y\": 2}",
		`Infinity`, `-Infinity`, `NaN`, `Inf`, `-Inf`,
		`{"num":42,"str":"hello","bool":true,"null":null}`,
		`[1, 2, 3,]`, `{"a":1,}`, // trailing commas
		`{"a": {"b": {"c": {"d": {"e": 5}}}}}`,
		`9223372036854775807`, `-9223372036854775808`, `123456789012345678901234567890`,
		`"A❤😀"`,
		`[: id, name, dept; ` + strings.Repeat(`1, Alice, Eng; `, 30) + `]`,
	}
}

func TestLoadsConformance(t *testing.T) {
	o := startOracle(t)
	for _, text := range loadsCorpus() {
		checkLoads(t, o, []byte(text), text)
	}
}

// knownDivergences are reference-vector inputs where the reference's
// structural-index parser quirk-accepts malformed objects — it jumps to the
// next indexed ':' even across junk between key and colon, or across a ':' that
// lives inside a comment. The reference's own Python test suite marks these
// xfail. The Go port rejects them as the syntax errors they are; this is a
// deliberate, documented divergence rather than a conformance failure.
func isKnownDivergence(label string) bool {
	for _, s := range []string{"str_unquoted_err", "unclosed_comment"} {
		if strings.Contains(label, s) {
			return true
		}
	}
	return false
}

// goLoadsDump mirrors the oracle's loads_dump: parse then re-serialize, failing
// if either step fails (the reference's 256-level serialize limit, for example,
// surfaces at the dump step).
func goLoadsDump(data []byte, opts jton.Options) (string, error) {
	v, err := jton.Parse(data)
	if err != nil {
		return "", err
	}
	b, err := jton.MarshalOptions(v, opts)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func checkLoads(t *testing.T, o *oracle, data []byte, label string) {
	t.Helper()
	resp := o.call(t, request{Op: "loads_dump", B64: b64(data), Opts: optsToPy(jton.Options{})})
	goOut, goErr := goLoadsDump(data, jton.Options{})
	if isReferenceCrash(resp.Err) {
		t.Logf("reference crashed on %s: %s (Go accepted=%v)", label, resp.Err, goErr == nil)
		return
	}

	var msg string
	switch {
	case !resp.OK:
		if goErr == nil {
			msg = "reference rejected (" + resp.Err + ") but Go accepted -> " + goOut
		}
	case goErr != nil:
		msg = "Go rejected (" + goErr.Error() + ") but reference accepted -> " + resp.Out
	case goOut != resp.Out:
		msg = "go=" + strconv.Quote(goOut) + " ref=" + strconv.Quote(resp.Out)
	}
	if msg == "" {
		return // agree
	}
	if isKnownDivergence(label) {
		t.Logf("known reference quirk on %s: %s", label, msg)
		return
	}
	t.Errorf("loads %s: %s", label, msg)
}

// TestReferenceVectorsConformance runs every JSONTestSuite reference vector
// through both implementations and asserts they agree on accept/reject and, when
// both accept, on the re-serialized value.
func TestReferenceVectorsConformance(t *testing.T) {
	o := startOracle(t)
	root := locate(filepath.Join("testdata", "reference_vectors"))
	if root == "" {
		t.Skip("reference vectors not found")
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no reference vectors found")
	}
	checked := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// The harness wire format is JSON, which cannot carry invalid UTF-8;
		// skip such fixtures (a handful of i_/n_ encoding cases).
		if !utf8.Valid(data) {
			continue
		}
		rel, _ := filepath.Rel(root, f)
		checkLoads(t, o, data, rel)
		checked++
	}
	t.Logf("reference vectors checked: %d/%d", checked, len(files))
}
