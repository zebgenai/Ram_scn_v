package main

import (
	"errors"
	"io"
	"net/http"
	"time"

	"remotefiles/internal/proto"
)

// Screen viewing is admin-only (wired up in routes). A session is bracketed by
// start/stop, which are the audited records of who viewed which PC and when;
// individual frames are not audited (they'd flood the log). While a session is
// active the agent shows a red tray icon + "Screen is being viewed" tooltip.

// handleScreenStart marks a viewing session active and logs it.
func (a *App) handleScreenStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := a.store.Device(id); !ok {
		writeErr(w, http.StatusNotFound, "device not found")
		return
	}
	if !a.hub.Online(id) {
		writeErr(w, http.StatusServiceUnavailable, errOffline.Error())
		return
	}
	a.hub.MarkViewing(id)
	a.audit.Log(AuditEntry{Actor: actor(r), IP: clientIP(r), Action: "screen-view-start", Device: id, OK: true})
	w.WriteHeader(http.StatusNoContent)
}

// handleScreenStop ends a viewing session, clears the indicator and logs it.
func (a *App) handleScreenStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := a.store.Device(id); !ok {
		writeErr(w, http.StatusNotFound, "device not found")
		return
	}
	a.hub.StopViewing(id)
	// Wake the agent's long-poll so its tray indicator clears promptly.
	_ = a.hub.Send(id, proto.Request{ID: randHex(8), Method: proto.MethodNop})
	a.audit.Log(AuditEntry{Actor: actor(r), IP: clientIP(r), Action: "screen-view-stop", Device: id, OK: true})
	w.WriteHeader(http.StatusNoContent)
}

// handleScreenFrame asks the agent for one screenshot and returns it as JPEG,
// reusing the same blob transfer path as a file download.
func (a *App) handleScreenFrame(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := a.store.Device(id); !ok {
		writeErr(w, http.StatusNotFound, "device not found")
		return
	}
	if !a.hub.Online(id) {
		writeErr(w, http.StatusServiceUnavailable, errOffline.Error())
		return
	}
	a.hub.MarkViewing(id) // keep the tray indicator alive while frames flow

	jobID, x := a.transfers.newDownload(id)
	defer a.transfers.remove(jobID)

	if err := a.hub.Send(id, proto.Request{ID: randHex(8), Method: proto.MethodScreen,
		Params: mustJSON(proto.ScreenParams{JobID: jobID, Quality: 55})}); err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	var meta dlMeta
	select {
	case meta = <-x.meta:
	case <-time.After(transferConnectTimeout):
		x.pw.CloseWithError(errTimeout)
		writeErr(w, http.StatusGatewayTimeout, "the PC did not respond in time")
		return
	case <-r.Context().Done():
		x.pw.CloseWithError(errors.New("admin cancelled"))
		return
	}
	if meta.err != nil {
		writeErr(w, http.StatusBadGateway, meta.err.Error())
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	if meta.size > 0 {
		w.Header().Set("Content-Length", itoa(meta.size))
	}
	_, _ = io.Copy(w, x.pr)
}
