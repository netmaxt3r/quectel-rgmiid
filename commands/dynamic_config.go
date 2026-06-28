package commands

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// DynamicConfig represents a registered configuration with a name and prefix command.
type DynamicConfig struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// DynamicSubcommand represents a parsed subcommand within a dynamic config format.
type DynamicSubcommand struct {
	Name      string   `json:"name"`
	RawFormat string   `json:"raw_format"`
	Arguments []string `json:"arguments"`
}

// DynamicConfigState keeps track of the registered config, its subcommands, and cached values.
type DynamicConfigState struct {
	Config      DynamicConfig       `json:"config"`
	Subcommands []DynamicSubcommand `json:"subcommands"`
	Values      map[string][]string `json:"values"`
	ValuesMu    sync.RWMutex        `json:"-"`
}

// NewDynamicConfigState creates a new state instance.
func NewDynamicConfigState(cfg DynamicConfig, subcommands []DynamicSubcommand) *DynamicConfigState {
	return &DynamicConfigState{
		Config:      cfg,
		Subcommands: subcommands,
		Values:      make(map[string][]string),
	}
}

// GetValue returns the cached value for a subcommand in a thread-safe manner.
func (s *DynamicConfigState) GetValue(subname string) []string {
	s.ValuesMu.RLock()
	defer s.ValuesMu.RUnlock()
	return s.Values[strings.ToLower(subname)]
}

// SetValue sets the cached value for a subcommand in a thread-safe manner.
func (s *DynamicConfigState) SetValue(subname string, val []string) {
	s.ValuesMu.Lock()
	defer s.ValuesMu.Unlock()
	s.Values[strings.ToLower(subname)] = val
}

var (
	dynamicConfigsMu sync.RWMutex
	dynamicConfigs   = []DynamicConfig{
		{Name: "QMAP", Command: "QMAP"},
		{Name: "QNWPREFCFG", Command: "QNWPREFCFG"},
		{Name: "QNWCFG", Command: "QNWCFG"},
		{Name: "QCFG", Command: "QCFG"},
	}
)

// RegisterDynamicConfig registers a new dynamic config.
func RegisterDynamicConfig(name, command string) {
	dynamicConfigsMu.Lock()
	defer dynamicConfigsMu.Unlock()
	dynamicConfigs = append(dynamicConfigs, DynamicConfig{Name: name, Command: command})
}

// GetDynamicConfigs returns a list of registered dynamic configurations.
func GetDynamicConfigs() []DynamicConfig {
	dynamicConfigsMu.RLock()
	defer dynamicConfigsMu.RUnlock()
	res := make([]DynamicConfig, len(dynamicConfigs))
	copy(res, dynamicConfigs)
	return res
}

// ParseDynamicConfigResponse parses the raw response from AT+<Command>=? into subcommands.
func ParseDynamicConfigResponse(command string, resp string) []DynamicSubcommand {
	var subcommands []DynamicSubcommand
	prefix := fmt.Sprintf("+%s:", strings.ToUpper(command))
	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		// Extract the format string after "+COMMAND: "
		formatPart := strings.TrimSpace(line[len(prefix):])
		if formatPart == "" {
			continue
		}

		// Parse formatPart. It should be like: "Subcommand",args...
		subName, args, err := ParseSubcommandFormat(formatPart)
		if err != nil {
			slog.Warn("Failed to parse subcommand format", "line", line, "error", err)
			continue
		}

		subcommands = append(subcommands, DynamicSubcommand{
			Name:      subName,
			RawFormat: formatPart,
			Arguments: args,
		})
	}
	return subcommands
}

// ParseSubcommandFormat splits a single format definition line like `"WWAN",(0,1),(1-42),<IP_family>,<IP_address>`
// into subcommand name and argument format parts.
func ParseSubcommandFormat(formatPart string) (string, []string, error) {
	// Look for the first quote
	firstQuoteIdx := strings.Index(formatPart, "\"")
	if firstQuoteIdx == -1 {
		return "", nil, fmt.Errorf("missing starting quote for subcommand name")
	}
	secondQuoteIdx := strings.Index(formatPart[firstQuoteIdx+1:], "\"")
	if secondQuoteIdx == -1 {
		return "", nil, fmt.Errorf("missing ending quote for subcommand name")
	}
	secondQuoteIdx = firstQuoteIdx + 1 + secondQuoteIdx

	subName := formatPart[firstQuoteIdx+1 : secondQuoteIdx]

	// Rest of the string after the second quote (and optional comma)
	rest := formatPart[secondQuoteIdx+1:]
	rest = strings.TrimPrefix(strings.TrimSpace(rest), ",")
	rest = strings.TrimSpace(rest)

	if rest == "" {
		return subName, nil, nil
	}

	// Split by commas, respecting quotes, parentheses, and angle brackets
	var args []string
	var currentArg strings.Builder
	parenCount := 0
	angleCount := 0
	inQuotes := false

	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		switch ch {
		case '(':
			if !inQuotes {
				parenCount++
			}
			currentArg.WriteByte(ch)
		case ')':
			if !inQuotes {
				parenCount--
			}
			currentArg.WriteByte(ch)
		case '<':
			if !inQuotes {
				angleCount++
			}
			currentArg.WriteByte(ch)
		case '>':
			if !inQuotes {
				angleCount--
			}
			currentArg.WriteByte(ch)
		case '"':
			inQuotes = !inQuotes
			currentArg.WriteByte(ch)
		case ',':
			if parenCount == 0 && angleCount == 0 && !inQuotes {
				args = append(args, strings.TrimSpace(currentArg.String()))
				currentArg.Reset()
			} else {
				currentArg.WriteByte(ch)
			}
		default:
			currentArg.WriteByte(ch)
		}
	}
	if currentArg.Len() > 0 {
		args = append(args, strings.TrimSpace(currentArg.String()))
	}

	return subName, args, nil
}
