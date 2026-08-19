package server

import "net/http"

type Server struct{}

func (s *Server) Start() error {
	http.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, err := r.Body.Read(body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	go http.ListenAndServe(":8080", nil)

	return nil
}
