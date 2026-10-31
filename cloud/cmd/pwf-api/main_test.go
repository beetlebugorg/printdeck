package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// All the cookies of a reply go to the browser: a function URL takes them in their own list.
func TestServeCookies(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, n := range []string{"CloudFront-Policy", "CloudFront-Signature", "CloudFront-Key-Pair-Id"} {
			http.SetCookie(w, &http.Cookie{Name: n, Value: "v", Path: "/x/p/"})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	})
	var req events.LambdaFunctionURLRequest
	req.RequestContext.HTTP.Method = "GET"
	req.RawPath = "/api/v1/projects/p"
	out := serve(context.Background(), h, req)
	if len(out.Cookies) != 3 || out.Headers["Content-Type"] != "application/json" {
		t.Fatalf("cookies %v, headers %v", out.Cookies, out.Headers)
	}
	if _, ok := out.Headers["Set-Cookie"]; ok {
		t.Error("a cookie in the headers")
	}
}
