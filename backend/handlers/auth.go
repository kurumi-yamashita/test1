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
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// JWTの署名に使う秘密鍵（※実運用では環境変数から読み込む）
var jwtSecret = []byte("your-secret-key")

// JWTから抽出されるカスタムクレーム構造体
type UserClaims struct {
	Username string
	UserID   int
}

// ✅ JWTの検証処理（API向け：Authorizationヘッダーから）
func validateJWT(r *http.Request) (*UserClaims, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, errors.New("認証トークンがありません")
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return nil, errors.New("bearer トークンの形式が不正です")
	}
	return validateJWTFromTokenString(tokenStr)
}

// ✅ JWT文字列から直接検証（WebSocket向けも対応）
func validateJWTFromTokenString(tokenStr string) (*UserClaims, error) {
	if tokenStr == "" {
		return nil, errors.New("トークンが空です")
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("不正な署名方式: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("トークンが無効です")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("クレームの形式が不正です")
	}

	// username 取得
	username, ok1 := claims["username"].(string)
	if !ok1 || username == "" {
		return nil, errors.New("トークンにusernameが含まれていません")
	}

	// userId を複数形式に対応して取得
	var userID int
	switch v := claims["userId"].(type) {
	case float64:
		userID = int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("userIdの形式が不正です")
		}
		userID = parsed
	default:
		return nil, errors.New("トークンにuserIdが含まれていません")
	}

	return &UserClaims{
		Username: username,
		UserID:   userID,
	}, nil
}

// ✅ ユーザー登録ハンドラ
func SignupHandler(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, "リクエスト読み込みエラー", http.StatusBadRequest)
		return
	}
	log.Println("🔍 受信ボディ内容:", string(bodyBytes))
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Email == "" || req.Password == "" {
		writeJSONError(w, "すべての項目を入力してください", http.StatusBadRequest)
		return
	}

	connStr := "user=user dbname=chat_app_db sslmode=disable password=password"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		writeJSONError(w, "DB接続エラー", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var existingEmail string
	err = db.QueryRow("SELECT email FROM users WHERE email = $1", req.Email).Scan(&existingEmail)
	if err == nil {
		writeJSONError(w, "このメールアドレスは既に利用されています", http.StatusBadRequest)
		return
	} else if err != sql.ErrNoRows {
		writeJSONError(w, "DB検索エラー", http.StatusInternalServerError)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, "パスワード処理エラー", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(`INSERT INTO users (username, email, password_hash, is_admin, created_at) VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)`,
		req.Name, req.Email, string(hashedPassword), 0)
	if err != nil {
		writeJSONError(w, "登録に失敗しました", http.StatusBadRequest)
		log.Println("❌ INSERTエラー:", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "登録成功"})
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"message": msg})
}
