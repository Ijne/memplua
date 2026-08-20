package server

import (
	"fmt"
	"net/http"
)

type Server struct{}

func (s *Server) Start() error {
	http.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Received request at /ingest")
		body := make([]byte, r.ContentLength)
		_, err := r.Body.Read(body)
		if err.Error() != "EOF" {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	go http.ListenAndServe(":8080", nil)

	return nil
}
