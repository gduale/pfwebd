package pfctl

import (
	"regexp"
	"strconv"
	"strings"
)

// Stat is a single named counter from "pfctl -s info".
type Stat struct {
	Name  string  `json:"name"`
	Total int64   `json:"total"`
	Rate  float64 `json:"rate"`
}

// Info is the parsed output of "pfctl -s info".
type Info struct {
	Enabled    bool   `json:"enabled"`
	Since      string `json:"since"` // e.g. "0 days 21:11:33"
	StateTable []Stat `json:"stateTable"`
	Counters   []Stat `json:"counters"`
}

// State is one entry from "pfctl -s states".
type State struct {
	Interface   string `json:"interface"`
	Proto       string `json:"proto"`
	Source      string `json:"source"`
	Direction   string `json:"direction"`
	Destination string `json:"destination"`
	State       string `json:"state"`
}

// Rule is one entry from "pfctl -s rules -v".
type Rule struct {
	Text        string `json:"text"`
	Evaluations int64  `json:"evaluations"`
	Packets     int64  `json:"packets"`
	Bytes       int64  `json:"bytes"`
	States      int64  `json:"states"`
}

// ParseInfo parses the output of "pfctl -s info".
func ParseInfo(out string) Info {
	var info Info
	section := ""
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "Status:"):
			info.Enabled = strings.Contains(trimmed, "Enabled")
			// "Status: Enabled for 0 days 21:11:33           Debug: err"
			if i := strings.Index(trimmed, "for "); i >= 0 {
				rest := trimmed[i+len("for "):]
				if j := strings.Index(rest, "  "); j >= 0 {
					rest = rest[:j]
				}
				info.Since = strings.TrimSpace(rest)
			}
			continue
		case strings.HasPrefix(trimmed, "State Table"):
			section = "state"
			continue
		case strings.HasPrefix(trimmed, "Counters"):
			section = "counters"
			continue
		}
		// Stat lines are indented; anything else starts a section we
		// do not parse (e.g. "Interface Stats for em0").
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			section = ""
			continue
		}
		stat, ok := parseStatLine(trimmed)
		if !ok {
			continue
		}
		switch section {
		case "state":
			info.StateTable = append(info.StateTable, stat)
		case "counters":
			info.Counters = append(info.Counters, stat)
		}
	}
	return info
}

// parseStatLine parses lines like:
//
//	"current entries                       42"
//	"searches                         1326778           17.4/s"
func parseStatLine(s string) (Stat, bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return Stat{}, false
	}
	numIdx := -1
	for i, f := range fields {
		if _, err := strconv.ParseInt(f, 10, 64); err == nil {
			numIdx = i
			break
		}
	}
	if numIdx <= 0 {
		return Stat{}, false
	}
	stat := Stat{Name: strings.Join(fields[:numIdx], " ")}
	stat.Total, _ = strconv.ParseInt(fields[numIdx], 10, 64)
	if len(fields) > numIdx+1 {
		rate := strings.TrimSuffix(fields[numIdx+1], "/s")
		stat.Rate, _ = strconv.ParseFloat(rate, 64)
	}
	return stat, true
}

// ParseStates parses the output of "pfctl -s states". Lines look like:
//
//	all tcp 192.168.1.1:22 <- 192.168.1.100:53421       ESTABLISHED:ESTABLISHED
//	em0 tcp 81.65.12.34:60123 (192.168.1.100:50113) -> 151.101.1.140:443       ESTABLISHED:ESTABLISHED
func ParseStates(out string) []State {
	var states []State
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		dirIdx := -1
		for i, f := range fields {
			if f == "->" || f == "<-" {
				dirIdx = i
				break
			}
		}
		// Source occupies at least fields[2], destination at least one
		// field before the trailing state description.
		if dirIdx < 3 || dirIdx >= len(fields)-2 {
			continue
		}
		states = append(states, State{
			Interface:   fields[0],
			Proto:       fields[1],
			Source:      strings.Join(fields[2:dirIdx], " "),
			Direction:   fields[dirIdx],
			Destination: strings.Join(fields[dirIdx+1:len(fields)-1], " "),
			State:       fields[len(fields)-1],
		})
	}
	return states
}

var ruleStatsRe = regexp.MustCompile(
	`\[\s*Evaluations:\s*(\d+)\s+Packets:\s*(\d+)\s+Bytes:\s*(\d+)\s+States:\s*(\d+)\s*\]`)

// ParseRules parses the output of "pfctl -s rules -v": each rule line is
// followed by an indented statistics line.
func ParseRules(out string) []Rule {
	var rules []Rule
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := ruleStatsRe.FindStringSubmatch(trimmed); m != nil {
			if len(rules) > 0 {
				r := &rules[len(rules)-1]
				r.Evaluations, _ = strconv.ParseInt(m[1], 10, 64)
				r.Packets, _ = strconv.ParseInt(m[2], 10, 64)
				r.Bytes, _ = strconv.ParseInt(m[3], 10, 64)
				r.States, _ = strconv.ParseInt(m[4], 10, 64)
			}
			continue
		}
		// Other indented lines are verbose details we ignore.
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		rules = append(rules, Rule{Text: trimmed})
	}
	return rules
}
