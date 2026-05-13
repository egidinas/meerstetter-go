package utility

import (
	"net/http"
	"strconv"
	"strings"
)

func parseUintQuery(r *http.Request, key string) uint64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseIntQuery(r *http.Request, key string) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func parseInt64Query(r *http.Request, key string) int64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseBoolQuery(r *http.Request, key string) bool {
	raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get(key)))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
