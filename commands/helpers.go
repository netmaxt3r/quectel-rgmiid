package commands

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func appendIP(current, newIP string) string {
	if current == "" {
		return newIP
	}
	parts := strings.Split(current, ", ")
	for _, part := range parts {
		if part == newIP {
			return current
		}
	}
	return current + ", " + newIP
}


func formatMcs(args []string) string {
	if len(args) == 0 {
		return "N/A"
	}
	enabled := "Disabled"
	if args[0] == "1" {
		enabled = "Enabled"
	} else if args[0] == "0" {
		enabled = "Disabled"
	} else {
		enabled = args[0]
	}

	if len(args) < 2 {
		return enabled
	}

	mcs := args[1]
	mod := ""
	if len(args) >= 3 {
		modVal := args[2]
		switch modVal {
		case "1":
			mod = "BPSK"
		case "2":
			mod = "QPSK"
		case "4":
			mod = "16QAM"
		case "6":
			mod = "64QAM"
		case "8":
			mod = "256QAM"
		default:
			mod = modVal
		}
	}

	if mod != "" {
		return fmt.Sprintf("%s (MCS: %s, Mod: %s)", enabled, mcs, mod)
	}
	return fmt.Sprintf("%s (MCS: %s)", enabled, mcs)
}

func formatCsi(args []string) string {
	if len(args) == 0 {
		return "N/A"
	}
	var parts []string
	labels := []string{"MCS", "RI", "CQI", "PMI"}
	for i, val := range args {
		if i < len(labels) {
			parts = append(parts, fmt.Sprintf("%s: %s", labels[i], val))
		} else {
			parts = append(parts, fmt.Sprintf("Val%d: %s", i+1, val))
		}
	}
	return strings.Join(parts, ", ")
}

func formatTxPower(args []string) string {
	if len(args) == 0 {
		return "N/A"
	}
	if len(args) == 1 {
		return args[0] + " dBm"
	}
	var parts []string
	labels := []string{"PUCCH", "PUSCH", "PRACH", "SRS"}
	for i, val := range args {
		if i < len(labels) {
			parts = append(parts, fmt.Sprintf("%s: %s dBm", labels[i], val))
		} else {
			parts = append(parts, fmt.Sprintf("Val%d: %s dBm", i+1, val))
		}
	}
	return strings.Join(parts, ", ")
}

func SplitCSV(line string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if b == '"' {
			inQuotes = !inQuotes
		} else if b == ',' && !inQuotes {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteByte(b)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func ParseCSVToStruct(dest interface{}, csvLine string) error {
	val := reflect.ValueOf(dest)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be a pointer to a struct")
	}

	structVal := val.Elem()
	parts := SplitCSV(csvLine)

	numFields := structVal.NumField()
	for i := 0; i < numFields; i++ {
		if i >= len(parts) {
			break
		}

		field := structVal.Field(i)
		if !field.CanSet() {
			continue
		}

		part := strings.TrimSpace(parts[i])

		switch field.Kind() {
		case reflect.String:
			field.SetString(part)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if part != "" {
				intValue, err := strconv.ParseInt(part, 10, 64)
				if err == nil {
					field.SetInt(intValue)
				}
			}
		case reflect.Bool:
			lower := strings.ToLower(part)
			isTrue := lower == "true" || lower == "1" || lower == "tdd" || lower == "yes" || lower == "y"
			field.SetBool(isTrue)
		}
	}
	return nil
}

func IsTerminalResponse(s string) bool {
	trimmed := strings.TrimSpace(s)
	if strings.HasSuffix(trimmed, "OK") {
		return true
	}
	if strings.HasSuffix(trimmed, "ERROR") {
		return true
	}
	if strings.Contains(trimmed, "+CME ERROR:") {
		return true
	}
	if strings.Contains(trimmed, "+CMS ERROR:") {
		return true
	}
	return false
}
