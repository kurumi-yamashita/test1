package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5" // JWTライブラリ
	_ "github.com/lib/pq"          // PostgreSQLドライバ（暗黙import）
	"golang.org/x/crypto/bcrypt"   // パスワードハッシュ照合用ライブラリ
)

// クライアントから受け取るログイン情報構造体
type LoginRequest struct {
	Email    string `json:"email"`    // メールアドレス
	Password string `json:"password"` // パスワード
}

// JWTトークンを生成する関数
func generateJWT(username string) (string, error) {
	// クレーム情報（有効期限・ユーザー名など）を定義
	claims := jwt.MapClaims{
		"username": username,                              // ユーザー名をクレームに含める
		"exp":      time.Now().Add(24 * time.Hour).Unix(), // 24時間の有効期限
	}

	// HMAC SHA256で署名されたトークンを生成
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 秘密鍵でトークンに署名して文字列として返す
	return token.SignedString(jwtSecret) // jwtSecretは外部定義された秘密鍵
}

// ログイン処理を行うHTTPハンドラ
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	// POST以外のメソッドは拒否
	if r.Method != http.MethodPost {
		writeJSONError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// リクエストボディをLoginRequest構造体へデコード
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	log.Printf("📥 ログインリクエスト: %+v", req)

	// メールアドレスとパスワードの空チェック
	if req.Email == "" || req.Password == "" {
		writeJSONError(w, "メールアドレスとパスワードは必須です", http.StatusBadRequest)
		return
	}

	// PostgreSQL接続文字列（ユーザー・DB名・パスワードは適宜変更）
	connStr := "user=user dbname=chat_app_db sslmode=disable password=password"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Println("❌ DB接続失敗:", err)
		writeJSONError(w, "DB接続エラー", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// メールアドレスに対応するユーザー情報をDBから取得
	var userID int
	var hashedPassword, username string
	err = db.QueryRow(
		"SELECT id, password_hash, username FROM users WHERE email = $1", req.Email).
		Scan(&userID, &hashedPassword, &username)

	// ユーザーが見つからない場合
	if err == sql.ErrNoRows {
		writeJSONError(w, "メールアドレスが存在しません", http.StatusUnauthorized)
		return
	} else if err != nil {
		// その他のDBエラー
		log.Println("❌ DBクエリエラー:", err)
		writeJSONError(w, "DBエラー", http.StatusInternalServerError)
		return
	}

	// ハッシュ化されたパスワードと比較（bcrypt使用）
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Password)); err != nil {
		writeJSONError(w, "パスワードが違います", http.StatusUnauthorized)
		return
	}

	// 認証成功 → JWT生成
	token, err := generateJWT(username)
	if err != nil {
		writeJSONError(w, "トークン生成エラー", http.StatusInternalServerError)
		return
	}

	// レスポンスヘッダ設定とJSONレスポンス送信
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "ログイン成功", // フロントに返すメッセージ
		"username": username, // 表示用ユーザー名
		"userId":   userID,   // ユーザーID（セッション管理などに使用）
		"token":    token,    // JWTトークン
	})
}
