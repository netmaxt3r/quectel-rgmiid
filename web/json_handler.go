package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"rgmii/commands"
)

// JSONHandler handles standard REST JSON requests.
type JSONHandler struct {
	server *Server
}

// NewJSONHandler creates a new instance of JSONHandler.
func NewJSONHandler(s *Server) *JSONHandler {
	return &JSONHandler{server: s}
}

type cmdRequest struct {
	Cmd string `json:"cmd"`
}

type cmdResponse struct {
	Success  bool   `json:"success"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Helpers

func (h *JSONHandler) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *JSONHandler) writeError(w http.ResponseWriter, statusCode int, errMsg string) {
	h.writeJSON(w, statusCode, map[string]string{"error": errMsg})
}

func (h *JSONHandler) parseContextID(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr := r.PathValue("cid")
	ctxID, err := strconv.Atoi(idStr)
	if err != nil || ctxID < 0 {
		h.writeError(w, http.StatusBadRequest, "invalid or missing id")
		return 0, false
	}
	return ctxID, true
}

// Implementation of RequestHandler

func (h *JSONHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	status := h.server.daemon.GetStatus()
	status.RawResponses["modem_addr"] = h.server.modemAddr
	h.writeJSON(w, http.StatusOK, status)
}

func (h *JSONHandler) HandleGetAPN(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	status := h.server.daemon.GetStatus()
	cfg, exists := status.APNConfigMap[ctxID]
	if !exists {
		h.writeError(w, http.StatusNotFound, "APN config not found for context")
		return
	}
	h.writeJSON(w, http.StatusOK, cfg)
}

func (h *JSONHandler) HandleSetAPN(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	var cfg commands.APNConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.server.daemon.SetAPN(ctxID, cfg); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (h *JSONHandler) HandleActivateData(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	if err := h.server.daemon.ActivateData(ctxID); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (h *JSONHandler) HandleDeactivateData(w http.ResponseWriter, r *http.Request) {
	ctxID, ok := h.parseContextID(w, r)
	if !ok {
		return
	}
	if err := h.server.daemon.DeactivateData(ctxID); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

func (h *JSONHandler) HandleCmd(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		h.writeJSON(w, http.StatusBadRequest, cmdResponse{Success: false, Error: "Content-Type must be application/json"})
		return
	}

	var req cmdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, cmdResponse{Success: false, Error: "Invalid JSON request"})
		return
	}

	if req.Cmd == "" {
		h.writeJSON(w, http.StatusBadRequest, cmdResponse{Success: false, Error: "Command is empty"})
		return
	}

	resp, err := h.server.daemon.SendCommand(req.Cmd)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, cmdResponse{Success: false, Error: err.Error()})
		return
	}

	h.writeJSON(w, http.StatusOK, cmdResponse{Success: true, Response: resp})
}

func (h *JSONHandler) HandleSMSGet(w http.ResponseWriter, r *http.Request) {
	status := h.server.daemon.GetStatus()
	h.writeJSON(w, http.StatusOK, status.SMSList)
}

func (h *JSONHandler) HandleSMSSend(w http.ResponseWriter, r *http.Request) {
	var number, text string

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Number string `json:"number"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid JSON body")
			return
		}
		number = req.Number
		text = req.Text
	} else {
		if err := r.ParseForm(); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid form data")
			return
		}
		number = r.FormValue("number")
		text = r.FormValue("text")
	}

	if number == "" || text == "" {
		h.writeError(w, http.StatusInternalServerError, "Recipient number and message text are required")
		return
	}

	if err := h.server.daemon.SendSMS(number, text); err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to send SMS: %v", err))
		return
	}

	h.server.daemon.PollSMSOnly()
	h.writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "SMS sent successfully",
	})
}

func (h *JSONHandler) HandleSMSDelete(w http.ResponseWriter, r *http.Request) {
	var index int
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Index int `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid JSON body")
			return
		}
		index = req.Index
	} else {
		indexStr := r.FormValue("index")
		if indexStr == "" {
			h.writeError(w, http.StatusBadRequest, "Index is required")
			return
		}
		var err error
		index, err = strconv.Atoi(indexStr)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid index format")
			return
		}
	}

	if err := h.server.daemon.DeleteSMS(index); err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Delete failed: %v", err))
		return
	}

	h.server.daemon.PollSMSOnly()
	h.writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Message deleted successfully",
	})
}

func (h *JSONHandler) HandleModemRestart(w http.ResponseWriter, r *http.Request) {
	if _, err := h.server.daemon.SendCommand("AT+CFUN=1,1"); err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Restart failed: %v", err))
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Modem restart initiated"})
}

func (h *JSONHandler) HandleSettingsPage(w http.ResponseWriter, r *http.Request) {
	h.writeError(w, http.StatusNotImplemented, "endpoint only supports HTMX/HTML requests")
}

func (h *JSONHandler) HandleConsole(w http.ResponseWriter, r *http.Request) {
	h.writeError(w, http.StatusNotImplemented, "endpoint only supports HTMX/HTML requests")
}

func (h *JSONHandler) HandleDebug(w http.ResponseWriter, r *http.Request) {
	status := h.server.daemon.GetStatus()
	status.RawResponses["modem_addr"] = h.server.modemAddr
	h.writeJSON(w, http.StatusOK, status.RawResponses)
}

func (h *JSONHandler) HandleLoginGet(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{
		"message": "Please POST username and password to /login to authenticate",
	})
}

func (h *JSONHandler) HandleLoginPost(w http.ResponseWriter, r *http.Request) {
	if h.server.authUser == "" || h.server.authPass == "" {
		h.writeError(w, http.StatusBadRequest, "Authentication is disabled")
		return
	}

	var username, password string
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid JSON body")
			return
		}
		username = req.Username
		password = req.Password
	} else {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if !h.server.authenticate(username, password) {
		h.writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	_, err := h.server.createSession(w)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Login successful",
	})
}

func (h *JSONHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	h.server.destroySession(w, r)

	h.writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Logout successful",
	})
}
