package main

import (
	"errors"
	"strings"
	"time"
)

// AllowedNumber is one entry of the SMS allowlist as the Phone page shows it.
// Only Number takes part in authorization; Label and Added exist so the list
// reads like a contact list instead of a column of digits.
type AllowedNumber struct {
	Number string    `json:"number"`
	Label  string    `json:"label,omitempty"`
	Added  time.Time `json:"added,omitempty"`
}

// Display formats a stored 10-digit number the way the UI shows it.
func (n AllowedNumber) Display() string { return formatUSPhone(n.Number) }

func formatUSPhone(raw string) string {
	n := normalizeUSPhone(raw)
	if n == "" {
		return strings.TrimSpace(raw)
	}
	return "+1 (" + n[0:3] + ") " + n[3:6] + "-" + n[6:]
}

// syncAllowedNumbers makes the structured list and the newline list agree.
// AllowedFrom remains authoritative for which numbers are allowed — every
// routing, parsing, and test path already reads it — so records are matched
// against it, unknown records are dropped, missing ones are added, and the
// newline list is rewritten from the normalized result.
func syncAllowedNumbers(gv *GoogleVoiceConfig) {
	numbers, err := normalizeAllowedPhoneList(gv.AllowedFrom)
	if err != nil {
		// An empty or not-yet-valid allowlist is a normal pre-setup state. Keep
		// whatever the user typed so the Phone page can show the problem.
		if strings.TrimSpace(gv.AllowedFrom) == "" {
			gv.AllowedNumbers = nil
		}
		return
	}
	existing := make(map[string]AllowedNumber, len(gv.AllowedNumbers))
	for _, rec := range gv.AllowedNumbers {
		if n := normalizeUSPhone(rec.Number); n != "" {
			rec.Number = n
			existing[n] = rec
		}
	}
	out := make([]AllowedNumber, 0, len(numbers))
	for _, n := range numbers {
		if rec, ok := existing[n]; ok {
			out = append(out, rec)
			continue
		}
		// Numbers carried over from an older install have no recorded date.
		// A zero time renders as "—" rather than inventing one.
		out = append(out, AllowedNumber{Number: n})
	}
	gv.AllowedNumbers = out
	gv.AllowedFrom = strings.Join(numbers, "\n")
}

// addAllowedNumber appends one number with its label. It returns an error the
// UI can show directly for a malformed or duplicate number.
func addAllowedNumber(gv *GoogleVoiceConfig, raw, label string) error {
	n := normalizeUSPhone(raw)
	if n == "" {
		return errors.New("enter a 10-digit US/Canada mobile number, for example (845) 555-1234")
	}
	syncAllowedNumbers(gv)
	for _, rec := range gv.AllowedNumbers {
		if rec.Number == n {
			return errors.New(formatUSPhone(n) + " is already allowed")
		}
	}
	gv.AllowedNumbers = append(gv.AllowedNumbers, AllowedNumber{
		Number: n,
		Label:  strings.TrimSpace(label),
		Added:  time.Now(),
	})
	rewriteAllowedFrom(gv)
	return nil
}

// removeAllowedNumber drops one number from the allowlist. Removing the last
// number is refused: an empty allowlist stops the bridge, and a silent stop is
// worse than an explicit error.
func removeAllowedNumber(gv *GoogleVoiceConfig, raw string) error {
	n := normalizeUSPhone(raw)
	if n == "" {
		return errors.New("that number could not be read")
	}
	syncAllowedNumbers(gv)
	if len(gv.AllowedNumbers) <= 1 {
		return errors.New("keep at least one allowed number — FlipAi cannot accept any text without one")
	}
	out := gv.AllowedNumbers[:0]
	found := false
	for _, rec := range gv.AllowedNumbers {
		if rec.Number == n {
			found = true
			continue
		}
		out = append(out, rec)
	}
	if !found {
		return errors.New(formatUSPhone(n) + " is not on the allowlist")
	}
	gv.AllowedNumbers = out
	rewriteAllowedFrom(gv)
	return nil
}

// labelAllowedNumber renames one entry. The number itself never changes.
func labelAllowedNumber(gv *GoogleVoiceConfig, raw, label string) error {
	n := normalizeUSPhone(raw)
	if n == "" {
		return errors.New("that number could not be read")
	}
	syncAllowedNumbers(gv)
	for i, rec := range gv.AllowedNumbers {
		if rec.Number == n {
			gv.AllowedNumbers[i].Label = strings.TrimSpace(label)
			return nil
		}
	}
	return errors.New(formatUSPhone(n) + " is not on the allowlist")
}

// rewriteAllowedFrom regenerates the newline list the bridge authorizes
// against, keeping it sorted and deduplicated exactly as before.
func rewriteAllowedFrom(gv *GoogleVoiceConfig) {
	parts := make([]string, 0, len(gv.AllowedNumbers))
	for _, rec := range gv.AllowedNumbers {
		if n := normalizeUSPhone(rec.Number); n != "" {
			parts = append(parts, n)
		}
	}
	gv.AllowedFrom = strings.Join(parts, "\n")
	syncAllowedNumbers(gv)
}
