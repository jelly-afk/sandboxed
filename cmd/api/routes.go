package main

import "github.com/gorilla/mux"

func (app *application) routes() *mux.Router {
	mux := mux.NewRouter()
	mux.HandleFunc("/v1/execute", app.executeHandler)
	mux.HandleFunc("/v1/healthcheck", app.healthcheckHandler)
	return mux
}
