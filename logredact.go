package main

import (
	"bytes"
	"io"
	"regexp"
)

// secretExpr matches the credentials lazyflickrgo puts in the request URL it
// logs for every API call.
var secretExpr = regexp.MustCompile(`(?i)\b(api_key|api_sig|auth_token|secret|access_token)=[^&\s"']+`)

// redactWriter strips Flickr credentials from log output. Without it every
// API call leaves the key and signature in docker logs and in log.log.
type redactWriter struct{ w io.Writer }

func (rw redactWriter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, []byte("=")) {
		return rw.w.Write(p)
	}
	if _, err := rw.w.Write(secretExpr.ReplaceAll(p, []byte("$1=REDACTED"))); err != nil {
		return 0, err
	}
	// io.Writer must report the caller's length, not the redacted one.
	return len(p), nil
}
