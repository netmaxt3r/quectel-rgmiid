package devicestatus

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

func parseServingCellParts(output string, typePrefix string) []string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QENG:") {
			continue
		}
		content := line[6:]
		if strings.HasPrefix(content, " ") {
			content = content[1:]
		}
		parts := strings.Split(content, ",")
		for i := range parts {
			parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
		}
		if len(parts) < 3 {
			continue
		}
		if typePrefix == "NR5G-NSA" && parts[0] == "NR5G-NSA" {
			return parts
		}
		if parts[0] == "servingcell" && parts[2] == typePrefix {
			return parts
		}
	}
	return nil
}

func parseMimoStringCommon(resp string, paramName string) string {
	cleaned := strings.ReplaceAll(resp, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")
	idx := strings.Index(cleaned, "+QNWCFG:\""+paramName+"\",")
	if idx != -1 {
		var mode, layers int
		_, err := fmt.Sscanf(cleaned[idx:], "+QNWCFG:\""+paramName+"\",%d,%d", &mode, &layers)
		if err == nil {
			return fmt.Sprintf("%dx%d MIMO", layers, layers)
		}
	}
	return "Unknown"
}

func parseQNWCFGString(resp string, paramName string) []string {
	cleaned := strings.ReplaceAll(resp, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")

	prefixWithQuotes := fmt.Sprintf("+QNWCFG:\"%s\",", paramName)
	prefixWithoutQuotes := fmt.Sprintf("+QNWCFG:%s,", paramName)

	var startIdx int
	if idx := strings.Index(cleaned, prefixWithQuotes); idx != -1 {
		startIdx = idx + len(prefixWithQuotes)
	} else if idx := strings.Index(cleaned, prefixWithoutQuotes); idx != -1 {
		startIdx = idx + len(prefixWithoutQuotes)
	} else {
		return nil
	}

	argsStr := cleaned[startIdx:]
	if strings.HasSuffix(argsStr, "OK") {
		argsStr = strings.TrimSuffix(argsStr, "OK")
	}

	if argsStr == "" {
		return nil
	}

	return strings.Split(argsStr, ",")
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

func splitCSV(line string) []string {
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
	parts := splitCSV(csvLine)

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
