package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Many guards in this repo are assertions about source text, and a good number
// of them live in Windows-only test files. Those never run on a Linux checkout,
// so a driver edit that breaks one is not discovered until the Windows job in
// the release workflow -- after the build, the signing and the attestation. That
// is exactly how a release was lost.
//
// This runs those assertions here. It reads each Windows test function that
// reads one source file, finds its plain strings.Contains checks, and holds them
// to the same polarity the test does: "if !Contains" means the text must be
// present, "if Contains" means it must be absent. Anything it cannot read with
// confidence it skips, so it never invents a failure of its own.
func TestWindowsOnlySourceAssertionsHoldOnThisCheckout(t *testing.T) {
	testFiles, err := filepath.Glob("*_windows_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(testFiles) == 0 {
		t.Skip("no Windows-only test files")
	}
	funcRE := regexp.MustCompile(`(?s)func (Test\w+)\(t \*testing\.T\) \{(.*?)\n\}\n`)
	readRE := regexp.MustCompile(`os\.ReadFile\("([^"]+\.go)"\)`)
	containsRE := regexp.MustCompile(`if (!?)strings\.Contains\([A-Za-z_][A-Za-z0-9_]*, ("(?:[^"\\]|\\.)*")\)`)
	rangeRE := regexp.MustCompile(`(?s)for _, (\w+) := range \[\]string\{([^}]*)\}\s*\{\s*if (!?)strings\.Contains\([A-Za-z_][A-Za-z0-9_]*, (\w+)\)`)
	literalRE := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

	checked := 0
	for _, testFile := range testFiles {
		source := readGoSource(t, testFile)
		for _, fn := range funcRE.FindAllStringSubmatch(source, -1) {
			name, body := fn[1], fn[2]
			reads := readRE.FindAllStringSubmatch(body, -1)
			// Only the unambiguous case: the function reads exactly one file, so
			// every Contains check in it is about that file.
			if len(reads) != 1 {
				continue
			}
			target := reads[0][1]
			if _, err := os.Stat(target); err != nil {
				continue
			}
			targetSource := readGoSource(t, target)
			// The other common shape: a list of literals ranged over, each checked
			// with the same polarity. That is where most of these guards live.
			for _, loop := range rangeRE.FindAllStringSubmatch(body, -1) {
				literals, wantPresent := loop[2], loop[3] == "!"
				if loop[1] != loop[4] {
					continue
				}
				for _, quoted := range literalRE.FindAllString(literals, -1) {
					literal, err := unquoteGoString(quoted)
					if err != nil || literal == "" {
						continue
					}
					checked++
					if present := strings.Contains(targetSource, literal); present != wantPresent {
						if wantPresent {
							t.Errorf("%s (in %s) requires %q in %s, and it is not there", name, testFile, literal, target)
						} else {
							t.Errorf("%s (in %s) forbids %q in %s, and it is there", name, testFile, literal, target)
						}
					}
				}
			}
			for _, check := range containsRE.FindAllStringSubmatch(body, -1) {
				wantPresent := check[1] == "!"
				literal, err := unquoteGoString(check[2])
				if err != nil {
					continue
				}
				if literal == "" {
					continue
				}
				checked++
				if present := strings.Contains(targetSource, literal); present != wantPresent {
					if wantPresent {
						t.Errorf("%s (in %s) requires %q in %s, and it is not there", name, testFile, literal, target)
					} else {
						t.Errorf("%s (in %s) forbids %q in %s, and it is there", name, testFile, literal, target)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Skip("no readable source assertions found in Windows-only tests")
	}
	t.Logf("checked %d source assertions from %d Windows-only test files", checked, len(testFiles))
}

// unquoteGoString reads a double-quoted Go literal without pulling in a parser.
// Only the escapes these assertions actually use are handled; anything else is
// reported so the caller skips the check rather than testing the wrong text.
func unquoteGoString(quoted string) (string, error) {
	if len(quoted) < 2 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' {
		return "", os.ErrInvalid
	}
	body := quoted[1 : len(quoted)-1]
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			out.WriteByte(body[i])
			continue
		}
		i++
		if i >= len(body) {
			return "", os.ErrInvalid
		}
		switch body[i] {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case '"':
			out.WriteByte('"')
		case '\\':
			out.WriteByte('\\')
		default:
			return "", os.ErrInvalid
		}
	}
	return out.String(), nil
}
