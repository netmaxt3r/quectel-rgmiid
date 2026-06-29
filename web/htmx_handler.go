package web

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"rgmii/commands"
)

// HTMXHandler handles requests from HTMX/HTML clients, returning HTML fragments.
type HTMXHandler struct {
	server *Server
}

// NewHTMXHandler creates a new instance of HTMXHandler.
func NewHTMXHandler(s *Server) *HTMXHandler {
	return &HTMXHandler{server: s}
}

// Helpers

func (h *HTMXHandler) renderSettingsWithMsg(w http.ResponseWriter, r *http.Request, successMsg, errorMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status := h.server.daemon.GetStatus()
	data := struct {
		commands.ModemStatus
		Success string
		Error   string
	}{
		ModemStatus: status,
		Success:     successMsg,
		Error:       errorMsg,
	}
	if err := tmpl.ExecuteTemplate(w, "settings.html", data); err != nil {
		slog.Error("Error executing settings template", "error", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

func (h *HTMXHandler) renderSMSWithMsg(w http.ResponseWriter, r *http.Request, successMsg, errorMsg, number, text string) {
	status := h.server.daemon.GetStatus()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		commands.ModemStatus
		Success string
		Error   string
		Number  string
		Text    string
	}{
		ModemStatus: status,
		Success:     successMsg,
		Error:       errorMsg,
		Number:      number,
		Text:        text,
	}

	err := tmpl.ExecuteTemplate(w, "sms.html", data)
	if err != nil {
		slog.Error("Error executing sms template", "error", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

func (h *HTMXHandler) parseContextID(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr := r.PathValue("cid")
	ctxID, err := strconv.Atoi(idStr)
	if err != nil || ctxID < 0 {
		h.renderSettingsWithMsg(w, r, "", "invalid or missing id")
		return 0, false
	}
	return ctxID, true
}

// Implementation of RequestHandler

func (h *HTMXHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	status := h.server.daemon.GetStatus()
	status.RawResponses["modem_addr"] = h.server.modemAddr
	h.server.renderTemplate(w, http.StatusOK, "status.html", status)
}

func (h *HTMXHandler) HandleGetAPN(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (h *HTMXHandler) HandleSetAPN(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	var cfg commands.APNConfig
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithMsg(w, r, "", "failed to parse form data")
		return
	}
	cfg.APN = r.FormValue("apn")
	cfg.PDPType = r.FormValue("pdp_type")
	cfg.Username = r.FormValue("username")
	cfg.Password = r.FormValue("password")

	if err := h.server.daemon.SetAPN(ctxID, cfg); err != nil {
		h.renderSettingsWithMsg(w, r, "", fmt.Sprintf("Failed to save APN: %v", err))
		return
	}

	h.renderSettingsWithMsg(w, r, fmt.Sprintf("APN for context %d saved successfully.", ctxID), "")
}

func (h *HTMXHandler) HandleActivateData(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	if err := h.server.daemon.ActivateData(ctxID); err != nil {
		h.renderSettingsWithMsg(w, r, "", fmt.Sprintf("Activation failed: %v", err))
		return
	}
	h.renderSettingsWithMsg(w, r, fmt.Sprintf("Data context %d activated.", ctxID), "")
}

func (h *HTMXHandler) HandleDeactivateData(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	if err := h.server.daemon.DeactivateData(ctxID); err != nil {
		h.renderSettingsWithMsg(w, r, "", fmt.Sprintf("Deactivation failed: %v", err))
		return
	}
	h.renderSettingsWithMsg(w, r, fmt.Sprintf("Data context %d deactivated.", ctxID), "")
}

func (h *HTMXHandler) HandleCmd(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	cmd := r.FormValue("cmd")
	if cmd == "" {
		http.Error(w, "Command is empty", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<div class=\"mb-3\"><span class=\"text-emerald-400\">&gt; %s</span><br>", template.HTMLEscapeString(cmd))

	resp, err := h.server.daemon.SendCommand(cmd)
	if err != nil {
		w.Header().Set("X-Cmd-Failed", "true")
		fmt.Fprintf(w, "<span class=\"text-rose-500 font-medium\">Error: %s</span></div>", template.HTMLEscapeString(err.Error()))
		return
	}

	escapedResp := template.HTMLEscapeString(resp)
	escapedResp = strings.ReplaceAll(escapedResp, "\n", "<br>")
	escapedResp = strings.ReplaceAll(escapedResp, "\r", "")
	fmt.Fprintf(w, "<span class=\"text-slate-300 font-mono text-xs leading-relaxed\">%s</span></div>", escapedResp)
}

func (h *HTMXHandler) HandleSMSGet(w http.ResponseWriter, r *http.Request) {
	h.renderSMSWithMsg(w, r, "", "", "", "")
}

func (h *HTMXHandler) HandleSMSSend(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		h.renderSMSWithMsg(w, r, "", "Invalid form data", "", "")
		return
	}
	number := r.FormValue("number")
	text := r.FormValue("text")

	if number == "" || text == "" {
		h.renderSMSWithMsg(w, r, "", "Recipient number and message text are required", number, text)
		return
	}

	err = h.server.daemon.SendSMS(number, text)
	if err != nil {
		slog.Error("Failed to send SMS", "error", err)
		h.renderSMSWithMsg(w, r, "", fmt.Sprintf("Failed to send SMS: %v", err), number, text)
		return
	}

	h.server.daemon.PollSMSOnly()
	h.renderSMSWithMsg(w, r, "SMS sent successfully", "", "", "")
}

func (h *HTMXHandler) HandleSMSDelete(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	indexStr := r.FormValue("index")
	if indexStr == "" {
		http.Error(w, "Index is required", http.StatusBadRequest)
		return
	}

	var index int
	_, err = fmt.Sscanf(indexStr, "%d", &index)
	if err != nil {
		http.Error(w, "Invalid index format", http.StatusBadRequest)
		return
	}

	slog.Info("Received request to delete SMS", "index", index)
	err = h.server.daemon.DeleteSMS(index)
	if err != nil {
		http.Error(w, fmt.Sprintf("Delete failed: %v", err), http.StatusInternalServerError)
		return
	}

	h.server.daemon.PollSMSOnly()
	h.renderSMSWithMsg(w, r, "Message deleted successfully", "", "", "")
}

func (h *HTMXHandler) HandleModemRestart(w http.ResponseWriter, r *http.Request) {
	slog.Info("Received request to restart modem, sending AT+CFUN=1,1")
	_, err := h.server.daemon.SendCommand("AT+CFUN=1,1")
	if err != nil {
		http.Error(w, fmt.Sprintf("Restart failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<span class=\"text-emerald-400 font-medium\">Modem restart command (AT+CFUN=1,1) executed successfully. Modem is rebooting.</span>"))
}

func (h *HTMXHandler) HandleSettingsPage(w http.ResponseWriter, r *http.Request) {
	h.renderSettingsWithMsg(w, r, "", "")
}

func (h *HTMXHandler) HandleConsole(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := tmpl.ExecuteTemplate(w, "console.html", nil)
	if err != nil {
		slog.Error("Error executing console template", "error", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

func (h *HTMXHandler) HandleDebug(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (h *HTMXHandler) HandleLoginGet(w http.ResponseWriter, r *http.Request) {
	if h.server.authUser == "" || h.server.authPass == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if h.server.isSessionValid(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.server.renderTemplate(w, http.StatusOK, "login.html", nil)
}

func (h *HTMXHandler) HandleLoginPost(w http.ResponseWriter, r *http.Request) {
	if h.server.authUser == "" || h.server.authPass == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	err := r.ParseForm()
	if err != nil {
		h.server.renderTemplate(w, http.StatusOK, "login.html", map[string]string{"Error": "Invalid form submission"})
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if !h.server.authenticate(username, password) {
		h.server.renderTemplate(w, http.StatusUnauthorized, "login.html", map[string]string{"Error": "Invalid username or password"})
		return
	}

	_, err = h.server.createSession(w)
	if err != nil {
		h.server.renderTemplate(w, http.StatusInternalServerError, "login.html", map[string]string{"Error": "Internal server error"})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *HTMXHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	h.server.destroySession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (h *HTMXHandler) HandleDynConfigs(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}
func (h *HTMXHandler) HandleDynConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		name = r.URL.Query().Get("name")
	}
	state, ok := h.server.daemon.GetDynamicConfigState(name)
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`
			<div class="glass-panel p-8 rounded-3xl text-center">
				<svg class="h-12 w-12 mx-auto mb-4 text-slate-500 animate-pulse" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376C1.83 15.018 1.83 14 2.87 14h18.26c1.04 0 1.68 1.018 1.16 1.976l-9.13 16.74c-.52.955-1.92.955-2.44 0l-9.13-16.74ZM12 17.25h.008v.008H12v-.008Z" />
				</svg>
				<h4 class="text-lg font-semibold text-slate-200 mb-2">Configuration State Not Found</h4>
				<p class="text-sm text-slate-400">Make sure the daemon is connected to the modem via the network connection, as format definitions are only queried on connection events.</p>
			</div>
		`))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "dynconfig.html", state); err != nil {
		slog.Error("Error executing dynconfig template", "error", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

func (h *HTMXHandler) HandleDynConfigGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	subname := r.URL.Query().Get("subname")

	val, resp, err := h.server.daemon.QueryDynamicConfigValue(name, subname)
	escapedResp := template.HTMLEscapeString(resp)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		//w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<div class="flex flex-col gap-1.5 min-w-0 w-full" title="Raw Response:&#10;%s">`, escapedResp)
		fmt.Fprintf(w, `<span class="text-rose-500 font-mono text-xs font-semibold">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
		fmt.Fprintf(w, `</div>`)
		return
	}

	fmt.Fprintf(w, `<div class="flex flex-col gap-1.5 min-w-0 w-full" title="Raw Response:&#10;%s">`, escapedResp)
	if len(val) == 0 {
		fmt.Fprintf(w, `<div class="flex items-start gap-1.5 text-cyan-400 font-mono text-xs">`)
		fmt.Fprintf(w, `<span class="text-cyan-600/50 select-none shrink-0 font-bold">&gt;</span>`)
		fmt.Fprintf(w, `<span class="select-all break-all leading-normal flex-1 font-semibold">OK</span>`)
		fmt.Fprintf(w, `</div>`)
	} else {
		for _, line := range val {
			fmt.Fprintf(w, `<div class="flex items-start gap-1.5 text-cyan-400 font-mono text-xs">`)
			fmt.Fprintf(w, `<span class="text-cyan-600/50 select-none shrink-0 font-bold">&gt;</span>`)
			fmt.Fprintf(w, `<span class="select-all break-all leading-normal flex-1 font-semibold">%s</span>`, template.HTMLEscapeString(line))
			fmt.Fprintf(w, `</div>`)
		}
	}
	fmt.Fprintf(w, `</div>`)
}

func (h *HTMXHandler) HandleDynConfigSet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	subname := r.URL.Query().Get("subname")

	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<span class="text-rose-500 font-mono text-xs font-semibold">Error parsing form</span>`)
		return
	}
	args := r.FormValue("args")

	val, resp, err := h.server.daemon.SetDynamicConfigValue(name, subname, args)
	escapedResp := template.HTMLEscapeString(resp)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		//need to show raw resp even on error
		fmt.Fprintf(w, `<div class="flex flex-col gap-1.5 min-w-0 w-full" title="Raw Response:&#10;%s">`, escapedResp)
		fmt.Fprintf(w, `<span class="text-rose-500 font-mono text-xs font-semibold">Error: %s</span>`, template.HTMLEscapeString(err.Error()))

		fmt.Fprintf(w, `</div>`)
		return
	}
	fmt.Fprintf(w, `<div class="flex flex-col gap-1.5 min-w-0 w-full" title="Raw Response:&#10;%s">`, escapedResp)
	if len(val) == 0 {
		fmt.Fprintf(w, `<div class="flex items-start gap-1.5 text-emerald-400 font-mono text-xs">`)
		fmt.Fprintf(w, `<span class="text-emerald-600/50 select-none shrink-0 font-bold">&gt;</span>`)
		fmt.Fprintf(w, `<span class="select-all break-all leading-normal flex-1 font-semibold">OK</span>`)
		fmt.Fprintf(w, `</div>`)
	} else {
		for _, line := range val {
			fmt.Fprintf(w, `<div class="flex items-start gap-1.5 text-emerald-400 font-mono text-xs">`)
			fmt.Fprintf(w, `<span class="text-emerald-600/50 select-none shrink-0 font-bold">&gt;</span>`)
			fmt.Fprintf(w, `<span class="select-all break-all leading-normal flex-1 font-semibold">%s</span>`, template.HTMLEscapeString(line))
			fmt.Fprintf(w, `</div>`)
		}
	}
	fmt.Fprintf(w, `</div>`)
}
