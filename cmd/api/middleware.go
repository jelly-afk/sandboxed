package main

import (
	"bufio"
	"net"
	"net/http"
	"time"
)

func (app *application) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		if rw.statusCode >= 400 {
			app.logger.Printf(
				"[ERROR] %s %s %s %d %s",
				r.RemoteAddr,
				r.Method,
				r.URL.Path,
				rw.statusCode,
				duration,
			)
		} else {
			app.logger.Printf(
				"[INFO] %s %s %s %d %s",
				r.RemoteAddr,
				r.Method,
				r.URL.Path,
				rw.statusCode,
				duration,
			)
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode    int
	headerWritten bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.headerWritten {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
		rw.headerWritten = true
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.headerWritten {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}
