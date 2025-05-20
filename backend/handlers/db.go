package handlers

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

// グローバルDB変数（他のハンドラ関数からも使える）
var DB *sql.DB

// DB初期化関数（アプリ起動時に呼び出す）
func InitDB() {
	var err error

	// 接続用情報を指定（ユーザー名・DB名・パスワード）
	db, err := sql.Open("postgres", "user=user dbname=chat_app_db sslmode=disable password=password")
	if err != nil {
		log.Fatalf("❌ DB接続失敗: %v", err)
	}

	// グローバル変数に代入
	DB = db
}
