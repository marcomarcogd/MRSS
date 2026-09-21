//go:build server

package settings

import (
	"MRSS/internal/handlers/core"
	"net/http"
)

func HandleSelectDataDirectory(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
