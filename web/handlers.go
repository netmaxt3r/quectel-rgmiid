package web

import (
	"net/http"
	"strings"
)

// RequestHandler defines the unified interface for serving HTTP requests.
// Separate implementations (JSON and HTMX) will satisfy this interface.
type RequestHandler interface {
	HandleStatus(w http.ResponseWriter, r *http.Request)
	HandleGetAPN(w http.ResponseWriter, r *http.Request)
	HandleSetAPN(w http.ResponseWriter, r *http.Request)
	HandleActivateData(w http.ResponseWriter, r *http.Request)
	HandleDeactivateData(w http.ResponseWriter, r *http.Request)
	HandleCmd(w http.ResponseWriter, r *http.Request)
	HandleSMSGet(w http.ResponseWriter, r *http.Request)
	HandleSMSSend(w http.ResponseWriter, r *http.Request)
	HandleSMSDelete(w http.ResponseWriter, r *http.Request)
	HandleModemRestart(w http.ResponseWriter, r *http.Request)
	HandleSettingsPage(w http.ResponseWriter, r *http.Request)
	HandleConsole(w http.ResponseWriter, r *http.Request)
	HandleDebug(w http.ResponseWriter, r *http.Request)
	HandleLoginGet(w http.ResponseWriter, r *http.Request)
	HandleLoginPost(w http.ResponseWriter, r *http.Request)
	HandleLogout(w http.ResponseWriter, r *http.Request)
	HandleDynConfigs(w http.ResponseWriter, r *http.Request)
	HandleDynConfig(w http.ResponseWriter, r *http.Request)
	HandleDynConfigGet(w http.ResponseWriter, r *http.Request)
	HandleDynConfigSet(w http.ResponseWriter, r *http.Request)
}

// dispatch routes the incoming request to the htmx handler if the HX-Request or
// Accept text/html header is set, otherwise falling back to the standard JSON handler.
func (s *Server) dispatch(fn func(RequestHandler, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isHTML := r.Header.Get("HX-Request") == "true" || strings.Contains(r.Header.Get("Accept"), "text/html")
		if isHTML {
			fn(s.htmxHandler, w, r)
		} else {
			fn(s.jsonHandler, w, r)
		}
	}
}
