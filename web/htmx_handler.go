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
