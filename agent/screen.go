package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"remotefiles/internal/proto"
)

// lastViewing tracks whether an admin is currently viewing this PC's screen, as
// last reported by the server on a poll. poll() runs on a single goroutine, so
// no locking is needed here.
var lastViewing bool

// updateViewing reacts to the server's per-poll "being viewed" flag. On a change
// it notifies the platform UI (on Windows, the tray icon turns red).
func updateViewing(on bool) {
	if on != lastViewing {
		lastViewing = on
		notifyViewing(on)
	}
}

// pushScreen captures the primary display and pushes the JPEG to the server's
// blob endpoint under p.JobID — the same path a file download uses. The admin's
// waiting request receives the bytes.
func pushScreen(cfg config, p proto.ScreenParams) (proto.OpResult, error) {
	q := p.Quality
	if q <= 0 || q > 100 {
		q = 55
	}
	img, err := captureScreenJPEG(q)
	if err != nil {
		notifyBlobError(cfg, p.JobID, err)
		return proto.OpResult{}, err
	}
	req, _ := http.NewRequest(http.MethodPost, cfg.Server+"/agent/blob/"+p.JobID, bytes.NewReader(img))
	req.Header.Set("Authorization", authHeader(cfg))
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("X-RF-Name", "screen.jpg")
	req.ContentLength = int64(len(img))
	resp, err := transferClient.Do(req)
	if err != nil {
		return proto.OpResult{}, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return proto.OpResult{}, fmt.Errorf("server rejected frame (%s)", resp.Status)
	}
	return proto.OpResult{Bytes: int64(len(img))}, nil
}
