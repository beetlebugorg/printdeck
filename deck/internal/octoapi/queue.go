package octoapi

import (
	"net/http"

	"printdeck/internal/queue"
)

// The print queue of the deck for the apps: the files in their order, a move, and a removal. The
// API has no start of a queued file: a press on the touch screen of the deck starts it.

// queueJSON is the reply of GET /api/plugin/printdeck/queue.
type queueJSON struct {
	Items   []queue.Item `json:"items"`
	Seconds int          `json:"seconds"` // the print time of all the files
}

func (s *Server) serveQueue(w http.ResponseWriter, r *http.Request) {
	if s.Queue == nil {
		writeJSON(w, http.StatusOK, queueJSON{Items: []queue.Item{}})
		return
	}
	items := s.Queue.List()
	if items == nil {
		items = []queue.Item{}
	}
	writeJSON(w, http.StatusOK, queueJSON{Items: items, Seconds: s.Queue.Seconds()})
}

// serveQueueMove moves a file of the queue: {"by": -1} is one place nearer the front.
func (s *Server) serveQueueMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		By int `json:"by"`
	}
	if !decode(w, r, &req) {
		return
	}
	if s.Queue == nil || req.By == 0 {
		apiError(w, http.StatusBadRequest, "No such move")
		return
	}
	if !s.Queue.Move(r.PathValue("id"), req.By) {
		apiError(w, http.StatusConflict, "The file is not in the queue, or it is at the end already")
		return
	}
	s.serveQueue(w, r)
}

func (s *Server) serveQueueRemove(w http.ResponseWriter, r *http.Request) {
	if s.Queue == nil || !s.Queue.Remove(r.PathValue("id")) {
		apiError(w, http.StatusNotFound, "The file is not in the queue")
		return
	}
	s.serveQueue(w, r)
}
