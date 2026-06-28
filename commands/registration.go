package commands

import (
	"strings"
)

// Registration represents the network registration response.
type Registration struct {
	NetworkRegistration string `json:"network_registration"`
}

func (r *Registration) Command(ctx *ParsingContext) ATCommand {
	if ctx != nil {
		switch ctx.Tech {
		case NR5G_SA, NSA_5G:
			return ATCommand{
				Name:           "c5greg",
				Command:        "AT+C5GREG?",
				ResponsePrefix: "+C5GREG:",
			}
		case LTE:
			return ATCommand{
				Name:           "cereg",
				Command:        "AT+CEREG?",
				ResponsePrefix: "+CEREG:",
			}

		default:
			//TODO rest of tech
		}
	}
	return ATCommand{
		Name:           "cgreg",
		Command:        "AT+CGREG?",
		ResponsePrefix: "+CGREG:",
	}
}

func (r *Registration) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	r.NetworkRegistration = "Not Registered"
	if len(resp) > 0 {
		line := resp[0]
		parts := strings.Split(line, ",")
		var stat string
		if len(parts) >= 2 {
			stat = strings.TrimSpace(parts[1])
		} else if len(parts) == 1 {
			stat = strings.TrimSpace(parts[0])
		}
		if stat != "" {
			switch stat {
			case "0":
				r.NetworkRegistration = "Not registered"
			case "1":
				r.NetworkRegistration = "Registered"
			case "2":
				r.NetworkRegistration = "Search"
			case "3":
				r.NetworkRegistration = "Registration denied"
			case "4":
				r.NetworkRegistration = "Unknown"
			case "5":
				r.NetworkRegistration = "Roaming"
			default:
				r.NetworkRegistration = "Unknown"
			}
		}
	}
}
