package handlers

import (
	"encoding/json"
	"net/http"
)

// ユーザー情報構造体（必要最小限の情報のみ）
type SimpleUser struct {
	ID       int    `json:"id"`       // ユーザーID
	Username string `json:"username"` // ユーザー名
}

// 全ユーザー取得ハンドラー（OPTIONSも含むCORS対応あり）
func GetAllUsers(w http.ResponseWriter, r *http.Request) {
	// CORS対応ヘッダーの設定
	w.Header().Set("Access-Control-Allow-Origin", "*") // すべてのオリジンを許可
	w.Header().Set("Content-Type", "application/json") // レスポンスのコンテンツタイプ
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// OPTIONSリクエスト（プリフライト）への対応
	if r.Method == http.MethodOptions {
		return // 200 OK で終了
	}

	// DBからユーザー情報を取得
	rows, err := DB.Query(`SELECT id, username FROM users`)
	if err != nil {
		// 取得失敗時のエラーレスポンス
		writeJSONError(w, "ユーザー取得失敗", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// 結果をSimpleUserの配列として構築
	var users []SimpleUser
	for rows.Next() {
		var u SimpleUser
		if err := rows.Scan(&u.ID, &u.Username); err == nil {
			users = append(users, u)
		}
	}

	// JSONとしてクライアントに返却
	json.NewEncoder(w).Encode(users)
}
