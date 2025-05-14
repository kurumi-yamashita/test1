package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// JWTの署名に使う秘密鍵（※実際の運用では環境変数から読み込むべき）
var jwtSecret = []byte("your-secret-key")

// ユーザー登録リクエストの構造体
type SignupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ✅ JWTの検証処理（保護されたAPIエンドポイントで使用）
func validateJWT(r *http.Request) (string, error) {
	// Authorization ヘッダーからトークン抽出
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("認証トークンがありません")
	}

	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return "", errors.New("bearer トークンの形式が不正です")
	}

	// トークンをパースし署名方式を確認
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("不正な署名方式: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return "", errors.New("トークンが無効です")
	}

	// クレーム（payload）からユーザー名を取得
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("クレームの形式が不正です")
	}

	username, ok := claims["username"].(string)
	if !ok {
		return "", errors.New("トークンにユーザー名が含まれていません")
	}

	return username, nil
}

// ✅ ユーザー登録用のエンドポイント
func SignupHandler(w http.ResponseWriter, r *http.Request) {
	// リクエストボディの読み込みとログ出力
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, "リクエスト読み込みエラー", http.StatusBadRequest)
		return
	}
	log.Println("🔍 受信ボディ内容:", string(bodyBytes))
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// JSONデコード
	var req SignupRequest
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	// 入力バリデーション
	if req.Name == "" || req.Email == "" || req.Password == "" {
		writeJSONError(w, "すべての項目を入力してください", http.StatusBadRequest)
		return
	}

	// DB接続（※実環境では外部設定から取得）
	connStr := "user=user dbname=chat_app_db sslmode=disable password=password"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		writeJSONError(w, "DB接続エラー", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// メールアドレスの重複確認
	var existingEmail string
	err = db.QueryRow("SELECT email FROM users WHERE email = $1", req.Email).Scan(&existingEmail)
	if err == nil {
		writeJSONError(w, "このメールアドレスは既に利用されています", http.StatusBadRequest)
		return
	} else if err != sql.ErrNoRows {
		writeJSONError(w, "DB検索エラー", http.StatusInternalServerError)
		return
	}

	// パスワードのハッシュ化（bcrypt）
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, "パスワード処理エラー", http.StatusInternalServerError)
		return
	}

	// ユーザー登録（is_admin は固定で 0）
	_, err = db.Exec(`
		INSERT INTO users (username, email, password_hash, is_admin, created_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
	`, req.Name, req.Email, string(hashedPassword), 0)

	if err != nil {
		writeJSONError(w, "登録に失敗しました", http.StatusBadRequest)
		log.Println("❌ INSERTエラー:", err)
		return
	}

	// 成功レスポンス
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "登録成功"})
}

// ✅ 共通のエラーレスポンス生成関数
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"message": msg})
}
