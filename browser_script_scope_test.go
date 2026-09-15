package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A page script that calls something it never declares fails only when that
// line is reached. FlipAi shipped exactly that: a guard added to one branch of
// ChatGPT's turn, with the function it calls left out of the file because the
// edit that added it never got written. Every driver test passed -- parsing it
// is fine, and the real-browser harness never reaches that branch -- and the
// turn died in the user's hands with
//
//	ReferenceError: pageFurnitureText is not defined
//
// So the scripts are checked for identifiers they call and never declare.
func TestPageScriptsOnlyCallWhatTheyDeclare(t *testing.T) {
	declRE := regexp.MustCompile(`(?:const|let|var|function)\s+([A-Za-z_$][\w$]*)`)
	paramRE := regexp.MustCompile(`\(\s*([A-Za-z_$][\w$]*(?:\s*,\s*[A-Za-z_$][\w$]*)*)\s*\)\s*=>`)
	oneArgRE := regexp.MustCompile(`(?:^|[^\w$.])([A-Za-z_$][\w$]*)\s*=>`)
	forOfRE := regexp.MustCompile(`for\s*\(\s*(?:const|let|var)\s+([A-Za-z_$][\w$]*)`)
	callRE := regexp.MustCompile(`(?:^|[^\w$.'"])([a-z][\w$]*)\s*\(`)

	// Globals and built-ins a page script may legitimately call.
	known := map[string]bool{}
	for _, n := range strings.Fields(`
		alert atob btoa clearInterval clearTimeout confirm decodeURI decodeURIComponent
		encodeURI encodeURIComponent escape eval fetch getComputedStyle getSelection
		isFinite isNaN parseFloat parseInt prompt queueMicrotask requestAnimationFrame
		setInterval setTimeout unescape structuredClone addEventListener removeEventListener
		if for while switch catch return typeof function of in do else new delete void
		await async yield throw try finally case default class extends super this
	`) {
		known[n] = true
	}

	for _, file := range []string{
		"chatgpt_webview_windows.go", "claude_chat_webview_windows.go",
		"copilot_chat_webview_windows.go", "gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go", "muse_chat_webview_windows.go",
	} {
		src := readGoSource(t, file)
		for _, m := range jsConstRE.FindAllStringSubmatch(src, -1) {
			name, js := m[1], stripJSLiterals(m[2])
			declared := map[string]bool{}
			for _, d := range declRE.FindAllStringSubmatch(js, -1) {
				declared[d[1]] = true
			}
			for _, d := range forOfRE.FindAllStringSubmatch(js, -1) {
				declared[d[1]] = true
			}
			// Arrow-function parameters are declarations too.
			for _, d := range paramRE.FindAllStringSubmatch(js, -1) {
				for _, p := range strings.Split(d[1], ",") {
					declared[strings.TrimSpace(p)] = true
				}
			}
			for _, d := range oneArgRE.FindAllStringSubmatch(js, -1) {
				declared[d[1]] = true
			}
			var missing []string
			seen := map[string]bool{}
			for _, c := range callRE.FindAllStringSubmatch(js, -1) {
				id := c[1]
				if declared[id] || known[id] || seen[id] {
					continue
				}
				seen[id] = true
				missing = append(missing, id)
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				t.Errorf("%s: %s calls %v, which it never declares; that is a ReferenceError the moment the line runs", file, name, missing)
			}
		}
	}
}

// stripJSLiterals blanks out strings and regular expressions so the scan reads
// code and not the text inside it. Without this, a pattern like /^chatgpt (can|
// may) / looks exactly like a call to chatgpt().
func stripJSLiterals(js string) string {
	var out strings.Builder
	out.Grow(len(js))
	const (
		code = iota
		single
		double
		backtickish
		regex
	)
	state := code
	prev := byte(0)
	// A "/" inside a character class does not end the regular expression:
	// /(^|[/?#=&_-])work(...)/ ended it early, and "work(" then read as a call.
	inClass := false
	for i := 0; i < len(js); i++ {
		c := js[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(js) && js[i+1] == '/':
				// A line comment. Its prose is full of apostrophes, and reading
				// one as the start of a string blanks out the rest of the script.
				for i < len(js) && js[i] != '\n' {
					i++
				}
				out.WriteByte('\n')
				continue
			case c == '/' && i+1 < len(js) && js[i+1] == '*':
				for i+1 < len(js) && !(js[i] == '*' && js[i+1] == '/') {
					i++
				}
				i++
				out.WriteByte(' ')
				continue
			case c == '\'':
				state = single
				out.WriteByte(' ')
			case c == '"':
				state = double
				out.WriteByte(' ')
			case c == '/' && i+1 < len(js) && js[i+1] != '/' && js[i+1] != '*' && regexCanStartAfter(prev):
				state = regex
				out.WriteByte(' ')
			default:
				out.WriteByte(c)
			}
		case single, double, regex:
			closer := byte('\'')
			if state == double {
				closer = '"'
			} else if state == regex {
				closer = '/'
			}
			if c == '\\' {
				i++
				out.WriteByte(' ')
				continue
			}
			if state == regex {
				if c == '[' {
					inClass = true
				} else if c == ']' {
					inClass = false
				}
			}
			if c == closer && !(state == regex && inClass) {
				state = code
				inClass = false
			}
			out.WriteByte(' ')
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			prev = c
		}
	}
	return out.String()
}

// regexCanStartAfter reports whether a "/" at this point begins a regular
// expression rather than a division.
func regexCanStartAfter(prev byte) bool {
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^':
		return true
	}
	return false
}
