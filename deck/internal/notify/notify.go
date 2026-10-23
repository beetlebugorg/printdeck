// Package notify sends push messages about a print to a phone and a watch, with ntfy
// (https://ntfy.sh): the end of a print, a pause, the end of the first layer, and a roll that runs
// short. The ntfy app on the phone shows them, and the watch shows the messages of the phone.
package notify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"printdeck/internal/printer"
)

// Server is the ntfy server for a topic with no server in it.
const Server = "https://ntfy.sh"

// URL is the address of a topic: a topic name ("printdeck-7f3a") on Server, or a full address of a
// topic on another ntfy server.
func URL(topic string) string {
	topic = strings.TrimSpace(topic)
	if strings.Contains(topic, "://") {
		return topic
	}
	return Server + "/" + strings.TrimPrefix(topic, "/")
}

// Message is a push message.
type Message struct {
	Title, Body string
	Tags        string // emoji tags of ntfy, for example "white_check_mark"
	Priority    int    // 1 to 5; 0 is the default (3)
}

// Send posts a message to the topic.
func Send(ctx context.Context, topic string, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, URL(topic), strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", m.Title)
	if m.Tags != "" {
		req.Header.Set("Tags", m.Tags)
	}
	if m.Priority > 0 {
		req.Header.Set("Priority", fmt.Sprint(m.Priority))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ntfy: %s", resp.Status)
	}
	return nil
}

// Roll is the roll in the printer for the check of a print: its name, the grams on it, and the
// grams of the rest of the print. Known is false when the deck has no roll or the file does not
// say its filament.
type Roll struct {
	Name       string
	Left, Need float64
	Known      bool
}

// Watcher follows the state of the printer, and makes a message at each event of a print.
type Watcher struct {
	started             bool
	busy, paused        bool
	file                string
	layer               int
	firstLayer, rollMsg bool // the messages of this print that are sent
}

// Step takes the state now and the roll, and returns the messages of the events since the last
// step.
func (w *Watcher) Step(st printer.State, roll Roll) []Message {
	busy := st.Reachable && st.Busy()
	if !w.started {
		// The first state is no event: the deck can start during a print.
		w.started, w.busy, w.paused, w.file, w.layer = true, busy, st.Paused, st.File, st.Layer
		w.firstLayer, w.rollMsg = st.Layer > 1, false
		return nil
	}
	var out []Message
	switch {
	case busy && (!w.busy || st.File != w.file):
		w.firstLayer, w.rollMsg = false, false // a new print
	case !busy && w.busy:
		switch st.Finished {
		case "complete":
			body := st.FinishedFile + " · " + duration(st.FinishedTime)
			out = append(out, Message{Title: "Print done", Body: body, Tags: "white_check_mark"})
		case "error":
			body := st.FinishedFile
			if st.FinishedError != "" {
				body += "\n" + st.FinishedError
			}
			out = append(out, Message{Title: "Print failed", Body: body, Tags: "x", Priority: 4})
		}
	}
	if busy && st.Paused && !w.paused {
		body := st.File
		switch {
		case st.PausePrompt != "":
			body = st.PausePrompt
		case st.Message != "":
			body = st.Message
		case st.Layers > 0:
			body = fmt.Sprintf("%s · layer %d of %d", st.File, st.Layer, st.Layers)
		}
		title := "Print paused"
		if st.PauseRoll != "" {
			title = "Change the filament"
		}
		out = append(out, Message{Title: title, Body: body, Tags: "pause_button", Priority: 4})
	}
	if busy && !w.firstLayer && st.Layers > 1 && st.Layer >= 2 {
		w.firstLayer = true
		out = append(out, Message{Title: "First layer done", Body: st.File, Tags: "ok"})
	}
	if busy && !w.rollMsg && roll.Known && roll.Need > roll.Left {
		w.rollMsg = true
		body := fmt.Sprintf("%s has %s left. The rest of %s uses %s.", roll.Name, printer.Weight(roll.Left), st.File, printer.Weight(roll.Need))
		out = append(out, Message{Title: "The roll runs short", Body: body, Tags: "warning", Priority: 4})
	}
	w.busy, w.paused, w.file, w.layer = busy, st.Paused, st.File, st.Layer
	return out
}

// duration is a time as "1h 42m" or "12 min".
func duration(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m >= 60 {
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", max(m, 1))
}
