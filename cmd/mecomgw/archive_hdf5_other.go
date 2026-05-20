//go:build !linux

package main

import "net/http"

func (s *server) handleHDF5Export(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "HDF5 export requires libhdf5 and is only supported on Linux", http.StatusNotImplemented)
}
