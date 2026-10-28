package relay

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

// Handle serves POST /register and POST /push of the function URL.
func Handle(r *Relay, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	if req.RequestContext.HTTP.Method != http.MethodPost {
		return reply(http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
	}
	body := []byte(req.Body)
	if req.IsBase64Encoded {
		b, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return reply(http.StatusBadRequest, map[string]string{"error": "bad body"})
		}
		body = b
	}
	if len(body) > 8192 {
		return reply(http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
	}
	switch req.RawPath {
	case "/register":
		var in RegisterRequest
		if json.Unmarshal(body, &in) != nil {
			return reply(http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		}
		t, err := r.Register(in)
		if err != nil {
			return errorReply(err)
		}
		if t == "" {
			return reply(http.StatusAccepted, map[string]string{"status": "challenge sent"})
		}
		return reply(http.StatusOK, map[string]string{"send_token": t})
	case "/push":
		var in PushRequest
		if json.Unmarshal(body, &in) != nil {
			return reply(http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		}
		if err := r.Push(in); err != nil {
			return errorReply(err)
		}
		return reply(http.StatusOK, map[string]string{"status": "sent"})
	}
	return reply(http.StatusNotFound, map[string]string{"error": "not found"})
}

func errorReply(err error) events.LambdaFunctionURLResponse {
	var e *Error
	if errors.As(err, &e) {
		return reply(e.Status, map[string]string{"error": e.Msg})
	}
	log.Print("relay: internal error")
	return reply(http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

func reply(status int, v any) events.LambdaFunctionURLResponse {
	b, _ := json.Marshal(v)
	return events.LambdaFunctionURLResponse{StatusCode: status, Headers: map[string]string{"content-type": "application/json"}, Body: string(b)}
}
