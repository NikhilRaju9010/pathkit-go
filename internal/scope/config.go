// Package scope reads .pathkitrc.json and decides which workflows count
// (CLAUDE.md D8). Every command that needs a scope (analyze, pathkit test,
// pathkit traces; coverage and report later) gets it from here, so they
// can never disagree about what is in scope.
package scope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the config file PathKit looks for.
const FileName = ".pathkitrc.json"

// Exclude is one workflows.exclude entry.
type Exclude struct {
	Name   string
	Reason string
}

// Config is a checked .pathkitrc.json.
type Config struct {
	Path string // absolute path of the file
	Dir  string // its folder; relative paths in the file are relative to it

	Packages   []string // as written ("./..." when not given)
	Traces     string   // as written, "" when not given
	Include    []string // nil when the file has no "include"
	HasInclude bool
	Exclude    []Exclude

	// Read and checked now, but used only when M6–M8 build their
	// features. No command applies them yet, and none claims to.
	FailUnder  *float64
	HTML       any // true/false, or a file path
	Out        string
	JSON       bool
	NoColor    bool
	AllowStale bool
}

// Abs resolves a path written in the file against the file's folder.
func (c *Config) Abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir, p)
}

// Find returns the config file that applies in folder start: start
// itself, then each parent folder, stopping after the folder that holds
// go.mod (D8). When no go.mod is found at all, only start is checked, so
// a stray file higher up is never used. "" means there is none.
func Find(start string) (string, error) {
	start, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	stop := start // no go.mod above: check start only
	for dir := start; ; {
		if exists(filepath.Join(dir, "go.mod")) {
			stop = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		if p := filepath.Join(dir, FileName); exists(p) {
			return p, nil
		}
		if dir == stop || filepath.Dir(dir) == dir {
			return "", nil
		}
	}
}

func exists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// Load returns the config for a command: the file named by --config
// (explicit), or else the one Find finds from cwd. nil means no config.
func Load(explicit, cwd string) (*Config, error) {
	path := explicit
	if path == "" {
		found, err := Find(cwd)
		if err != nil || found == "" {
			return nil, err
		}
		path = found
	} else if !exists(path) {
		return nil, fmt.Errorf("--config file not found: %s", path)
	}
	return Read(path)
}

// file is the JSON shape; unknown keys are refused.
type file struct {
	Packages   []string       `json:"packages"`
	Traces     *string        `json:"traces"`
	Workflows  *workflowsPart `json:"workflows"`
	FailUnder  *float64       `json:"failUnder"`
	HTML       any            `json:"html"`
	Out        *string        `json:"out"`
	JSON       *bool          `json:"json"`
	NoColor    *bool          `json:"noColor"`
	AllowStale *bool          `json:"allowStale"`
}

type workflowsPart struct {
	Include *[]string         `json:"include"`
	Exclude []json.RawMessage `json:"exclude"`
}

type excludeEntry struct {
	Name   *string `json:"name"`
	Reason *string `json:"reason"`
}

// Read reads and checks one config file. Every error starts with the
// file's absolute path.
func Read(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s: %s", abs, fmt.Sprintf(format, args...))
	}

	var f file
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fail("%s", jsonProblem(data, err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fail("unexpected text after the closing }")
	}

	c := &Config{Path: abs, Dir: filepath.Dir(abs), Packages: f.Packages}
	if c.Packages == nil {
		c.Packages = []string{"./..."}
	}
	for _, p := range c.Packages {
		if strings.TrimSpace(p) == "" {
			return nil, fail(`"packages" has an empty entry`)
		}
	}
	if f.Traces != nil {
		if *f.Traces == "" {
			return nil, fail(`"traces" is empty; remove it to use the default .pathkit/traces`)
		}
		c.Traces = *f.Traces
	}
	if w := f.Workflows; w != nil {
		if w.Include != nil {
			if len(*w.Include) == 0 {
				return nil, fail("workflows.include is empty: remove it to include every workflow, or list the ones you want")
			}
			for _, n := range *w.Include {
				if strings.TrimSpace(n) == "" {
					return nil, fail("workflows.include has an empty name")
				}
			}
			c.Include, c.HasInclude = *w.Include, true
		}
		for i, raw := range w.Exclude {
			var e excludeEntry
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err := d.Decode(&e); err != nil || e.Name == nil {
				return nil, fail(`workflows.exclude[%d] must look like {"name": "OrderWorkflow", "reason": "why it doesn't count"}`, i)
			}
			if strings.TrimSpace(*e.Name) == "" {
				return nil, fail("workflows.exclude[%d] has an empty name", i)
			}
			if e.Reason == nil || strings.TrimSpace(*e.Reason) == "" {
				return nil, fail(`exclude %q needs a "reason"`, *e.Name)
			}
			c.Exclude = append(c.Exclude, Exclude{Name: *e.Name, Reason: *e.Reason})
		}
	}
	if f.FailUnder != nil {
		if *f.FailUnder < 0 || *f.FailUnder > 100 {
			return nil, fail(`"failUnder" must be a number from 0 to 100, got %v`, *f.FailUnder)
		}
		c.FailUnder = f.FailUnder
	}
	switch h := f.HTML.(type) {
	case nil, bool:
		c.HTML = h
	case string:
		if h == "" {
			return nil, fail(`"html" must be true, false, or a file path`)
		}
		c.HTML = h
	default:
		return nil, fail(`"html" must be true, false, or a file path`)
	}
	if f.Out != nil {
		c.Out = *f.Out
	}
	c.JSON = f.JSON != nil && *f.JSON
	c.NoColor = f.NoColor != nil && *f.NoColor
	c.AllowStale = f.AllowStale != nil && *f.AllowStale
	return c, nil
}

// jsonProblem turns a decoding error into plain words, with the line and
// column for syntax errors.
func jsonProblem(data []byte, err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		// Offset is just after the bad character; point at the character.
		line, col := position(data, syn.Offset-1)
		return fmt.Sprintf("invalid JSON at line %d, column %d: %v", line, col, syn)
	case errors.As(err, &typ):
		line, _ := position(data, typ.Offset)
		return fmt.Sprintf("%q has the wrong type (line %d): expected %s, found %s", typ.Field, line, plainType(typ.Type.String()), plainJSON(typ.Value))
	case errors.Is(err, io.EOF):
		return "the file is empty; write {} for no settings"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "invalid JSON: the file ends too early (a missing } or ]?)"
	}
	if k, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return "unknown key " + k
	}
	return err.Error()
}

// plainType names a Go type the way the setup guide does.
func plainType(goType string) string {
	switch goType {
	case "float64":
		return "a number"
	case "bool":
		return "true or false"
	case "string":
		return "text in quotes"
	case "[]string":
		return `a list of text, like ["a", "b"]`
	}
	if strings.HasPrefix(goType, "[]") {
		return "a list"
	}
	return "an object {...}"
}

func plainJSON(kind string) string {
	switch kind {
	case "number":
		return "a number"
	case "string":
		return "text"
	case "bool":
		return "true/false"
	case "array":
		return "a list"
	case "object":
		return "an object"
	}
	return kind
}

func position(data []byte, offset int64) (line, col int) {
	line, col = 1, 1
	for i := int64(0); i < offset && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}
