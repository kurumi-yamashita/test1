package handlers

import (
	"encoding/json"
	"net/http"
)

// 共通のエラー書き出し関数（エクスポート版）
func WriteJSONError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
